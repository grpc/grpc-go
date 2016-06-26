package transport

import (
	"bytes"
	"errors"
	"golang.org/x/net/context"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrIllegalHeaderWrite indicates that setting header is illegal because of
// the stream's state.
var ErrIllegalHeaderWrite = StreamErrorf(codes.Unknown, "the stream is done or WriteHeader was already called")

type http2Transport struct {
	isClient bool // TODO(dennwc): remove

	conn     net.Conn             // underlying communication channel
	authInfo credentials.AuthInfo // auth info about the connection

	// writableChan synchronizes write access to the transport.
	// A writer acquires the write lock by sending a value on writableChan
	// and releases it by receiving from writableChan.
	writableChan chan int
	// shutdownChan is closed when Close is called.
	// Blocking operations should select on shutdownChan to avoid
	// blocking forever after Close.
	// TODO(zhaoq): Maybe have a channel context (client)?
	shutdownChan chan struct{}
	framer       *framer
	hBuf         *bytes.Buffer  // the buffer for HPACK encoding
	hEnc         *hpack.Encoder // HPACK encoder

	// The max number of concurrent streams.
	maxStreamsMe   uint32
	maxStreamsPeer uint32
	// controlBuf delivers all the control related tasks (e.g., window
	// updates, reset streams, and various settings) to the controller.
	controlBuf *recvBuffer
	fc         *inFlow
	// sendQuotaPool provides flow control to outbound message.
	sendQuotaPool *quotaPool

	mu            sync.Mutex     // guard the following variables
	state         transportState // the state of underlying connection
	activeStreams map[uint32]*Stream
	// the per-stream outbound flow control window size set by the peer.
	streamSendQuota uint32

	// server-specific

	maxStreamID uint32 // max stream ID ever seen

	// client-specific

	// streamsQuota limits the max number of concurrent streams.
	streamsQuota *quotaPool
	target       string // server name/addr
	userAgent    string
	nextID       uint32 // the next stream ID to be used
	// errorChan is closed to notify the I/O error to the caller.
	errorChan chan struct{}
	// The scheme used: https if TLS is on, http otherwise.
	scheme string
	creds  []credentials.PerRPCCredentials
}

type TransportOptions struct {
	MaxStreams uint32
	PeerAddr   string
	Scheme     string
	AuthInfo   credentials.AuthInfo
	// UserAgent is the application user agent.
	UserAgent string
	// PerRPCCredentials stores the PerRPCCredentials required to issue RPCs.
	PerRPCCredentials []credentials.PerRPCCredentials
	HandleStreams     func(*Stream)
}

func newHTTP2Transport(conn net.Conn, isClient bool, opts *TransportOptions) (_ *http2Transport, err error) {
	if isClient {
		// Send connection preface to server.
		n, err := conn.Write(clientPreface)
		if err != nil {
			return nil, ConnectionErrorf("transport: %v", err)
		}
		if n != len(clientPreface) {
			return nil, ConnectionErrorf("transport: preface mismatch, wrote %d bytes; want %d", n, len(clientPreface))
		}
	} else {
		// Check the validity of client preface.
		preface := make([]byte, len(clientPreface))
		if _, err := io.ReadFull(conn, preface); err != nil {
			return nil, ConnectionErrorf("transport: failed to receive the preface from client: %v", err)
		}
		if !bytes.Equal(preface, clientPreface) {
			return nil, ConnectionErrorf("transport: received bogus greeting from client: %q", preface)
		}
	}
	framer := newFramer(conn)
	// Send initial settings as connection preface to client.
	var settings []http2.Setting
	// TODO(zhaoq): Have a better way to signal "no limit" because 0 is
	// permitted in the HTTP2 spec.
	maxStreams := opts.MaxStreams
	if maxStreams == 0 {
		maxStreams = math.MaxUint32
	} else {
		settings = append(settings, http2.Setting{http2.SettingMaxConcurrentStreams, maxStreams})
	}
	if initialWindowSize != defaultWindowSize {
		settings = append(settings, http2.Setting{http2.SettingInitialWindowSize, uint32(initialWindowSize)})
	}
	if err := framer.writeSettings(true, settings...); err != nil {
		return nil, ConnectionErrorf("transport: %v", err)
	}
	// Adjust the connection flow control window if needed.
	if delta := uint32(initialConnWindowSize - defaultWindowSize); delta > 0 {
		if err := framer.writeWindowUpdate(true, 0, delta); err != nil {
			return nil, ConnectionErrorf("transport: %v", err)
		}
	}
	var buf bytes.Buffer
	ua := primaryUA
	if opts.UserAgent != "" {
		ua = opts.UserAgent + " " + ua
	}
	var nextID uint32 = 2
	if isClient {
		// The client initiated stream id is odd starting from 1.
		nextID = 1
	}
	t := &http2Transport{
		isClient: isClient,

		conn:            conn,
		authInfo:        opts.AuthInfo,
		framer:          framer,
		hBuf:            &buf,
		hEnc:            hpack.NewEncoder(&buf),
		maxStreamsMe:    maxStreams,
		maxStreamsPeer:  math.MaxInt32,
		controlBuf:      newRecvBuffer(),
		fc:              &inFlow{limit: initialConnWindowSize},
		sendQuotaPool:   newQuotaPool(defaultWindowSize),
		state:           reachable,
		writableChan:    make(chan int, 1),
		shutdownChan:    make(chan struct{}),
		activeStreams:   make(map[uint32]*Stream),
		errorChan:       make(chan struct{}),
		streamSendQuota: defaultWindowSize,
		nextID:          nextID,
		userAgent:       ua,
		creds:           opts.PerRPCCredentials,
		target:          opts.PeerAddr,
		scheme:          opts.Scheme,
	}
	// Start the reader goroutine for incoming message. Each transport has
	// a dedicated goroutine which reads HTTP2 frame from network. Then it
	// dispatches the frame to the corresponding stream entity.
	if isClient || opts.HandleStreams != nil {
		go t.reader(opts.HandleStreams)
	}
	go t.controller()
	t.writableChan <- 0
	return t, nil
}

// newHTTP2Server constructs a ServerTransport based on HTTP2. ConnectionError is
// returned if something goes wrong.
func newHTTP2Server(conn net.Conn, maxStreams uint32, authInfo credentials.AuthInfo) (_ ServerTransport, err error) {
	return newHTTP2Transport(conn, false, &TransportOptions{
		MaxStreams: maxStreams,
		AuthInfo:   authInfo,
	})
}

// newHTTP2Client constructs a connected ClientTransport to addr based on HTTP2
// and starts to receive messages on it. Non-nil error returns if construction
// fails.
func newHTTP2Client(addr string, opts *ConnectOptions) (_ ClientTransport, err error) {
	if opts.Dialer == nil {
		// Set the default Dialer.
		opts.Dialer = func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("tcp", addr, timeout)
		}
	}
	scheme := "http"
	startT := time.Now()
	timeout := opts.Timeout
	conn, connErr := opts.Dialer(addr, timeout)
	if connErr != nil {
		return nil, ConnectionErrorf("transport: %v", connErr)
	}
	var authInfo credentials.AuthInfo
	if opts.TransportCredentials != nil {
		scheme = "https"
		if timeout > 0 {
			timeout -= time.Since(startT)
		}
		conn, authInfo, connErr = opts.TransportCredentials.ClientHandshake(addr, conn, timeout)
	}
	if connErr != nil {
		return nil, ConnectionErrorf("transport: %v", connErr)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	return newHTTP2Transport(conn, true, &TransportOptions{
		UserAgent:         opts.UserAgent,
		PerRPCCredentials: opts.PerRPCCredentials,
		AuthInfo:          authInfo,
		Scheme:            scheme,
		PeerAddr:          addr,
	})
}

func (t *http2Transport) getStream(f http2.Frame) (*Stream, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.activeStreams == nil {
		// The transport is closing.
		return nil, false
	}
	s, ok := t.activeStreams[f.Header().StreamID]
	if !ok {
		// The stream is already done.
		return nil, false
	}
	return s, true
}

// updateWindow adjusts the inbound quota for the stream and the transport.
// Window updates will deliver to the controller for sending when
// the cumulative quota exceeds the corresponding threshold.
func (t *http2Transport) updateWindow(s *Stream, n uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == streamDone {
		return
	}
	if w := t.fc.onRead(n); w > 0 {
		t.controlBuf.put(&windowUpdate{0, w})
	}
	if w := s.fc.onRead(n); w > 0 {
		t.controlBuf.put(&windowUpdate{s.id, w})
	}
}

func (t *http2Transport) handleData(f *http2.DataFrame) {
	size := len(f.Data())
	if err := t.fc.onData(uint32(size)); err != nil {
		t.notifyError(ConnectionErrorf("%v", err))
		return
	}
	// Select the right stream to dispatch.
	s, ok := t.getStream(f)
	if !ok {
		if w := t.fc.onRead(uint32(size)); w > 0 {
			t.controlBuf.put(&windowUpdate{0, w})
		}
		return
	}
	if size > 0 {
		s.mu.Lock()
		if s.state == streamDone {
			s.mu.Unlock()
			// The stream has been closed. Release the corresponding quota.
			if w := t.fc.onRead(uint32(size)); w > 0 {
				t.controlBuf.put(&windowUpdate{0, w})
			}
			return
		}
		if err := s.fc.onData(uint32(size)); err != nil {
			if s.st == nil {
				s.state = streamDone
				s.statusCode = codes.Internal
				s.statusDesc = err.Error()
			}
			s.mu.Unlock()
			if s.st == nil {
				s.write(recvMsg{err: io.EOF})
			} else {
				t.closeStream(s, 0)
			}
			t.controlBuf.put(&resetStream{s.id, http2.ErrCodeFlowControl})
			return
		}
		s.mu.Unlock()
		// TODO(bradfitz, zhaoq): A copy is required here because there is no
		// guarantee f.Data() is consumed before the arrival of next frame.
		// Can this copy be eliminated?
		data := make([]byte, size)
		copy(data, f.Data())
		s.write(recvMsg{data: data})
	}
	// The server has closed the stream without sending trailers.  Record that
	// the read direction is closed, and set the status appropriately.
	if f.FrameHeader.Flags.Has(http2.FlagDataEndStream) {
		s.mu.Lock()
		if s.state != streamDone {
			if s.state == streamWriteDone {
				s.state = streamDone
			} else {
				s.state = streamReadDone
			}
		}
		if s.st == nil {
			s.statusCode = codes.Internal
			s.statusDesc = "server closed the stream without sending trailers"
		}
		s.mu.Unlock()
		s.write(recvMsg{err: io.EOF})
	}
}

func (t *http2Transport) handleRSTStream(f *http2.RSTStreamFrame) {
	s, ok := t.getStream(f)
	if !ok {
		return
	}
	t.closeStream(s, f.ErrCode)
}

func (t *http2Transport) handleSettings(f *http2.SettingsFrame) {
	if f.IsAck() {
		return
	}
	var ss []http2.Setting
	f.ForeachSetting(func(s http2.Setting) error {
		ss = append(ss, s)
		return nil
	})
	// The settings will be applied once the ack is sent.
	t.controlBuf.put(&settings{ack: true, ss: ss})
}

func (t *http2Transport) handlePing(f *http2.PingFrame) {
	pingAck := &ping{ack: true}
	copy(pingAck.data[:], f.Data[:])
	t.controlBuf.put(pingAck)
}

func (t *http2Transport) handleWindowUpdate(f *http2.WindowUpdateFrame) {
	id := f.Header().StreamID
	incr := f.Increment
	if id == 0 {
		t.sendQuotaPool.add(int(incr))
		return
	}
	if s, ok := t.getStream(f); ok {
		s.sendQuotaPool.add(int(incr))
	}
}

func handleMalformedHTTP2(s *Stream, err error) {
	s.mu.Lock()
	if !s.headerDone {
		close(s.headerChan)
		s.headerDone = true
	}
	s.mu.Unlock()
	s.write(recvMsg{err: err})
}

// reader runs as a separate goroutine in charge of reading data from network
// connection.
//
// TODO(zhaoq): currently one reader per transport. Investigate whether this is
// optimal.
// TODO(zhaoq): Check the validity of the incoming frame sequence.
//
// TODO(dennwc): comments
// HandleStreams receives incoming streams using the given handler. This is
// typically run in a separate goroutine.
func (t *http2Transport) reader(handle func(*Stream)) {
	// Check the validity of server preface.
	frame, err := t.framer.readFrame()
	if err != nil {
		t.notifyError(err)
		return
	}
	sf, ok := frame.(*http2.SettingsFrame)
	if !ok {
		t.notifyError(ConnectionErrorf("invalid preface type %T, expected settings", frame))
		return
	}
	t.handleSettings(sf)

	// loop to keep reading incoming messages on this transport.
	for {
		frame, err := t.framer.readFrame()
		if err != nil {
			// Abort an active stream if the http2.Framer returns a
			// http2.StreamError. This can happen only if the server's response
			// is malformed http2.
			if se, ok := err.(http2.StreamError); ok {
				t.mu.Lock()
				s := t.activeStreams[se.StreamID]
				t.mu.Unlock()
				if s != nil {
					if s.st == nil {
						// use error detail to provide better err message
						handleMalformedHTTP2(s, StreamErrorf(http2ErrConvTab[se.Code], "%v", t.framer.errorDetail()))
					} else {
						t.closeStream(s, se.Code)
					}
				}
				if se.StreamID%2 != t.nextID%2 {
					t.controlBuf.put(&resetStream{se.StreamID, se.Code})
				}
				continue
			}
			// Transport error.
			t.notifyError(err)
			return
		}
		switch frame := frame.(type) {
		case *http2.MetaHeadersFrame:
			t.operateHeaders(frame, handle)
		case *http2.DataFrame:
			t.handleData(frame)
		case *http2.RSTStreamFrame:
			t.handleRSTStream(frame)
		case *http2.SettingsFrame:
			t.handleSettings(frame)
		case *http2.PingFrame:
			t.handlePing(frame)
		case *http2.WindowUpdateFrame:
			t.handleWindowUpdate(frame)
		case *http2.GoAwayFrame:
			if t.isClient {
				// TODO(zhaoq): GoAwayFrame handler to be implemented
			} else {
				break
			}
		default:
			grpclog.Printf("transport: http2Transport.HandleStreams got unhandled frame type %v.", frame)
		}
	}
}

// HandleStreams receives incoming streams using the given handler. This is
// typically run in a separate goroutine.
func (t *http2Transport) HandleStreams(handle func(*Stream)) {
	t.reader(handle)
}

// operateHeaders takes action on the decoded headers.
func (t *http2Transport) operateHeaders(frame *http2.MetaHeadersFrame, handle func(*Stream)) {
	if id := frame.Header().StreamID; id%2 == t.nextID%2 { // Response
		s, ok := t.getStream(frame)
		if !ok {
			return
		}
		var state decodeState
		for _, hf := range frame.Fields {
			state.processHeaderField(hf)
		}
		if state.err != nil {
			s.write(recvMsg{err: state.err})
			// Something wrong. Stops reading even when there is remaining.
			return
		}

		endStream := frame.StreamEnded()

		s.mu.Lock()
		if !endStream {
			s.recvCompress = state.encoding
		}
		if !s.headerDone {
			if !endStream && len(state.mdata) > 0 {
				s.header = state.mdata
			}
			close(s.headerChan)
			s.headerDone = true
		}
		if !endStream || s.state == streamDone {
			s.mu.Unlock()
			return
		}

		if len(state.mdata) > 0 {
			s.trailer = state.mdata
		}
		s.state = streamDone
		s.statusCode = state.statusCode
		s.statusDesc = state.statusDesc
		s.mu.Unlock()

		s.write(recvMsg{err: io.EOF})
	} else { // New request
		if id < t.maxStreamID {
			t.notifyError(ConnectionErrorf("received an illegal stream id: %v", id))
			return
		}
		t.maxStreamID = id
		buf := newRecvBuffer()
		s := &Stream{
			id:  frame.Header().StreamID,
			st:  t,
			buf: buf,
			fc:  &inFlow{limit: initialWindowSize},
		}

		var state decodeState
		for _, hf := range frame.Fields {
			state.processHeaderField(hf)
		}
		if err := state.err; err != nil {
			if se, ok := err.(StreamError); ok {
				t.controlBuf.put(&resetStream{s.id, statusCodeConvTab[se.Code]})
			}
			return
		}

		if frame.StreamEnded() {
			// s is just created by the caller. No lock needed.
			s.state = streamReadDone
		}
		s.recvCompress = state.encoding
		if state.timeoutSet {
			s.ctx, s.cancel = context.WithTimeout(context.TODO(), state.timeout)
		} else {
			s.ctx, s.cancel = context.WithCancel(context.TODO())
		}
		pr := &peer.Peer{
			Addr: t.conn.RemoteAddr(),
		}
		// Attach Auth info if there is any.
		if t.authInfo != nil {
			pr.AuthInfo = t.authInfo
		}
		s.ctx = peer.NewContext(s.ctx, pr)
		// Cache the current stream to the context so that the server application
		// can find out. Required when the server wants to send some metadata
		// back to the client (unary call only).
		s.ctx = newContextWithStream(s.ctx, s)
		// Attach the received metadata to the context.
		if len(state.mdata) > 0 {
			s.ctx = metadata.NewContext(s.ctx, state.mdata)
		}

		s.dec = &recvBufferReader{
			ctx:  s.ctx,
			recv: s.buf,
		}
		s.recvCompress = state.encoding
		s.method = state.method
		t.mu.Lock()
		if t.state != reachable {
			t.mu.Unlock()
			return
		}
		if uint32(len(t.activeStreams)) >= t.maxStreamsMe {
			t.mu.Unlock()
			t.controlBuf.put(&resetStream{s.id, http2.ErrCodeRefusedStream})
			return
		}
		s.sendQuotaPool = newQuotaPool(int(t.streamSendQuota))
		t.activeStreams[s.id] = s
		t.mu.Unlock()
		s.windowHandler = func(n int) {
			t.updateWindow(s, uint32(n))
		}
		if handle != nil {
			handle(s)
		} else {
			t.controlBuf.put(&resetStream{s.id, http2.ErrCodeRefusedStream})
		}
	}
}

func (t *http2Transport) applySettings(ss []http2.Setting) {
	for _, s := range ss {
		switch s.ID {
		case http2.SettingMaxConcurrentStreams:
			// TODO(zhaoq): This is a hack to avoid significant refactoring of the
			// code to deal with the unrealistic int32 overflow. Probably will try
			// to find a better way to handle this later.
			if s.Val > math.MaxInt32 {
				s.Val = math.MaxInt32
			}
			t.mu.Lock()
			reset := t.streamsQuota != nil
			if !reset {
				t.streamsQuota = newQuotaPool(int(s.Val) - len(t.activeStreams))
			}
			ms := t.maxStreamsPeer
			t.maxStreamsPeer = uint32(s.Val)
			t.mu.Unlock()
			if reset {
				t.streamsQuota.reset(int(s.Val) - int(ms))
			}
		case http2.SettingInitialWindowSize:
			t.mu.Lock()
			for _, stream := range t.activeStreams {
				// Adjust the sending quota for each stream.
				stream.sendQuotaPool.reset(int(s.Val - t.streamSendQuota))
			}
			t.streamSendQuota = s.Val
			t.mu.Unlock()
		}
	}
}

// controller running in a separate goroutine takes charge of sending control
// frames (e.g., window update, reset stream, setting, etc.) to the server.
func (t *http2Transport) controller() {
	for {
		select {
		case i := <-t.controlBuf.get():
			t.controlBuf.load()
			select {
			case <-t.writableChan:
				switch i := i.(type) {
				case *windowUpdate:
					t.framer.writeWindowUpdate(true, i.streamID, i.increment)
				case *settings:
					if i.ack {
						t.framer.writeSettingsAck(true)
						t.applySettings(i.ss)
					} else {
						t.framer.writeSettings(true, i.ss...)
					}
				case *resetStream:
					t.framer.writeRSTStream(true, i.streamID, i.code)
				case *flushIO:
					t.framer.flushWrite()
				case *ping:
					t.framer.writePing(true, i.ack, i.data)
				default:
					grpclog.Printf("transport: http2Transport.controller got unexpected item type %v\n", i)
				}
				t.writableChan <- 0
				continue
			case <-t.shutdownChan:
				return
			}
		case <-t.shutdownChan:
			return
		}
	}
}

func (t *http2Transport) newStream(ctx context.Context, callHdr *CallHdr) *Stream {
	// TODO(zhaoq): Handle uint32 overflow of Stream.id.
	s := &Stream{
		id:            t.nextID,
		method:        callHdr.Method,
		sendCompress:  callHdr.SendCompress,
		buf:           newRecvBuffer(),
		fc:            &inFlow{limit: initialWindowSize},
		sendQuotaPool: newQuotaPool(int(t.streamSendQuota)),
		headerChan:    make(chan struct{}),
	}
	t.nextID += 2
	s.windowHandler = func(n int) {
		t.updateWindow(s, uint32(n))
	}
	// Make a stream be able to cancel the pending operations by itself.
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.dec = &recvBufferReader{
		ctx:  s.ctx,
		recv: s.buf,
	}
	return s
}

// NewStream creates a stream and register it into the transport as "active"
// streams.
func (t *http2Transport) NewStream(ctx context.Context, callHdr *CallHdr) (_ *Stream, err error) {
	// Record the timeout value on the context.
	var timeout time.Duration
	if dl, ok := ctx.Deadline(); ok {
		timeout = dl.Sub(time.Now())
	}
	select {
	case <-ctx.Done():
		return nil, ContextErr(ctx.Err())
	default:
	}
	pr := &peer.Peer{
		Addr: t.conn.RemoteAddr(),
	}
	// Attach Auth info if there is any.
	if t.authInfo != nil {
		pr.AuthInfo = t.authInfo
	}
	ctx = peer.NewContext(ctx, pr)
	authData := make(map[string]string)
	for _, c := range t.creds {
		// Construct URI required to get auth request metadata.
		var port string
		if pos := strings.LastIndex(t.target, ":"); pos != -1 {
			// Omit port if it is the default one.
			if t.target[pos+1:] != "443" {
				port = ":" + t.target[pos+1:]
			}
		}
		pos := strings.LastIndex(callHdr.Method, "/")
		if pos == -1 {
			return nil, StreamErrorf(codes.InvalidArgument, "transport: malformed method name: %q", callHdr.Method)
		}
		audience := "https://" + callHdr.Host + port + callHdr.Method[:pos]
		data, err := c.GetRequestMetadata(ctx, audience)
		if err != nil {
			return nil, StreamErrorf(codes.InvalidArgument, "transport: %v", err)
		}
		for k, v := range data {
			authData[k] = v
		}
	}
	t.mu.Lock()
	if t.activeStreams == nil {
		t.mu.Unlock()
		return nil, ErrConnClosing
	}
	if t.state != reachable {
		t.mu.Unlock()
		return nil, ErrConnClosing
	}
	checkStreamsQuota := t.streamsQuota != nil
	t.mu.Unlock()
	if checkStreamsQuota {
		sq, err := wait(ctx, t.shutdownChan, t.streamsQuota.acquire())
		if err != nil {
			return nil, err
		}
		// Returns the quota balance back.
		if sq > 1 {
			t.streamsQuota.add(sq - 1)
		}
	}
	if _, err := wait(ctx, t.shutdownChan, t.writableChan); err != nil {
		// Return the quota back now because there is no stream returned to the caller.
		if _, ok := err.(StreamError); ok && checkStreamsQuota {
			t.streamsQuota.add(1)
		}
		return nil, err
	}
	t.mu.Lock()
	if t.state != reachable {
		t.mu.Unlock()
		return nil, ErrConnClosing
	}
	s := t.newStream(ctx, callHdr)
	t.activeStreams[s.id] = s

	// This stream is not counted when applySetings(...) initialize t.streamsQuota.
	// Reset t.streamsQuota to the right value.
	var reset bool
	if !checkStreamsQuota && t.streamsQuota != nil {
		reset = true
	}
	t.mu.Unlock()
	if reset {
		t.streamsQuota.reset(-1)
	}

	// HPACK encodes various headers. Note that once WriteField(...) is
	// called, the corresponding headers/continuation frame has to be sent
	// because hpack.Encoder is stateful.
	t.hBuf.Reset()
	t.hEnc.WriteField(hpack.HeaderField{Name: ":method", Value: "POST"})
	t.hEnc.WriteField(hpack.HeaderField{Name: ":scheme", Value: t.scheme})
	t.hEnc.WriteField(hpack.HeaderField{Name: ":path", Value: callHdr.Method})
	t.hEnc.WriteField(hpack.HeaderField{Name: ":authority", Value: callHdr.Host})
	t.hEnc.WriteField(hpack.HeaderField{Name: "content-type", Value: "application/grpc"})
	t.hEnc.WriteField(hpack.HeaderField{Name: "user-agent", Value: t.userAgent})
	t.hEnc.WriteField(hpack.HeaderField{Name: "te", Value: "trailers"})

	if callHdr.SendCompress != "" {
		t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-encoding", Value: callHdr.SendCompress})
	}
	if timeout > 0 {
		t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-timeout", Value: timeoutEncode(timeout)})
	}
	for k, v := range authData {
		// Capital header names are illegal in HTTP/2.
		k = strings.ToLower(k)
		t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: v})
	}
	var (
		hasMD      bool
		endHeaders bool
	)
	if md, ok := metadata.FromContext(ctx); ok {
		hasMD = true
		for k, v := range md {
			// HTTP doesn't allow you to set pseudoheaders after non pseudoheaders were set.
			if isReservedHeader(k) {
				continue
			}
			for _, entry := range v {
				t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: entry})
			}
		}
	}
	first := true
	// Sends the headers in a single batch even when they span multiple frames.
	for !endHeaders {
		size := t.hBuf.Len()
		if size > http2MaxFrameLen {
			size = http2MaxFrameLen
		} else {
			endHeaders = true
		}
		var flush bool
		if endHeaders && (hasMD || callHdr.Flush) {
			flush = true
		}
		if first {
			// Sends a HeadersFrame to server to start a new stream.
			p := http2.HeadersFrameParam{
				StreamID:      s.id,
				BlockFragment: t.hBuf.Next(size),
				EndStream:     false,
				EndHeaders:    endHeaders,
			}
			// Do a force flush for the buffered frames iff it is the last headers frame
			// and there is header metadata to be sent. Otherwise, there is flushing until
			// the corresponding data frame is written.
			err = t.framer.writeHeaders(flush, p)
			first = false
		} else {
			// Sends Continuation frames for the leftover headers.
			err = t.framer.writeContinuation(flush, s.id, endHeaders, t.hBuf.Next(size))
		}
		if err != nil {
			t.notifyError(err)
			return nil, ConnectionErrorf("transport: %v", err)
		}
	}
	t.writableChan <- 0
	return s, nil
}

// CloseStream clears the footprint of a stream when the stream is not needed any more.
// This must not be executed in reader's goroutine.
func (t *http2Transport) CloseStream(s *Stream, err error) {
	var updateStreams bool
	t.mu.Lock()
	if t.activeStreams == nil {
		t.mu.Unlock()
		return
	}
	if t.streamsQuota != nil {
		updateStreams = true
	}
	if t.state == draining && len(t.activeStreams) == 1 {
		// The transport is draining and s is the last live stream on t.
		t.mu.Unlock()
		t.Close()
		return
	}
	delete(t.activeStreams, s.id)
	t.mu.Unlock()
	if updateStreams {
		t.streamsQuota.add(1)
	}
	// In case stream sending and receiving are invoked in separate
	// goroutines (e.g., bi-directional streaming), the caller needs
	// to call cancel on the stream to interrupt the blocking on
	// other goroutines.
	s.cancel()
	s.mu.Lock()
	if q := s.fc.resetPendingData(); q > 0 {
		if n := t.fc.onRead(q); n > 0 {
			t.controlBuf.put(&windowUpdate{0, n})
		}
	}
	if s.state == streamDone {
		s.mu.Unlock()
		return
	}
	if !s.headerDone {
		close(s.headerChan)
		s.headerDone = true
	}
	s.state = streamDone
	s.mu.Unlock()
	if _, ok := err.(StreamError); ok {
		t.controlBuf.put(&resetStream{s.id, http2.ErrCodeCancel})
	}
}

func (t *http2Transport) writeHeaders(s *Stream, b *bytes.Buffer, endStream bool) error {
	first := true
	endHeaders := false
	var err error
	// Sends the headers in a single batch.
	for !endHeaders {
		size := t.hBuf.Len()
		if size > http2MaxFrameLen {
			size = http2MaxFrameLen
		} else {
			endHeaders = true
		}
		if first {
			p := http2.HeadersFrameParam{
				StreamID:      s.id,
				BlockFragment: b.Next(size),
				EndStream:     endStream,
				EndHeaders:    endHeaders,
			}
			err = t.framer.writeHeaders(endHeaders, p)
			first = false
		} else {
			err = t.framer.writeContinuation(endHeaders, s.id, endHeaders, b.Next(size))
		}
		if err != nil {
			t.notifyError(err)
			return ConnectionErrorf("transport: %v", err)
		}
	}
	return nil
}

// WriteHeader sends the header metedata md back to the client.
func (t *http2Transport) WriteHeader(s *Stream, md metadata.MD) error {
	s.mu.Lock()
	if s.headerOk || s.state == streamDone {
		s.mu.Unlock()
		return ErrIllegalHeaderWrite
	}
	s.headerOk = true
	s.mu.Unlock()
	if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
		return err
	}
	t.hBuf.Reset()
	t.hEnc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
	t.hEnc.WriteField(hpack.HeaderField{Name: "content-type", Value: "application/grpc"})
	if s.sendCompress != "" {
		t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-encoding", Value: s.sendCompress})
	}
	for k, v := range md {
		if isReservedHeader(k) {
			// Clients don't tolerate reading restricted headers after some non restricted ones were sent.
			continue
		}
		for _, entry := range v {
			t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: entry})
		}
	}
	if err := t.writeHeaders(s, t.hBuf, false); err != nil {
		return err
	}
	t.writableChan <- 0
	return nil
}

// WriteStatus sends stream status to the client and terminates the stream.
// There is no further I/O operations being able to perform on this stream.
// TODO(zhaoq): Now it indicates the end of entire stream. Revisit if early
// OK is adopted.
func (t *http2Transport) WriteStatus(s *Stream, statusCode codes.Code, statusDesc string) error {
	var headersSent bool
	s.mu.Lock()
	if s.state == streamDone {
		s.mu.Unlock()
		return nil
	}
	if s.headerOk {
		headersSent = true
	}
	s.mu.Unlock()
	if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
		return err
	}
	t.hBuf.Reset()
	if !headersSent {
		t.hEnc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
		t.hEnc.WriteField(hpack.HeaderField{Name: "content-type", Value: "application/grpc"})
	}
	t.hEnc.WriteField(
		hpack.HeaderField{
			Name:  "grpc-status",
			Value: strconv.Itoa(int(statusCode)),
		})
	t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-message", Value: statusDesc})
	// Attach the trailer metadata.
	for k, v := range s.trailer {
		// Clients don't tolerate reading restricted headers after some non restricted ones were sent.
		if isReservedHeader(k) {
			continue
		}
		for _, entry := range v {
			t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: entry})
		}
	}
	if err := t.writeHeaders(s, t.hBuf, true); err != nil {
		t.notifyError(err)
		return err
	}
	t.closeStream(s, http2.ErrCodeStreamClosed) // TODO(dennwc): proper code
	t.writableChan <- 0
	return nil
}

// Write formats the data into HTTP2 data frame(s) and sends it out. The caller
// should proceed only if Write returns nil.
// TODO(zhaoq): opts.Delay is ignored in this implementation. Support it later
// if it improves the performance.
//
// TODO(dennwc)
// Write converts the data into HTTP2 data frame and sends it out. Non-nil error
// is returns if it fails (e.g., framing error, transport error).
func (t *http2Transport) Write(s *Stream, data []byte, opts *Options) error {
	// TODO(zhaoq): Support multi-writers for a single stream.
	response := s.st != nil
	if response { // Response stream - must send status code.
		// Write headers if not already done that.
		if err := t.WriteHeader(s, nil); err != nil && err != ErrIllegalHeaderWrite {
			return err
		}
	}
	r := bytes.NewBuffer(data)
	for {
		if response && r.Len() == 0 {
			break
		}
		var p []byte
		if r.Len() > 0 {
			size := http2MaxFrameLen
			s.sendQuotaPool.add(0)
			// Wait until the stream has some quota to send the data.
			sq, err := wait(s.ctx, t.shutdownChan, s.sendQuotaPool.acquire())
			if err != nil {
				return err
			}
			t.sendQuotaPool.add(0)
			// Wait until the transport has some quota to send the data.
			tq, err := wait(s.ctx, t.shutdownChan, t.sendQuotaPool.acquire())
			if err != nil {
				if _, ok := err.(StreamError); ok {
					t.sendQuotaPool.cancel()
				}
				return err
			}
			if sq < size {
				size = sq
			}
			if tq < size {
				size = tq
			}
			p = r.Next(size)
			ps := len(p)
			if ps < sq {
				// Overbooked stream quota. Return it back.
				s.sendQuotaPool.add(sq - ps)
			}
			if ps < tq {
				// Overbooked transport quota. Return it back.
				t.sendQuotaPool.add(tq - ps)
			}
		}
		var endStream bool
		if s.st == nil && opts.Last && r.Len() == 0 {
			endStream = true
		}
		// Indicate there is a writer who is about to write a data frame.
		t.framer.adjustNumWriters(1)
		// Got some quota. Try to acquire writing privilege on the
		// transport.
		if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
			if _, ok := err.(StreamError); ok {
				// Return the connection quota back.
				t.sendQuotaPool.add(len(p))
			}
			if t.framer.adjustNumWriters(-1) == 0 {
				// This writer is the last one in this batch and has the
				// responsibility to flush the buffered frames. It queues
				// a flush request to controlBuf instead of flushing directly
				// in order to avoid the race with other writing or flushing.
				t.controlBuf.put(&flushIO{})
			}
			return err
		}
		select {
		case <-s.ctx.Done():
			t.sendQuotaPool.add(len(p))
			if t.framer.adjustNumWriters(-1) == 0 {
				t.controlBuf.put(&flushIO{})
			}
			t.writableChan <- 0
			return ContextErr(s.ctx.Err())
		default:
		}
		var forceFlush bool
		if r.Len() == 0 && t.framer.adjustNumWriters(0) == 1 && !opts.Last {
			// Do a force flush iff this is last frame for the entire gRPC message
			// and the caller is the only writer at this moment.
			forceFlush = true
		}
		// If WriteData fails, all the pending streams will be handled
		// by http2Client.Close(). No explicit CloseStream() needs to be
		// invoked.
		if err := t.framer.writeData(forceFlush, s.id, endStream, p); err != nil {
			t.notifyError(err)
			return ConnectionErrorf("transport: %v", err)
		}
		if t.framer.adjustNumWriters(-1) == 0 {
			t.framer.flushWrite()
		}
		t.writableChan <- 0
		if !response && r.Len() == 0 {
			break
		}
	}
	if !response {
		if !opts.Last {
			return nil
		}
		s.mu.Lock()
		if s.state != streamDone {
			if s.state == streamReadDone {
				s.state = streamDone
			} else {
				s.state = streamWriteDone
			}
		}
		s.mu.Unlock()
	}
	return nil
}

// TODO(dennwc): comments
// Close starts shutting down the http2Server transport.
// TODO(zhaoq): Now the destruction is not blocked on any pending streams. This
// could cause some resource issue. Revisit this later.
//
// Close kicks off the shutdown process of the transport. This should be called
// only once on a transport. Once it is called, the transport should not be
// accessed any more.
func (t *http2Transport) Close() (err error) {
	t.mu.Lock()
	if t.state == reachable {
		close(t.errorChan)
	}
	if t.state == closing {
		t.mu.Unlock()
		if t.isClient {
			return
		} else {
			return errors.New("transport: Close() was already called")
		}
	}
	t.state = closing
	streams := t.activeStreams
	t.activeStreams = nil
	t.mu.Unlock()
	close(t.shutdownChan)
	err = t.conn.Close()
	// Notify/cancel all active streams.
	for _, s := range streams {
		if s.st == nil {
			s.mu.Lock()
			if !s.headerDone {
				close(s.headerChan)
				s.headerDone = true
			}
			s.mu.Unlock()
			s.write(recvMsg{err: ErrConnClosing})
		} else {
			s.cancel()
		}
	}
	return
}

// closeStream clears the footprint of a stream when the stream is not needed
// any more.
func (t *http2Transport) closeStream(s *Stream, errCode http2.ErrCode) {
	ours := s.st == nil
	if !ours {
		t.mu.Lock()
		delete(t.activeStreams, s.id)
		t.mu.Unlock()
		// In case stream sending and receiving are invoked in separate
		// goroutines (e.g., bi-directional streaming), cancel needs to be
		// called to interrupt the potential blocking on other goroutines.
		s.cancel()
	}
	s.mu.Lock()
	if !ours {
		if q := s.fc.resetPendingData(); q > 0 {
			if w := t.fc.onRead(q); w > 0 {
				t.controlBuf.put(&windowUpdate{0, w})
			}
		}
	}
	if s.state == streamDone {
		s.mu.Unlock()
		return
	}
	s.state = streamDone
	if ours {
		if !s.headerDone {
			close(s.headerChan)
			s.headerDone = true
		}
		var ok bool
		s.statusCode, ok = http2ErrConvTab[errCode]
		if !ok {
			grpclog.Println("transport: http2Transport.handleRSTStream found no mapped gRPC status for the received http2 error ", errCode)
			s.statusCode = codes.Unknown
		}
	}
	s.mu.Unlock()
	if ours {
		s.write(recvMsg{err: io.EOF})
	}
}

func (t *http2Transport) RemoteAddr() net.Addr {
	return t.conn.RemoteAddr()
}

func (t *http2Transport) Error() <-chan struct{} {
	return t.errorChan
}

func (t *http2Transport) notifyError(err error) {
	if t.isClient {
		t.mu.Lock()
		defer t.mu.Unlock()
		// make sure t.errorChan is closed only once.
		if t.state == reachable {
			t.state = unreachable
			close(t.errorChan)
			grpclog.Printf("transport: http2Client.notifyError got notified that the client transport was broken %v.", err)
		}
	} else {
		grpclog.Printf("transport: http2Server: transport was broken: %v", err)
		t.Close()
	}
}

func (t *http2Transport) GracefulClose() error {
	t.mu.Lock()
	if t.state == closing {
		t.mu.Unlock()
		return nil
	}
	if t.state == draining {
		t.mu.Unlock()
		return nil
	}
	t.state = draining
	active := len(t.activeStreams)
	t.mu.Unlock()
	if active == 0 {
		return t.Close()
	}
	return nil
}
