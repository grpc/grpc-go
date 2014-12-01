/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package transport

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/arcwire-go/codes/codes"
	"golang.org/x/net/context"
	"http2/hpack/hpack"
	"http2/http2"
)

// http2Client implements the ClientTransport interface with HTTP2.
type http2Client struct {
	target string   // server name/addr
	conn   net.Conn // underlying communication channel
	nextID uint32   // the next stream ID to be used

	// writableChan synchronizes write access to the transport.
	// A writer acquires the write lock by sending a value on writableChan
	// and releases it by receiving from writableChan.
	writableChan chan int
	// shutdownChan is closed when Close is called.
	// Blocking operations should select on shutdownChan to avoid
	// blocking forever after Close.
	// TODO(zhaoq): Maybe have a channel context?
	shutdownChan chan struct{}
	// errorChan is closed to notify the I/O error to the caller.
	errorChan chan struct{}

	framer *http2.Framer
	hBuf   *bytes.Buffer  // the buffer for HPACK encoding
	hEnc   *hpack.Encoder // HPACK encoder

	// controlBuf delivers all the control related tasks (e.g., window
	// updates, reset streams, and various settings) to the controller.
	controlBuf *recvBuffer
	// sendQuotaPool provides flow control to outbound message.
	sendQuotaPool *quotaPool

	mu            sync.Mutex     // guard the following variables
	state         transportState // the state of underlying connection
	activeStreams map[uint32]*Stream
	// The max number of concurrent streams
	maxStreams uint32
	// Inbound quota for flow control
	recvQuota int
}

// readCA reads a CA file and returns it in a X509.CertPool pointer.
func readCA(certFile string) (*x509.CertPool, error) {
	f, err := os.Open(certFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if pool.AppendCertsFromPEM(b) {
		return pool, nil
	}
	return nil, fmt.Errorf("could not load any certicate from %s", certFile)
}

// newHTTP2Client constructs a connected ClientTransport to addr based on HTTP2
// and starts to receive messages on it. Non-nil error returns if construction
// fails.
func newHTTP2Client(addr string, useTLS bool, caFile string) (_ ClientTransport, err error) {
	var connErr error
	var conn net.Conn
	// TODO(zhaoq): Use DialTimeout instead.
	if useTLS {
		config := &tls.Config{InsecureSkipVerify: true}
		if caFile != "" {
			cp, err := readCA(caFile)
			if err != nil {
				return nil, ConnectionErrorf("%v", err)
			}
			// TODO(zhaoq): Deal with BNS.
			sn, _, err := net.SplitHostPort(addr)
			if err != nil {
				log.Fatalf("failed to parse server address %v", err)
			}
			config = &tls.Config{
				ServerName: sn,
				RootCAs:    cp,
				NextProtos: []string{ALPNProtoStr},
			}
		}
		conn, connErr = tls.Dial("tcp", addr, config)
	} else {
		conn, connErr = net.Dial("tcp", addr)
	}
	if connErr != nil {
		return nil, ConnectionErrorf("%v", connErr)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	// Send connection preface to server.
	n, err := conn.Write(clientPreface)
	if err != nil {
		return nil, ConnectionErrorf("%v", err)
	}
	if n != len(clientPreface) {
		return nil, ConnectionErrorf("Wrting client preface, wrote %d bytes; want %d", n, len(clientPreface))
	}
	framer := http2.NewFramer(conn, conn)
	if err := framer.WriteSettings(); err != nil {
		return nil, ConnectionErrorf("%v", err)
	}
	var buf bytes.Buffer
	t := &http2Client{
		target: addr,
		conn:   conn,
		// The client initiated stream id is odd starting from 1.
		nextID:        1,
		writableChan:  make(chan int, 1),
		shutdownChan:  make(chan struct{}),
		errorChan:     make(chan struct{}),
		framer:        framer,
		hBuf:          &buf,
		hEnc:          hpack.NewEncoder(&buf),
		controlBuf:    newRecvBuffer(),
		sendQuotaPool: newQuotaPool(initialWindowSize),
		state:         reachable,
		activeStreams: make(map[uint32]*Stream),
		maxStreams:    math.MaxUint32,
	}
	go t.controller()
	t.writableChan <- 0
	// Start the reader goroutine for incoming message. The threading model
	// on receiving is that each transport has a dedicated goroutine which
	// reads HTTP2 frame from network. Then it dispatches the frame to the
	// corresponding stream entity.
	go t.reader()
	return t, nil
}

func (t *http2Client) newStream(ctx context.Context, callHdr *CallHdr) *Stream {
	t.mu.Lock()
	// TODO(zhaoq): Handle uint32 overflow.
	s := &Stream{
		id:            t.nextID,
		method:        callHdr.Method,
		buf:           newRecvBuffer(),
		sendQuotaPool: newQuotaPool(initialWindowSize),
	}
	s.windowHandler = func(n int) {
		t.addRecvQuota(s, n)
	}
	// Make a stream be able to cancel the pending operations by itself.
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.dec = &recvBufferReader{
		ctx:  s.ctx,
		recv: s.buf,
	}
	t.nextID += 2
	t.mu.Unlock()
	return s
}

// NewStream creates a stream and register it into the transport as "active"
// streams.
func (t *http2Client) NewStream(ctx context.Context, callHdr *CallHdr) (*Stream, error) {
	select {
	case <-ctx.Done():
		return nil, ContextErr(ctx.Err())
	case <-t.writableChan:
		// HPACK encodes various headers.
		t.hBuf.Reset()
		t.hEnc.WriteField(hpack.HeaderField{Name: ":method", Value: "POST"})
		t.hEnc.WriteField(hpack.HeaderField{Name: ":scheme", Value: "grpc"})
		t.hEnc.WriteField(hpack.HeaderField{Name: ":path", Value: callHdr.Method})
		t.hEnc.WriteField(hpack.HeaderField{Name: ":authority", Value: callHdr.Host})
		t.hEnc.WriteField(hpack.HeaderField{Name: "content-type", Value: "application/grpc"})
		t.hEnc.WriteField(hpack.HeaderField{Name: "te", Value: "trailers"})
		if callHdr.Auth != nil {
			authToken, err := callHdr.Auth.Token()
			if err != nil {
				return nil, StreamErrorf(codes.InvalidArgument, "%v", err)
			}
			if authToken != "" {
				t.hEnc.WriteField(hpack.HeaderField{Name: "authorization", Value: authToken})
			}
		}
		if dl, ok := ctx.Deadline(); ok {
			t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-timeout", Value: timeoutEncode(dl.Sub(time.Now()))})
		}
		first := true
		endHeaders := false
		var err error
		// Sends the headers in a single batch even when they span multiple frames.
		for !endHeaders {
			size := t.hBuf.Len()
			if size > http2MaxFrameLen {
				size = http2MaxFrameLen
			} else {
				endHeaders = true
			}
			if first {
				// Sends a HeadersFrame to server to start a new stream.
				p := http2.HeadersFrameParam{
					StreamID:      t.nextID,
					BlockFragment: t.hBuf.Next(size),
					EndStream:     false,
					EndHeaders:    endHeaders,
				}
				err = t.framer.WriteHeaders(p)
				first = false
			} else {
				// Sends Continuation frames for the leftover headers.
				err = t.framer.WriteContinuation(t.nextID, endHeaders, t.hBuf.Next(size))
			}
			if err != nil {
				t.notifyError()
				return nil, ConnectionErrorf("%v", err)
			}
		}
		s := t.newStream(ctx, callHdr)
		t.mu.Lock()
		if t.state != reachable {
			t.mu.Unlock()
			return nil, ErrConnClosing
		}
		if uint32(len(t.activeStreams)) >= t.maxStreams {
			t.mu.Unlock()
			t.writableChan <- 0
			return nil, StreamErrorf(codes.Unavailable, "failed to create new stream because the limit has been reached.")
		}
		t.activeStreams[s.id] = s
		t.mu.Unlock()
		t.writableChan <- 0
		return s, nil
	case <-t.shutdownChan:
		return nil, ErrConnClosing
	}
}

// CloseStream clears the footprint of a stream when the stream is not needed any more.
// This must not be executed in reader's goroutine.
func (t *http2Client) CloseStream(s *Stream, err error) {
	t.mu.Lock()
	delete(t.activeStreams, s.id)
	t.mu.Unlock()
	s.mu.Lock()
	if s.state == streamDone {
		s.mu.Unlock()
		return
	}
	s.state = streamDone
	s.mu.Unlock()
	// In case stream sending and receiving are invoked in separate
	// goroutines (e.g., bi-directional streaming), the caller needs
	// to call cancel on the stream to interrupt the blocking on
	// other goroutines.
	s.cancel()
	if _, ok := err.(StreamError); ok {
		t.controlBuf.put(&resetStream{s.id, http2.ErrCodeCancel})
	}
}

// Close kicks off the shutdown process of the transport. This should be called
// only once on a transport. Once it is called, the transport should not be
// accessed any more.
func (t *http2Client) Close() {
	t.mu.Lock()
	t.state = closing
	t.mu.Unlock()
	close(t.shutdownChan)
	t.conn.Close()
	t.mu.Lock()
	streams := t.activeStreams
	t.activeStreams = nil
	t.mu.Unlock()
	// Notify all active streams.
	for _, s := range streams {
		s.write(recvMsg{err: ErrConnClosing})
	}
}

// Write formats the data into HTTP2 data frame(s) and sends it out. The caller
// should proceed only if Write returns nil.
// TODO(zhaoq): opts.Delay is ignored in this implementation. Support it later
// if it improves the performance.
func (t *http2Client) Write(s *Stream, data []byte, opts *Options) error {
	r := bytes.NewBuffer(data)
	for {
		if r.Len() == 0 {
			return nil
		}
		size := http2MaxFrameLen
		// Wait until the stream has some quota to send the data.
		sq, err := wait(s.ctx, t.shutdownChan, s.sendQuotaPool.acquire())
		if err != nil {
			return err
		}
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
		p := r.Next(size)
		ps := len(p)
		if ps < sq {
			// Overbooked stream quota. Return it back.
			s.sendQuotaPool.add(sq - ps)
		}
		if ps < tq {
			// Overbooked transport quota. Return it back.
			t.sendQuotaPool.add(tq - ps)
		}
		var endStream bool
		if opts.Last && r.Len() == 0 {
			endStream = true
		}
		// Got some quota. Try to acquire writing privilege on the transport.
		if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
			return err
		}
		// If WriteData fails, all the pending streams will be handled
		// by http2Client.Close(). No explicit CloseStream() needs to be
		// invoked.
		if err := t.framer.WriteData(s.id, endStream, p); err != nil {
			t.notifyError()
			return ConnectionErrorf("%v", err)
		}
		t.writableChan <- 0
		if endStream {
			s.mu.Lock()
			if s.state != streamDone {
				if s.state == streamReadDone {
					s.state = streamDone
				} else {
					s.state = streamWriteDone
				}
			}
			s.mu.Unlock()
			// Fast return to save one iteration.
			return nil
		}
	}
}

func (t *http2Client) getStream(f http2.Frame) (*Stream, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.activeStreams == nil {
		// The transport is closing.
		return nil, false
	}
	if s, ok := t.activeStreams[f.Header().StreamID]; ok {
		return s, true
	}
	return nil, false
}

// addRecvQuota adjusts the inbound quota for the stream and the transport.
// Window updates will deliver to the controller for sending when
// the cumulative quota exceeds windowUpdateThreshold.
func (t *http2Client) addRecvQuota(s *Stream, n int) {
	t.mu.Lock()
	t.recvQuota += n
	if t.recvQuota >= windowUpdateThreshold {
		t.controlBuf.put(&windowUpdate{0, uint32(t.recvQuota)})
		t.recvQuota = 0
	}
	t.mu.Unlock()

	s.recvQuota += n
	if s.recvQuota >= windowUpdateThreshold {
		t.controlBuf.put(&windowUpdate{s.id, uint32(s.recvQuota)})
		s.recvQuota = 0
	}
}

func (t *http2Client) handleData(f *http2.DataFrame) {
	// Select the right stream to dispatch.
	s, ok := t.getStream(f)
	if !ok {
		return
	}
	// TODO(bradfitz, zhaoq): A copy is required here because there is no
	// guarantee f.Data() is consumed before the arrival of next frame.
	// Can this copy be eliminated?
	data := make([]byte, len(f.Data()))
	copy(data, f.Data())
	s.write(recvMsg{data: data})
}

func (t *http2Client) handleRSTStream(f *http2.RSTStreamFrame) {
	s, ok := t.getStream(f)
	if !ok {
		return
	}
	s.mu.Lock()
	if s.state == streamDone {
		s.mu.Unlock()
		return
	}
	s.state = streamDone
	s.statusCode, ok = http2RSTErrConvTab[http2.ErrCode(f.ErrCode)]
	if !ok {
		log.Println("No gRPC status found for http2 error ", f.ErrCode)
	}
	s.mu.Unlock()
	s.write(recvMsg{err: io.EOF})
}

func (t *http2Client) handleSettings(f *http2.SettingsFrame) {
	if v, ok := f.Value(http2.SettingMaxConcurrentStreams); ok {
		t.mu.Lock()
		t.maxStreams = v
		t.mu.Unlock()
	}
}

func (t *http2Client) handlePing(f *http2.PingFrame) {
	log.Println("PingFrame handler to be implemented")
}

func (t *http2Client) handleGoAway(f *http2.GoAwayFrame) {
	log.Println("GoAwayFrame handler to be implemented")
}

func (t *http2Client) handleWindowUpdate(f *http2.WindowUpdateFrame) {
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

// operateHeader takes action on the decoded headers. It returns the current
// stream if there are remaining headers on the wire (in the following
// Continuation frame).
func (t *http2Client) operateHeaders(hDec *hpackDecoder, s *Stream, frame headerFrame, endStream bool) (pendingStream *Stream) {
	defer func() {
		if pendingStream == nil {
			hDec.state = decodeState{}
		}
	}()
	endHeaders, err := hDec.decodeClientHTTP2Headers(s, frame)
	if err != nil {
		s.write(recvMsg{err: err})
		// Something wrong. Stops reading even when there is remaining.
		return nil
	}
	if !endHeaders {
		return s
	}
	if endStream {
		s.mu.Lock()
		if s.state == streamDone {
			s.mu.Unlock()
			return nil
		}
		s.state = streamDone
		s.statusCode = hDec.state.statusCode
		s.statusDesc = hDec.state.statusDesc
		s.mu.Unlock()
		s.write(recvMsg{err: io.EOF})
	}
	return nil
}

// reader runs as a separate goroutine in charge of reading data from network
// connection.
//
// TODO(zhaoq): currently one reader per transport. Investigate whether this is
// optimal.
// TODO(zhaoq): Check the validity of the incoming frame sequence.
func (t *http2Client) reader() {
	// Check the validity of server preface.
	frame, err := t.framer.ReadFrame()
	if err != nil {
		t.notifyError()
		return
	}
	sf, ok := frame.(*http2.SettingsFrame)
	if !ok {
		t.notifyError()
		return
	}
	t.handleSettings(sf)

	hDec := newHPACKDecoder()
	var curStream *Stream
	// loop to keep reading incoming messages on this transport.
	for {
		frame, err := t.framer.ReadFrame()
		if err != nil {
			t.notifyError()
			return
		}
		switch frame := frame.(type) {
		case *http2.HeadersFrame:
			var ok bool
			if curStream, ok = t.getStream(frame); !ok {
				continue
			}
			endStream := frame.Header().Flags.Has(http2.FlagHeadersEndStream)
			curStream = t.operateHeaders(hDec, curStream, frame, endStream)
		case *http2.ContinuationFrame:
			if curStream == nil {
				continue
			}
			curStream = t.operateHeaders(hDec, curStream, frame, false)
		case *http2.DataFrame:
			t.handleData(frame)
		case *http2.RSTStreamFrame:
			t.handleRSTStream(frame)
		case *http2.SettingsFrame:
			t.handleSettings(frame)
		case *http2.PingFrame:
			t.handlePing(frame)
		case *http2.GoAwayFrame:
			t.handleGoAway(frame)
		case *http2.WindowUpdateFrame:
			t.handleWindowUpdate(frame)
		default:
			log.Printf("http2Client: unhandled frame type %v.", frame)
		}
	}
}

// controller running in a separate goroutine takes charge of sending control
// frames (e.g., window update, reset stream, setting, etc.) to the server.
func (t *http2Client) controller() {
	for {
		select {
		case i := <-t.controlBuf.get():
			t.controlBuf.load()
			select {
			case <-t.writableChan:
				switch i := i.(type) {
				case *windowUpdate:
					t.framer.WriteWindowUpdate(i.streamID, i.increment)
				case *settings:
					t.framer.WriteSettings(http2.Setting{i.id, i.val})
				case *resetStream:
					t.framer.WriteRSTStream(i.streamID, uint32(i.code))
				default:
					log.Printf("http2Client.controller got unexpected item type %v\n", i)
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

func (t *http2Client) Error() <-chan struct{} {
	return t.errorChan
}

func (t *http2Client) notifyError() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// make sure t.errorChan is closed only once.
	if t.state == reachable {
		t.state = unreachable
		close(t.errorChan)
	}
}
