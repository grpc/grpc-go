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
	"errors"
	"io"
	"log"
	"math"
	"net"
	"strconv"
	"sync"

	"github.com/bradfitz/http2"
	"github.com/bradfitz/http2/hpack"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// ErrIllegalHeaderWrite indicates that setting header is illegal because of
// the stream's state.
var ErrIllegalHeaderWrite = errors.New("transport: the stream is done or WriteHeader was already called")

// http2Server implements the ServerTransport interface with HTTP2.
type http2Server struct {
	conn        net.Conn
	maxStreamID uint32 // max stream ID ever seen
	// writableChan synchronizes write access to the transport.
	// A writer acquires the write lock by sending a value on writableChan
	// and releases it by receiving from writableChan.
	writableChan chan int
	// shutdownChan is closed when Close is called.
	// Blocking operations should select on shutdownChan to avoid
	// blocking forever after Close.
	shutdownChan chan struct{}
	framer       *http2.Framer
	hBuf         *bytes.Buffer  // the buffer for HPACK encoding
	hEnc         *hpack.Encoder // HPACK encoder

	// The max number of concurrent streams.
	maxStreams uint32
	// controlBuf delivers all the control related tasks (e.g., window
	// updates, reset streams, and various settings) to the controller.
	controlBuf *recvBuffer
	// sendQuotaPool provides flow control to outbound message.
	sendQuotaPool *quotaPool

	mu            sync.Mutex // guard the following
	state         transportState
	activeStreams map[uint32]*Stream
	// Inbound quota for flow control
	recvQuota int
}

// newHTTP2Server constructs a ServerTransport based on HTTP2. ConnectionError is
// returned if something goes wrong.
func newHTTP2Server(conn net.Conn, maxStreams uint32) (_ ServerTransport, err error) {
	framer := http2.NewFramer(conn, conn)
	// Send initial settings as connection preface to client.
	// TODO(zhaoq): Have a better way to signal "no limit" because 0 is
	// permitted in the HTTP2 spec.
	if maxStreams == 0 {
		err = framer.WriteSettings()
		maxStreams = math.MaxUint32
	} else {
		err = framer.WriteSettings(http2.Setting{http2.SettingMaxConcurrentStreams, maxStreams})
	}
	if err != nil {
		return
	}
	var buf bytes.Buffer
	t := &http2Server{
		conn:          conn,
		framer:        framer,
		hBuf:          &buf,
		hEnc:          hpack.NewEncoder(&buf),
		maxStreams:    maxStreams,
		controlBuf:    newRecvBuffer(),
		sendQuotaPool: newQuotaPool(initialWindowSize),
		state:         reachable,
		writableChan:  make(chan int, 1),
		shutdownChan:  make(chan struct{}),
		activeStreams: make(map[uint32]*Stream),
	}
	go t.controller()
	t.writableChan <- 0
	return t, nil
}

// operateHeader takes action on the decoded headers. It returns the current
// stream if there are remaining headers on the wire (in the following
// Continuation frame).
func (t *http2Server) operateHeaders(hDec *hpackDecoder, s *Stream, frame headerFrame, endStream bool, handle func(*Stream), wg *sync.WaitGroup) (pendingStream *Stream) {
	defer func() {
		if pendingStream == nil {
			hDec.state = decodeState{}
		}
	}()
	endHeaders, err := hDec.decodeServerHTTP2Headers(s, frame)
	if err != nil {
		log.Printf("transport: http2Server.operateHeader found %v", err)
		if se, ok := err.(StreamError); ok {
			t.controlBuf.put(&resetStream{s.id, statusCodeConvTab[se.Code]})
		}
		return nil
	}
	if endStream {
		// s is just created by the caller. No lock needed.
		s.state = streamReadDone
	}
	if !endHeaders {
		return s
	}
	t.mu.Lock()
	if t.state != reachable {
		t.mu.Unlock()
		return nil
	}
	if uint32(len(t.activeStreams)) >= t.maxStreams {
		t.mu.Unlock()
		t.controlBuf.put(&resetStream{s.id, http2.ErrCodeRefusedStream})
		return nil
	}
	t.activeStreams[s.id] = s
	t.mu.Unlock()
	s.windowHandler = func(n int) {
		t.addRecvQuota(s, n)
	}
	if hDec.state.timeoutSet {
		s.ctx, s.cancel = context.WithTimeout(context.TODO(), hDec.state.timeout)
	} else {
		s.ctx, s.cancel = context.WithCancel(context.TODO())
	}
	// Cache the current stream to the context so that the server application
	// can find out. Required when the server wants to send some metadata
	// back to the client (unary call only).
	s.ctx = newContextWithStream(s.ctx, s)
	// Attach the received metadata to the context.
	if len(hDec.state.mdata) > 0 {
		s.ctx = metadata.NewContext(s.ctx, hDec.state.mdata)
	}

	s.dec = &recvBufferReader{
		ctx:  s.ctx,
		recv: s.buf,
	}
	s.method = hDec.state.method

	wg.Add(1)
	go func() {
		handle(s)
		wg.Done()
	}()
	return nil
}

// HandleStreams receives incoming streams using the given handler. This is
// typically run in a separate goroutine.
func (t *http2Server) HandleStreams(handle func(*Stream)) {
	// Check the validity of client preface.
	preface := make([]byte, len(clientPreface))
	if _, err := io.ReadFull(t.conn, preface); err != nil {
		log.Printf("transport: http2Server.HandleStreams failed to receive the preface from client: %v", err)
		t.Close()
		return
	}
	if !bytes.Equal(preface, clientPreface) {
		log.Printf("transport: http2Server.HandleStreams received bogus greeting from client: %q", preface)
		t.Close()
		return
	}

	frame, err := t.framer.ReadFrame()
	if err != nil {
		t.Close()
		return
	}
	sf, ok := frame.(*http2.SettingsFrame)
	if !ok {
		log.Printf("transport: http2Server.HandleStreams saw invalid preface type %T from client", frame)
		t.Close()
		return
	}
	t.handleSettings(sf)

	hDec := newHPACKDecoder()
	var curStream *Stream
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		frame, err := t.framer.ReadFrame()
		if err != nil {
			t.Close()
			return
		}
		switch frame := frame.(type) {
		case *http2.HeadersFrame:
			id := frame.Header().StreamID
			if id%2 != 1 || id <= t.maxStreamID {
				// illegal gRPC stream id.
				log.Println("transport: http2Server.HandleStreams received an illegal stream id: ", id)
				t.Close()
				break
			}
			t.maxStreamID = id
			buf := newRecvBuffer()
			curStream = &Stream{
				id:            frame.Header().StreamID,
				st:            t,
				buf:           buf,
				sendQuotaPool: newQuotaPool(initialWindowSize),
			}
			endStream := frame.Header().Flags.Has(http2.FlagHeadersEndStream)
			curStream = t.operateHeaders(hDec, curStream, frame, endStream, handle, &wg)
		case *http2.ContinuationFrame:
			if curStream == nil {
				continue
			}
			curStream = t.operateHeaders(hDec, curStream, frame, false, handle, &wg)
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
		default:
			log.Printf("transport: http2Server.HanldeStreams found unhandled frame type %v.", frame)
		}
	}
}

func (t *http2Server) getStream(f http2.Frame) (*Stream, bool) {
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

// addRecvQuota adjusts the inbound quota for the stream and the transport.
// Window updates will deliver to the controller for sending when
// the cumulative quota exceeds windowUpdateThreshold.
func (t *http2Server) addRecvQuota(s *Stream, n int) {
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

func (t *http2Server) handleData(f *http2.DataFrame) {
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
	if f.Header().Flags.Has(http2.FlagDataEndStream) {
		// Received the end of stream from the client.
		s.mu.Lock()
		if s.state != streamDone {
			if s.state == streamWriteDone {
				s.state = streamDone
			} else {
				s.state = streamReadDone
			}
		}
		s.mu.Unlock()
		s.write(recvMsg{err: io.EOF})
	}
}

func (t *http2Server) handleRSTStream(f *http2.RSTStreamFrame) {
	s, ok := t.getStream(f)
	if !ok {
		return
	}
	s.mu.Lock()
	// Sets the stream state to avoid sending RSTStreamFrame to client
	// unnecessarily.
	s.state = streamDone
	s.mu.Unlock()
	t.closeStream(s)
}

func (t *http2Server) handleSettings(f *http2.SettingsFrame) {
	// TODO(zhaoq): Handle the useful settings from client.
}

func (t *http2Server) handlePing(f *http2.PingFrame) {
	// TODO(zhaoq): PingFrame handler to be implemented
}

func (t *http2Server) handleWindowUpdate(f *http2.WindowUpdateFrame) {
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

func (t *http2Server) writeHeaders(s *Stream, b *bytes.Buffer, endStream bool) error {
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
			err = t.framer.WriteHeaders(p)
			first = false
		} else {
			err = t.framer.WriteContinuation(s.id, endHeaders, b.Next(size))
		}
		if err != nil {
			t.Close()
			return ConnectionErrorf("transport: %v", err)
		}
	}
	return nil
}

// WriteHeader sends the header metedata md back to the client.
func (t *http2Server) WriteHeader(s *Stream, md metadata.MD) error {
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
	for k, v := range md {
		t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: v})
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
func (t *http2Server) WriteStatus(s *Stream, statusCode codes.Code, statusDesc string) error {
	s.mu.RLock()
	if s.state == streamDone {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()
	if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
		return err
	}
	t.hBuf.Reset()
	t.hEnc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
	t.hEnc.WriteField(
		hpack.HeaderField{
			Name:  "grpc-status",
			Value: strconv.Itoa(int(statusCode)),
		})
	t.hEnc.WriteField(hpack.HeaderField{Name: "grpc-message", Value: statusDesc})
	// Attach the trailer metadata.
	for k, v := range s.trailer {
		t.hEnc.WriteField(hpack.HeaderField{Name: k, Value: v})
	}
	if err := t.writeHeaders(s, t.hBuf, true); err != nil {
		return err
	}
	t.closeStream(s)
	t.writableChan <- 0
	return nil
}

// Write converts the data into HTTP2 data frame and sends it out. Non-nil error
// is returns if it fails (e.g., framing error, transport error).
func (t *http2Server) Write(s *Stream, data []byte, opts *Options) error {
	// TODO(zhaoq): Support multi-writers for a single stream.
	var writeHeaderFrame bool
	s.mu.Lock()
	if !s.headerOk {
		writeHeaderFrame = true
		s.headerOk = true
	}
	s.mu.Unlock()
	if writeHeaderFrame {
		if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
			return err
		}
		t.hBuf.Reset()
		t.hEnc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
		t.hEnc.WriteField(hpack.HeaderField{Name: "content-type", Value: "application/grpc"})
		p := http2.HeadersFrameParam{
			StreamID:      s.id,
			BlockFragment: t.hBuf.Bytes(),
			EndHeaders:    true,
		}
		if err := t.framer.WriteHeaders(p); err != nil {
			t.Close()
			return ConnectionErrorf("transport: %v", err)
		}
		t.writableChan <- 0
	}
	r := bytes.NewBuffer(data)
	for {
		if r.Len() == 0 {
			return nil
		}
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
		// Got some quota. Try to acquire writing privilege on the
		// transport.
		if _, err := wait(s.ctx, t.shutdownChan, t.writableChan); err != nil {
			return err
		}
		if err := t.framer.WriteData(s.id, false, p); err != nil {
			t.Close()
			return ConnectionErrorf("transport: %v", err)
		}
		t.writableChan <- 0
	}

}

// controller running in a separate goroutine takes charge of sending control
// frames (e.g., window update, reset stream, setting, etc.) to the server.
func (t *http2Server) controller() {
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
					t.framer.WriteRSTStream(i.streamID, i.code)
				default:
					log.Printf("transport: http2Server.controller got unexpected item type %v\n", i)
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

// Close starts shutting down the http2Server transport.
// TODO(zhaoq): Now the destruction is not blocked on any pending streams. This
// could cause some resource issue. Revisit this later.
func (t *http2Server) Close() (err error) {
	t.mu.Lock()
	if t.state == closing {
		t.mu.Unlock()
		return errors.New("transport: Close() was already called")
	}
	t.state = closing
	streams := t.activeStreams
	t.activeStreams = nil
	t.mu.Unlock()
	close(t.shutdownChan)
	err = t.conn.Close()
	// Notify all active streams.
	for _, s := range streams {
		s.write(recvMsg{err: ErrConnClosing})
	}
	return
}

// closeStream clears the footprint of a stream when the stream is not needed
// any more.
func (t *http2Server) closeStream(s *Stream) {
	t.mu.Lock()
	delete(t.activeStreams, s.id)
	t.mu.Unlock()
	s.mu.Lock()
	if s.state == streamDone {
		return
	}
	s.state = streamDone
	s.mu.Unlock()
	// In case stream sending and receiving are invoked in separate
	// goroutines (e.g., bi-directional streaming), the caller needs
	// to call cancel on the stream to interrupt the blocking on
	// other goroutines.
	s.cancel()
}
