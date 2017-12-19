/*
 *
 * Copyright 2014 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package transport defines and implements message oriented communication
// channel to complete various transactions (e.g., an RPC).  It is meant for
// grpc-internal usage and is not intended to be imported directly by users.
package transport // import "google.golang.org/grpc/transport"

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// recvMsg represents the received msg from the transport. All transport
// protocol specific info has been removed.
type recvMsg struct {
	data []byte
	// nil: received some data
	// io.EOF: stream is completed. data is nil.
	// other non-nil error: transport failure. data is nil.
	err error
}

//type recvMsgList struct {
//	msg  recvMsg
//	next *recvMsgList
//}
//
//type recvBuffer struct {
//	ch chan struct{}
//
//	mu   sync.Mutex
//	head *recvMsgList
//	tail *recvMsgList
//}
//
//func newRecvBuffer() *recvBuffer {
//	return &recvBuffer{ch: make(chan struct{}, 1)}
//}
//
//func (r *recvBuffer) put(msg recvMsg) {
//	rl := &recvMsgList{msg: msg}
//	var wakeUp bool
//	r.mu.Lock()
//	if r.head == nil {
//		r.head, r.tail = rl, rl
//		wakeUp = true
//	} else {
//		r.tail.next = rl
//		r.tail = rl
//	}
//	r.mu.Unlock()
//	if wakeUp {
//		select {
//		case r.ch <- struct{}{}:
//		default:
//			panic(fmt.Errorf("This shouldn't have happened!"))
//		}
//	}
//}
//
//func (r *recvBuffer) get() *recvMsgList {
//	r.mu.Lock()
//	h := r.head
//	r.head, r.tail = nil, nil
//	r.mu.Unlock()
//	return h
//}

// recvBuffer is an unbounded channel of recvMsg structs.
// Note recvBuffer differs from controlBuffer only in that recvBuffer
// holds a channel of only recvMsg structs instead of objects implementing "item" interface.
// recvBuffer is written to much more often than
// controlBuffer and using strict recvMsg structs helps avoid allocation in "recvBuffer.put"
type recvBuffer struct {
	c       chan recvMsg
	mu      sync.Mutex
	backlog []recvMsg
}

func newRecvBuffer() *recvBuffer {
	b := &recvBuffer{
		c: make(chan recvMsg, 1),
	}
	return b
}

func (b *recvBuffer) put(r recvMsg) {
	b.mu.Lock()
	if len(b.backlog) == 0 {
		select {
		case b.c <- r:
			b.mu.Unlock()
			return
		default:
		}
	}
	b.backlog = append(b.backlog, r)
	b.mu.Unlock()
}

func (b *recvBuffer) load() {
	b.mu.Lock()
	if len(b.backlog) > 0 {
		select {
		case b.c <- b.backlog[0]:
			b.backlog[0] = recvMsg{}
			b.backlog = b.backlog[1:]
		default:
		}
	}
	b.mu.Unlock()
}

// get returns the channel that receives a recvMsg in the buffer.
//
// Upon receipt of a recvMsg, the caller should call load to send another
// recvMsg onto the channel if there is any.
func (b *recvBuffer) get() <-chan recvMsg {
	return b.c
}

//
// recvBufferReader implements io.Reader interface to read the data from
// recvBuffer.
type recvBufferReader struct {
	ctxDone <-chan struct{}
	goAway  chan struct{}
	recv    *recvBuffer
	//rl      *recvMsgList
	last []byte // Stores the remaining data in the previous calls.
	err  error
}

// Read reads the next len(p) bytes from last. If last is drained, it tries to
// read additional data from recv. It blocks if there no additional data available
// in recv. If Read returns any non-nil error, it will continue to return that error.
func (r *recvBufferReader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	n, r.err = r.read(p)
	return n, r.err
}

func (r *recvBufferReader) read(p []byte) (n int, err error) {
	if r.last != nil && len(r.last) > 0 {
		// Read remaining data left in last call.
		copied := copy(p, r.last)
		r.last = r.last[copied:]
		return copied, nil
	}
	select {
	case <-r.ctxDone:
		return 0, errStreamClosing
	case <-r.goAway:
		return 0, errStreamDrain
	case m := <-r.recv.get():
		r.recv.load()
		if m.err != nil {
			return 0, m.err
		}
		copied := copy(p, m.data)
		r.last = m.data[copied:]
		return copied, nil
	}
}

//func (r *recvBufferReader) read(p []byte) (n int, err error) {
//	if n, err = r.fillSlice(p); err != nil || n != 0 {
//		return n, err
//	}
//	select {
//	case <-r.ctxDone:
//		return 0, errStreamClosing
//	case <-r.goAway:
//		return 0, errStreamDrain
//	case <-r.recv.ch:
//		r.rl = r.recv.get()
//		return r.fillSlice(p)
//	}
//}
//
//func (r *recvBufferReader) fillSlice(p []byte) (n int, err error) {
//	for {
//		if len(p) == n || r.rl == nil {
//			return n, nil
//		}
//		if err = r.rl.msg.err; err != nil {
//			return 0, err
//		}
//		if len(r.rl.msg.data) == 0 {
//			r.rl = r.rl.next
//			continue
//		}
//		remaining := len(p) - n
//		if remaining > len(r.rl.msg.data) {
//			remaining = len(r.rl.msg.data)
//		}
//		n += copy(p[n:], r.rl.msg.data[:remaining])
//		r.rl.msg.data = r.rl.msg.data[remaining:]
//	}
//}

type streamState uint8

const (
	streamActive    streamState = iota
	streamWriteDone             // EndStream sent
	streamReadDone              // EndStream received
	streamDone                  // the entire stream is finished.
)

// Stream represents an RPC in the transport layer.
type Stream struct {
	id           uint32
	st           ServerTransport    // nil for client side Stream
	ctx          context.Context    // the associated context of the stream
	cancel       context.CancelFunc // always nil for client side Stream
	done         chan struct{}      // closed when the final status arrives
	goAway       chan struct{}      // closed when a GOAWAY control message is received
	msgWritten   chan struct{}      // signal stream that message was written on wire.
	method       string             // the associated RPC method of the stream
	recvCompress string
	sendCompress string
	buf          *recvBuffer
	trReader     io.Reader
	fc           *inFlow
	recvQuota    uint32
	waiters      *waiters
	wq           *writeQuota

	// Callback to state application's intentions to read data. This
	// is used to adjust flow control, if needed.
	requestRead func(int)

	headerChan chan struct{} // closed to indicate the end of header metadata.
	headerDone bool          // set when headerChan is closed. Used to avoid closing headerChan multiple times.
	header     metadata.MD   // the received header metadata.
	trailer    metadata.MD   // the key-value map of trailer metadata.

	mu       sync.RWMutex // guard the following
	headerOk bool         // becomes true from the first header is about to send
	state    streamState

	status *status.Status // the status error received from the server

	rstStream bool          // indicates whether a RST_STREAM frame needs to be sent
	rstError  http2.ErrCode // the error that needs to be sent along with the RST_STREAM frame

	bytesReceived bool // indicates whether any bytes have been received on this stream
	unprocessed   bool // set if the server sends a refused stream or GOAWAY including this stream
}

// RecvCompress returns the compression algorithm applied to the inbound
// message. It is empty string if there is no compression applied.
func (s *Stream) RecvCompress() string {
	if s.headerChan != nil {
		<-s.headerChan
	}
	return s.recvCompress
}

// SetSendCompress sets the compression algorithm to the stream.
func (s *Stream) SetSendCompress(str string) {
	s.sendCompress = str
}

// Done returns a chanel which is closed when it receives the final status
// from the server.
func (s *Stream) Done() <-chan struct{} {
	return s.done
}

// GoAway returns a channel which is closed when the server sent GoAways signal
// before this stream was initiated.
func (s *Stream) GoAway() <-chan struct{} {
	return s.goAway
}

// Header acquires the key-value pairs of header metadata once it
// is available. It blocks until i) the metadata is ready or ii) there is no
// header metadata or iii) the stream is canceled/expired.
func (s *Stream) Header() (metadata.MD, error) {
	var err error
	select {
	case <-s.ctx.Done():
		err = ContextErr(s.ctx.Err())
	case <-s.goAway:
		err = errStreamDrain
	case <-s.headerChan:
		return s.header.Copy(), nil
	}
	// Even if the stream is closed, header is returned if available.
	select {
	case <-s.headerChan:
		return s.header.Copy(), nil
	default:
	}
	return nil, err
}

// Trailer returns the cached trailer metedata. Note that if it is not called
// after the entire stream is done, it could return an empty MD. Client
// side only.
func (s *Stream) Trailer() metadata.MD {
	s.mu.RLock()
	c := s.trailer.Copy()
	s.mu.RUnlock()
	return c
}

// ServerTransport returns the underlying ServerTransport for the stream.
// The client side stream always returns nil.
func (s *Stream) ServerTransport() ServerTransport {
	return s.st
}

// Context returns the context of the stream.
func (s *Stream) Context() context.Context {
	return s.ctx
}

// Method returns the method for the stream.
func (s *Stream) Method() string {
	return s.method
}

// Status returns the status received from the server.
func (s *Stream) Status() *status.Status {
	return s.status
}

// SetHeader sets the header metadata. This can be called multiple times.
// Server side only.
func (s *Stream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	if s.headerOk || s.state == streamDone {
		s.mu.Unlock()
		return ErrIllegalHeaderWrite
	}
	if md.Len() == 0 {
		s.mu.Unlock()
		return nil
	}
	s.header = metadata.Join(s.header, md)
	s.mu.Unlock()
	return nil
}

// SetTrailer sets the trailer metadata which will be sent with the RPC status
// by the server. This can be called multiple times. Server side only.
func (s *Stream) SetTrailer(md metadata.MD) error {
	if md.Len() == 0 {
		return nil
	}
	s.mu.Lock()
	s.trailer = metadata.Join(s.trailer, md)
	s.mu.Unlock()
	return nil
}

func (s *Stream) write(m recvMsg) {
	s.buf.put(m)
}

// Read reads all p bytes from the wire for this stream.
func (s *Stream) Read(p []byte) (n int, err error) {
	// Don't request a read if there was an error earlier
	if er := s.trReader.(*transportReader).er; er != nil {
		return 0, er
	}
	s.requestRead(len(p))
	return io.ReadFull(s.trReader, p)
}

// tranportReader reads all the data available for this Stream from the transport and
// passes them into the decoder, which converts them into a gRPC message stream.
// The error is io.EOF when the stream is done or another non-nil error if
// the stream broke.
type transportReader struct {
	reader io.Reader
	// The handler to control the window update procedure for both this
	// particular stream and the associated transport.
	windowHandler func(int)
	er            error
}

func (t *transportReader) Read(p []byte) (n int, err error) {
	n, err = t.reader.Read(p)
	if err != nil {
		t.er = err
		return
	}
	t.windowHandler(n)
	return
}

// finish sets the stream's state and status, and closes the done channel.
// s.mu must be held by the caller.  st must always be non-nil.
func (s *Stream) finish(st *status.Status) {
	s.status = st
	s.state = streamDone
	close(s.done)
}

// BytesReceived indicates whether any bytes have been received on this stream.
func (s *Stream) BytesReceived() bool {
	s.mu.Lock()
	br := s.bytesReceived
	s.mu.Unlock()
	return br
}

// Unprocessed indicates whether the server did not process this stream --
// i.e. it sent a refused stream or GOAWAY including this stream ID.
func (s *Stream) Unprocessed() bool {
	s.mu.Lock()
	br := s.unprocessed
	s.mu.Unlock()
	return br
}

// GoString is implemented by Stream so context.String() won't
// race when printing %#v.
func (s *Stream) GoString() string {
	return fmt.Sprintf("<stream: %p, %v>", s, s.method)
}

// The key to save transport.Stream in the context.
type streamKey struct{}

// newContextWithStream creates a new context from ctx and attaches stream
// to it.
func newContextWithStream(ctx context.Context, stream *Stream) context.Context {
	return context.WithValue(ctx, streamKey{}, stream)
}

// StreamFromContext returns the stream saved in ctx.
func StreamFromContext(ctx context.Context) (s *Stream, ok bool) {
	s, ok = ctx.Value(streamKey{}).(*Stream)
	return
}

// state of transport
type transportState int

const (
	reachable transportState = iota
	closing
	draining
)

// ServerConfig consists of all the configurations to establish a server transport.
type ServerConfig struct {
	MaxStreams            uint32
	AuthInfo              credentials.AuthInfo
	InTapHandle           tap.ServerInHandle
	StatsHandler          stats.Handler
	KeepaliveParams       keepalive.ServerParameters
	KeepalivePolicy       keepalive.EnforcementPolicy
	InitialWindowSize     int32
	InitialConnWindowSize int32
	WriteBufferSize       int
	ReadBufferSize        int
}

// NewServerTransport creates a ServerTransport with conn or non-nil error
// if it fails.
func NewServerTransport(protocol string, conn net.Conn, config *ServerConfig) (ServerTransport, error) {
	return newHTTP2Server(conn, config)
}

// ConnectOptions covers all relevant options for communicating with the server.
type ConnectOptions struct {
	// UserAgent is the application user agent.
	UserAgent string
	// Authority is the :authority pseudo-header to use. This field has no effect if
	// TransportCredentials is set.
	Authority string
	// Dialer specifies how to dial a network address.
	Dialer func(context.Context, string) (net.Conn, error)
	// FailOnNonTempDialError specifies if gRPC fails on non-temporary dial errors.
	FailOnNonTempDialError bool
	// PerRPCCredentials stores the PerRPCCredentials required to issue RPCs.
	PerRPCCredentials []credentials.PerRPCCredentials
	// TransportCredentials stores the Authenticator required to setup a client connection.
	TransportCredentials credentials.TransportCredentials
	// KeepaliveParams stores the keepalive parameters.
	KeepaliveParams keepalive.ClientParameters
	// StatsHandler stores the handler for stats.
	StatsHandler stats.Handler
	// InitialWindowSize sets the initial window size for a stream.
	InitialWindowSize int32
	// InitialConnWindowSize sets the initial window size for a connection.
	InitialConnWindowSize int32
	// WriteBufferSize sets the size of write buffer which in turn determines how much data can be batched before it's written on the wire.
	WriteBufferSize int
	// ReadBufferSize sets the size of read buffer, which in turn determines how much data can be read at most for one read syscall.
	ReadBufferSize int
}

// TargetInfo contains the information of the target such as network address and metadata.
type TargetInfo struct {
	Addr      string
	Metadata  interface{}
	Authority string
}

// NewClientTransport establishes the transport with the required ConnectOptions
// and returns it to the caller.
func NewClientTransport(ctx context.Context, target TargetInfo, opts ConnectOptions, timeout time.Duration) (ClientTransport, error) {
	return newHTTP2Client(ctx, target, opts, timeout)
}

// Options provides additional hints and information for message
// transmission.
type Options struct {
	// Last indicates whether this write is the last piece for
	// this stream.
	Last bool

	// Delay is a hint to the transport implementation for whether
	// the data could be buffered for a batching write. The
	// transport implementation may ignore the hint.
	Delay bool
}

// CallHdr carries the information of a particular RPC.
type CallHdr struct {
	// Host specifies the peer's host.
	Host string

	// Method specifies the operation to perform.
	Method string

	// SendCompress specifies the compression algorithm applied on
	// outbound message.
	SendCompress string

	// Creds specifies credentials.PerRPCCredentials for a call.
	Creds credentials.PerRPCCredentials

	// Flush indicates whether a new stream command should be sent
	// to the peer without waiting for the first data. This is
	// only a hint.
	// If it's true, the transport may modify the flush decision
	// for performance purposes.
	// If it's false, new stream will never be flushed.
	Flush bool
}

// ClientTransport is the common interface for all gRPC client-side transport
// implementations.
type ClientTransport interface {
	// Close tears down this transport. Once it returns, the transport
	// should not be accessed any more. The caller must make sure this
	// is called only once.
	Close() error

	// GracefulClose starts to tear down the transport. It stops accepting
	// new RPCs and wait the completion of the pending RPCs.
	GracefulClose() error

	// Write sends the data for the given stream. A nil stream indicates
	// the write is to be performed on the transport as a whole.
	Write(s *Stream, hdr []byte, data []byte, opts *Options) error

	// NewStream creates a Stream for an RPC.
	NewStream(ctx context.Context, callHdr *CallHdr) (*Stream, error)

	// CloseStream clears the footprint of a stream when the stream is
	// not needed any more. The err indicates the error incurred when
	// CloseStream is called. Must be called when a stream is finished
	// unless the associated transport is closing.
	CloseStream(stream *Stream, err error)

	// Error returns a channel that is closed when some I/O error
	// happens. Typically the caller should have a goroutine to monitor
	// this in order to take action (e.g., close the current transport
	// and create a new one) in error case. It should not return nil
	// once the transport is initiated.
	Error() <-chan struct{}

	// GoAway returns a channel that is closed when ClientTransport
	// receives the draining signal from the server (e.g., GOAWAY frame in
	// HTTP/2).
	GoAway() <-chan struct{}

	// GetGoAwayReason returns the reason why GoAway frame was received.
	GetGoAwayReason() GoAwayReason
}

// ServerTransport is the common interface for all gRPC server-side transport
// implementations.
//
// Methods may be called concurrently from multiple goroutines, but
// Write methods for a given Stream will be called serially.
type ServerTransport interface {
	// HandleStreams receives incoming streams using the given handler.
	HandleStreams(func(*Stream), func(context.Context, string) context.Context)

	// WriteHeader sends the header metadata for the given stream.
	// WriteHeader may not be called on all streams.
	WriteHeader(s *Stream, md metadata.MD) error

	// Write sends the data for the given stream.
	// Write may not be called on all streams.
	Write(s *Stream, hdr []byte, data []byte, opts *Options) error

	// WriteStatus sends the status of a stream to the client.  WriteStatus is
	// the final call made on a stream and always occurs.
	WriteStatus(s *Stream, st *status.Status) error

	// Close tears down the transport. Once it is called, the transport
	// should not be accessed any more. All the pending streams and their
	// handlers will be terminated asynchronously.
	Close() error

	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr

	// Drain notifies the client this ServerTransport stops accepting new RPCs.
	Drain()
}

// streamErrorf creates an StreamError with the specified error code and description.
func streamErrorf(c codes.Code, format string, a ...interface{}) StreamError {
	return StreamError{
		Code: c,
		Desc: fmt.Sprintf(format, a...),
	}
}

// connectionErrorf creates an ConnectionError with the specified error description.
func connectionErrorf(temp bool, e error, format string, a ...interface{}) ConnectionError {
	return ConnectionError{
		Desc: fmt.Sprintf(format, a...),
		temp: temp,
		err:  e,
	}
}

// ConnectionError is an error that results in the termination of the
// entire connection and the retry of all the active streams.
type ConnectionError struct {
	Desc string
	temp bool
	err  error
}

func (e ConnectionError) Error() string {
	return fmt.Sprintf("connection error: desc = %q", e.Desc)
}

// Temporary indicates if this connection error is temporary or fatal.
func (e ConnectionError) Temporary() bool {
	return e.temp
}

// Origin returns the original error of this connection error.
func (e ConnectionError) Origin() error {
	// Never return nil error here.
	// If the original error is nil, return itself.
	if e.err == nil {
		return e
	}
	return e.err
}

var (
	// ErrConnClosing indicates that the transport is closing.
	ErrConnClosing = connectionErrorf(true, nil, "transport is closing")
	// errStreamDrain indicates that the stream is rejected by the server because
	// the server stops accepting new RPCs.
	// TODO: delete this error; it is no longer necessary.
	errStreamDrain = streamErrorf(codes.Unavailable, "the server stops accepting new RPCs")
	// errStreamClosing indicates that the context of stream was either cancelled
	// or expired.
	errStreamClosing = streamErrorf(codes.Unavailable, "the stream is closing")
	// StatusGoAway indicates that the server sent a GOAWAY that included this
	// stream's ID in unprocessed RPCs.
	statusGoAway = status.New(codes.Unavailable, "the server stopped accepting new RPCs")
)

// TODO: See if we can replace StreamError with status package errors.

// StreamError is an error that only affects one stream within a connection.
type StreamError struct {
	Code codes.Code
	Desc string
}

func (e StreamError) Error() string {
	return fmt.Sprintf("stream error: code = %s desc = %q", e.Code, e.Desc)
}

// waiters are passed to quotaPool get methods to
// wait on in addition to waiting on quota.
type waiters struct {
	trDone <-chan struct{}
	// Following are stream-specific chans
	strDone <-chan struct{}
	done    chan struct{}
	goAway  chan struct{}
}

// GoAwayReason contains the reason for the GoAway frame received.
type GoAwayReason uint8

const (
	// GoAwayInvalid indicates that no GoAway frame is received.
	GoAwayInvalid GoAwayReason = 0
	// GoAwayNoReason is the default value when GoAway frame is received.
	GoAwayNoReason GoAwayReason = 1
	// GoAwayTooManyPings indicates that a GoAway frame with
	// ErrCodeEnhanceYourCalm was received and that the debug data said
	// "too_many_pings".
	GoAwayTooManyPings GoAwayReason = 2
)

type outStreamState int

const (
	active outStreamState = iota
	empty
	notStartedYet
	waitingOnStreamQuota
)

type outStream struct {
	st               outStreamState
	itl              *itemList
	bytesOutStanding int
	wq               *writeQuota

	next *outStream
	prev *outStream
}

func (s *outStream) deleteSelf() {
	if s.prev != nil {
		s.prev.next = s.next
	}
	if s.next != nil {
		s.next.prev = s.prev
	}
	s.next, s.prev = nil, nil
}

type outStreamList struct {
	// Following are sentinal objects that mark the
	// begining and end of the list. They do not
	// contain any item lists. All valid objects are
	// inserted in between them.
	head *outStream
	tail *outStream
}

func newOutStreamList() *outStreamList {
	head, tail := new(outStream), new(outStream)
	head.next = tail
	tail.prev = head
	return &outStreamList{
		head: head,
		tail: tail,
	}
}

//func (l *outStreamList) display() {
//	nxt := l.head.next
//	for {
//		if nxt == l.tail {
//			break
//		}
//		fmt.Printf("%d ", nxt.itl.seek().(*dataFrame).streamID)
//		nxt = nxt.next
//	}
//	fmt.Println()
//}

// remove from the end of the list.
func (l *outStreamList) add(s *outStream) {
	e := l.tail.prev
	e.next = s
	s.prev = e
	s.next = l.tail
	l.tail.prev = s
}

// remove from the begining of the list.
func (l *outStreamList) remove() *outStream {
	b := l.head.next
	if b == l.tail {
		return nil
	}
	b.deleteSelf()
	return b
}

func (l *outStreamList) deleteAll() {
	b := l.head.next
	if b == l.tail {
		return
	}
	l.head.next = l.tail
	b.prev = nil
	e := l.tail.prev
	l.tail.prev = l.head
	e.next = nil
}

type controlBuffer struct {
	ch chan item
}

func newControlBuffer() *controlBuffer {
	return &controlBuffer{ch: make(chan item, 10000)}
}

func (c *controlBuffer) put(it item, wc *waiters) error {
	select {
	case c.ch <- it:
		return nil
	case <-wc.trDone:
		return ErrConnClosing
	// Following are stream specific cases
	case <-wc.strDone:
		return errStreamClosing
	case <-wc.goAway:
		return errStreamDrain
	case <-wc.done:
		return io.EOF
	}
}

type side int

const (
	client side = iota
	server
)

type loopyWriter struct {
	side           side
	cbuf           *controlBuffer
	sendQuota      uint32
	oiws           uint32 // outbound initial window size.
	mcs            uint32
	numEstdStreams uint32
	allStreams     map[uint32]*outStream
	activeStreams  *outStreamList
	waitingStreams *outStreamList
	framer         *framer
	hBuf           *bytes.Buffer  // The buffer for HPACK encoding.
	hEnc           *hpack.Encoder // HPACK encoder.
	bdpEst         *bdpEstimator
	noFlush        bool

	// Side-specific handlers
	ssGoAwayHandler func(*goAway) error
}

func newLoopyWriter(s side, fr *framer, cbuf *controlBuffer, bdpEst *bdpEstimator) *loopyWriter {
	var buf bytes.Buffer
	return &loopyWriter{
		side:           s,
		cbuf:           cbuf,
		sendQuota:      defaultWindowSize,
		mcs:            defaultMaxStreamsClient,
		oiws:           defaultWindowSize,
		allStreams:     make(map[uint32]*outStream),
		activeStreams:  newOutStreamList(),
		waitingStreams: newOutStreamList(),
		framer:         fr,
		hBuf:           &buf,
		hEnc:           hpack.NewEncoder(&buf),
		bdpEst:         bdpEst,
	}
}

// run should be run in a separate goroutine.
func (l *loopyWriter) run(ctxDone <-chan struct{}) {
	for {
		select {
		case it := <-l.cbuf.ch:
			if err := l.preprocess(it); err != nil {
				errorf("transport: error while handling item. Err: %v", err)
				return
			}
			if _, err := l.processData(); err != nil {
				errorf("transport: error while processing data. Err: %v", err)
				return
			}
		case <-ctxDone:
			warningf("transport: loopy returning. Err: %v", ErrConnClosing)
			return
		}
	hasdata:
		for {
			select {
			case it := <-l.cbuf.ch:
				l.noFlush = false
				if err := l.preprocess(it); err != nil {
					errorf("transport: error while handling item. Err: %v", err)
					return
				}
				if _, err := l.processData(); err != nil {
					errorf("transport: error while processing data. Err: %v", err)
					return
				}
			case <-ctxDone:
				warningf("transport: loopy returning. Err: %v", ErrConnClosing)
				return
			default:
				if isEmpty, err := l.processData(); err != nil {
					errorf("transport: error while processing data. Err: %v", err)
					return
				} else if !isEmpty {
					continue
				}
				if !l.noFlush {
					l.framer.writer.Flush()
				}
				l.noFlush = false
				break hasdata
			}
		}
	}
}

func (l *loopyWriter) windowUpdateHandler(w *windowUpdate) error {
	if w.dir == outgoing {
		return l.framer.fr.WriteWindowUpdate(w.streamID, w.increment)
	}
	// Otherwise update the quota.
	if w.streamID == 0 {
		l.sendQuota += w.increment
		return nil
	}
	// Find the stream and update it.
	if str, ok := l.allStreams[w.streamID]; ok {
		str.bytesOutStanding -= int(w.increment)
		if str.st == waitingOnStreamQuota {
			str.st = active
			l.activeStreams.add(str)
			return nil
		}
	}
	return nil
}

func (l *loopyWriter) settingsHandler(s *settings) error {
	if s.dir == outgoing {
		return l.framer.fr.WriteSettings(s.ss...)
	}
	if err := l.applySettings(s.ss); err != nil {
		return err
	}
	if err := l.framer.fr.WriteSettingsAck(); err != nil {
		return err
	}
	return nil
}

func (l *loopyWriter) headerHandler(h *headerFrame) error {
	if l.side == server {
		if h.endStream { // Case 1.A: Server wants to close stream.
			// Make sure it's not a trailers only response.
			if str, ok := l.allStreams[h.streamID]; ok {
				if str.st != empty { // either active or waiting on stream quota.
					// add it str's list of items.
					str.itl.put(h)
					return nil
				}
				delete(l.allStreams, h.streamID)
				str.deleteSelf()
			}
			//fmt.Println("Server finished stream:", h.streamID)
			return l.headerWriter(h.streamID, h.endStream, h.hf, h.onWrite, false)
		}
		// Case 1.B: Server is responding back with headers.
		str := &outStream{
			st:  empty,
			itl: &itemList{},
			wq:  h.wq,
		}
		l.allStreams[h.streamID] = str
		return l.headerWriter(h.streamID, h.endStream, h.hf, h.onWrite, false)
	}
	// Case 2: Client wants to originate stream.
	str := &outStream{
		st:  notStartedYet,
		itl: &itemList{},
		wq:  h.wq,
	}
	l.allStreams[h.streamID] = str
	if l.numEstdStreams < l.mcs {
		l.numEstdStreams++
		str.st = empty
		return l.headerWriter(h.streamID, h.endStream, h.hf, h.onWrite, h.isUnary)
	}
	// Can't start another stream right now.
	str.itl.put(h)
	l.waitingStreams.add(str)
	return nil
}

func (l *loopyWriter) headerWriter(streamID uint32, endStream bool, hf []hpack.HeaderField, onWrite func(), isUnary bool) error {
	if onWrite != nil {
		onWrite()
	}
	l.hBuf.Reset()
	for _, f := range hf {
		l.hEnc.WriteField(f)
	}
	var (
		err               error
		endHeaders, first bool
	)
	first = true
	for !endHeaders {
		size := l.hBuf.Len()
		if size > http2MaxFrameLen {
			size = http2MaxFrameLen
		} else {
			endHeaders = true
		}
		if first {
			first = false
			err = l.framer.fr.WriteHeaders(http2.HeadersFrameParam{
				StreamID:      streamID,
				BlockFragment: l.hBuf.Next(size),
				EndStream:     endStream,
				EndHeaders:    endHeaders,
			})
		} else {
			err = l.framer.fr.WriteContinuation(
				streamID,
				endHeaders,
				l.hBuf.Next(size),
			)
		}
		if err != nil {
			return err
		}
	}
	if isUnary {
		l.noFlush = true
	}
	return nil
}

func (l *loopyWriter) dataHandler(df *dataFrame) error {
	str, ok := l.allStreams[df.streamID]
	if !ok {
		panic(fmt.Errorf("Tried to send data for a non existant stream!"))
		warningf("transport: Tried to write data on an already closed stream: %d.", df.streamID)
		return nil
	}
	// If we got data for a stream it means that
	// stream was originated and the headers were sent out.
	str.itl.put(df)
	if str.st == empty {
		str.st = active
		l.activeStreams.add(str)
		return nil
	}
	return nil
}

func (l *loopyWriter) pingHandler(p *ping) error {
	if p.ack {
		l.bdpEst.timesnap(p.data)
	}
	return l.framer.fr.WritePing(p.ack, p.data)

}

func (l *loopyWriter) cleanupStreamHandler(c *cleanupStream) error {
	str, ok := l.allStreams[c.streamID]
	if !ok {
		if l.side == client {
			panic(fmt.Errorf("How could this have happend")) // TODO(mmukhi): Remove this.
		}
		return nil
	}
	delete(l.allStreams, c.streamID)
	str.deleteSelf()
	if l.side == server {
		return l.framer.fr.WriteRSTStream(c.streamID, c.rstCode)
	}

	l.numEstdStreams--
	if c.rst && str.st != notStartedYet { // If RST_STREAM needs to be sent.
		if err := l.framer.fr.WriteRSTStream(c.streamID, c.rstCode); err != nil {
			return err
		}
	}
	// Originate a waiting stream
	str = l.waitingStreams.remove()
	if str == nil {
		return nil
	}
	l.numEstdStreams++
	str.st = empty
	hdr := str.itl.remove().(*headerFrame)
	return l.headerWriter(hdr.streamID, hdr.endStream, hdr.hf, hdr.onWrite, hdr.isUnary)
}

func (l *loopyWriter) incomingGoAwayHandler(*incomingGoAway) error {
	if l.side == client {
		l.waitingStreams.deleteAll()
	}
	return nil
}

func (l *loopyWriter) goAwayHandler(g *goAway) error {
	// Handling of outgoing GoAway is very specific to side.
	if l.ssGoAwayHandler != nil {
		return l.ssGoAwayHandler(g)
	}
	return nil
}

func (l *loopyWriter) preprocess(i item) error {
	var err error
	switch i := i.(type) {
	case *windowUpdate:
		err = l.windowUpdateHandler(i)
	case *settings:
		err = l.settingsHandler(i)
	case *headerFrame:
		err = l.headerHandler(i)
	case *cleanupStream:
		err = l.cleanupStreamHandler(i)
	case *incomingGoAway:
		err = l.incomingGoAwayHandler(i)
	case *dataFrame:
		err = l.dataHandler(i)
	case *ping:
		err = l.pingHandler(i)
	case *goAway:
		err = l.goAwayHandler(i)
	default:
		err = fmt.Errorf("transport: unknown control message type %T", i)
	}
	return err
}

func (l *loopyWriter) applySettings(ss []http2.Setting) error {
	for _, s := range ss {
		switch s.ID {
		case http2.SettingMaxConcurrentStreams:
			delta := s.Val - l.mcs
			l.mcs = s.Val
			// If the server is allowing more streams,
			// send out headers of waiting streams.
			var str *outStream
			for ; delta > 0; delta-- {
				if str = l.waitingStreams.remove(); str == nil {
					break
				}
				str.st = empty
				hdr := str.itl.remove().(*headerFrame)
				l.numEstdStreams++
				if err := l.headerWriter(hdr.streamID, hdr.endStream, hdr.hf, hdr.onWrite, hdr.isUnary); err != nil {
					return err
				}
			}
		case http2.SettingInitialWindowSize:
			l.oiws = s.Val
		}
	}
	return nil
}

func (l *loopyWriter) processData() (bool, error) {
	var str *outStream
	//for {
	if l.sendQuota == 0 {
		return true, nil
	}
	if str = l.activeStreams.remove(); str == nil {
		return true, nil
	}
	l.noFlush = false
	dataItem := str.itl.seek().(*dataFrame)
	if len(dataItem.h) == 0 && len(dataItem.d) == 0 {
		// Client sends out empty data frame with endStream = true
		if err := l.framer.fr.WriteData(dataItem.streamID, dataItem.endStream, nil); err != nil {
			return false, err
		}
		str.itl.remove()
		if str.itl.isEmpty() {
			str.st = empty
		} else if trailer, ok := str.itl.seek().(*headerFrame); ok { // the next item is trailers.
			delete(l.allStreams, trailer.streamID)
			if err := l.headerWriter(trailer.streamID, trailer.endStream, trailer.hf, trailer.onWrite, false); err != nil {
				return false, err
			}

		} else {
			l.activeStreams.add(str)
		}
		return false, nil
	}
	var (
		idx int
		buf []byte
	)
	if len(dataItem.h) != 0 { // data header has not been written out yet.
		buf = dataItem.h
	} else {
		idx = 1
		buf = dataItem.d
	}
	size := http2MaxFrameLen
	if len(buf) < size {
		size = len(buf)
	}
	if strQuota := int(l.oiws) - str.bytesOutStanding; strQuota < size {
		size = strQuota
	}
	if l.sendQuota < uint32(size) {
		size = int(l.sendQuota)
	}
	if size > 0 {
		// Now that outgoing flow controls are checked we can replenish str's write quota
		str.wq.replenish(int32(size))
		var endStream bool
		// This last data message on this stream and all
		// of it can be written in this go.
		if dataItem.endStream && size == len(buf) {
			// buf contains either data or it contians header but data is empty.
			if idx == 1 || len(dataItem.d) == 0 {
				endStream = true
			}
		}
		if dataItem.onEachWrite != nil {
			dataItem.onEachWrite()
		}
		if err := l.framer.fr.WriteData(dataItem.streamID, endStream, buf[:size]); err != nil {
			return false, err
		}
		buf = buf[size:]
		str.bytesOutStanding += size
		l.sendQuota -= uint32(size)
		if idx == 0 {
			dataItem.h = buf
		} else {
			dataItem.d = buf
		}
	}

	if len(dataItem.h) == 0 && len(dataItem.d) == 0 { // All the data from that message was written out.
		str.itl.remove()
	}
	if str.itl.isEmpty() {
		str.st = empty
	} else if trailer, ok := str.itl.seek().(*headerFrame); ok { // The next item is trailers.
		delete(l.allStreams, trailer.streamID)
		if err := l.headerWriter(trailer.streamID, trailer.endStream, trailer.hf, trailer.onWrite, false); err != nil {
			return false, err
		}
	} else if int(l.oiws)-str.bytesOutStanding <= 0 { // Ran out of stream quota.
		str.st = waitingOnStreamQuota
	} else {
		l.activeStreams.add(str) // Otherwise, add it to the back of list.
	}
	//}
	return false, nil
}
