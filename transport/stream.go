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

package transport

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/msgdecoder"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const maxInt = int(^uint(0) >> 1)

type streamState uint32

const (
	streamActive    streamState = iota
	streamWriteDone             // EndStream sent
	streamReadDone              // EndStream received
	streamDone                  // the entire stream is finished.
)

// transport's reader goroutine adds msgdecoder.RecvMsg to it which are later
// read by RPC's reading goroutine.
//
// It is protected by a lock in the Stream that owns it.
type recvBuffer struct {
	ctx     context.Context
	ctxDone <-chan struct{}
	c       chan *msgdecoder.RecvMsg
	mu      sync.Mutex
	waiting bool
	list    *msgdecoder.RecvMsgList
}

func newRecvBuffer(ctx context.Context, ctxDone <-chan struct{}) *recvBuffer {
	return &recvBuffer{
		ctx:     ctx,
		ctxDone: ctxDone,
		c:       make(chan *msgdecoder.RecvMsg, 1),
		list:    &msgdecoder.RecvMsgList{},
	}
}

// put adds r to the underlying list if there's no consumer
// waiting, otherwise, it writes on the chan directly.
func (b *recvBuffer) put(r *msgdecoder.RecvMsg) {
	b.mu.Lock()
	if b.waiting {
		b.waiting = false
		b.mu.Unlock()
		b.c <- r
		return
	}
	b.list.Enqueue(r)
	b.mu.Unlock()
}

// getNoBlock returns a msgdecoder.RecvMsg and true status, if there's
// any available.
// If the status is false, the caller must then call
// getWithBlock() before calling getNoBlock() again.
func (b *recvBuffer) getNoBlock() (*msgdecoder.RecvMsg, bool) {
	b.mu.Lock()
	r := b.list.Dequeue()
	if r != nil {
		b.mu.Unlock()
		return r, true
	}
	b.waiting = true
	b.mu.Unlock()
	return nil, false

}

// getWithBlock() blocks until a complete message has been
// received, or an error has occurred or the underlying
// context has expired.
// It must only be called after having called GetNoBlock()
// once.
func (b *recvBuffer) getWithBlock() (*msgdecoder.RecvMsg, error) {
	select {
	case <-b.ctxDone:
		return nil, ContextErr(b.ctx.Err())
	case r := <-b.c:
		return r, nil
	}
}

func (b *recvBuffer) get() (*msgdecoder.RecvMsg, error) {
	m, ok := b.getNoBlock()
	if ok {
		return m, nil
	}
	m, err := b.getWithBlock()
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Stream represents an RPC in the transport layer.
type Stream struct {
	id           uint32
	st           ServerTransport    // nil for client side Stream
	ctx          context.Context    // the associated context of the stream
	cancel       context.CancelFunc // always nil for client side Stream
	done         chan struct{}      // closed at the end of stream to unblock writers. On the client side.
	ctxDone      <-chan struct{}    // same as done chan but for server side. Cache of ctx.Done() (for performance)
	method       string             // the associated RPC method of the stream
	recvCompress string
	sendCompress string
	buf          *recvBuffer
	wq           *writeQuota

	headerChan chan struct{} // closed to indicate the end of header metadata.
	headerDone uint32        // set when headerChan is closed. Used to avoid closing headerChan multiple times.
	header     metadata.MD   // the received header metadata.
	trailer    metadata.MD   // the key-value map of trailer metadata.

	headerOk bool // becomes true from the first header is about to send
	state    streamState

	status *status.Status // the status error received from the server

	bytesReceived uint32 // indicates whether any bytes have been received on this stream
	unprocessed   uint32 // set if the server sends a refused stream or GOAWAY including this stream

	// contentSubtype is the content-subtype for requests.
	// this must be lowercase or the behavior is undefined.
	contentSubtype string

	rbuf       *recvBuffer
	fc         *stInFlow
	msgDecoder *msgdecoder.MessageDecoder
	readErr    error
}

func newStream(ctx context.Context) *Stream {
	// Cache the done chan since a Done() call is expensive.
	ctxDone := ctx.Done()
	s := &Stream{
		ctx:     ctx,
		ctxDone: ctxDone,
		rbuf:    newRecvBuffer(ctx, ctxDone),
	}
	dispatch := func(r *msgdecoder.RecvMsg) {
		s.rbuf.put(r)
	}
	s.msgDecoder = msgdecoder.NewMessageDecoder(dispatch)
	return s
}

// notifyErr notifies RPC of an error seen by the transport.
//
// Note to developers: This call can unblock Read calls on RPC
// and lead to reading of unprotected fields on stream on the
// client-side. It should only be called from inside
// transport.closeStream() if the stream was initialized or from
// inside the cleanup callback if the stream was not initialized.
func (s *Stream) notifyErr(err error) {
	s.rbuf.put(&msgdecoder.RecvMsg{Err: err})
}

// consume is called by transport's reader goroutine for parsing
// and decoding data received for this stream.
func (s *Stream) consume(b []byte, padding int) error {
	// Flow control check.
	if s.fc != nil { // HandlerServer doesn't use our flow control.
		if err := s.fc.onData(uint32(len(b) + padding)); err != nil {
			return err
		}
	}
	s.msgDecoder.Decode(b, padding)
	return nil
}

// Read reads one whole message from the transport.
// It is called by RPC's goroutine.
// It is not safe to be called concurrently by multiple goroutines.
//
// Returns:
// 1. received message's compression status(true if was compressed)
// 2. Message as a byte slice
// 3. Error, if any.
func (s *Stream) Read(maxRecvMsgSize int) (bool, []byte, error) {
	if s.readErr != nil {
		return false, nil, s.readErr
	}
	var (
		m   *msgdecoder.RecvMsg
		err error
	)
	// First read the underlying message header
	if m, err = s.rbuf.get(); err != nil {
		s.readErr = err
		return false, nil, err
	}
	if m.Err != nil {
		s.readErr = m.Err
		return false, nil, s.readErr
	}
	// Make sure the message being received isn't too large.
	if int64(m.Length) > int64(maxInt) {
		s.readErr = status.Errorf(codes.ResourceExhausted, "grpc: received message larger than max length allowed on current machine (%d vs. %d)", m.Length, maxInt)
		return false, nil, s.readErr
	}
	if m.Length > maxRecvMsgSize {
		s.readErr = status.Errorf(codes.ResourceExhausted, "grpc: received message larger than max (%d vs. %d)", m.Length, maxRecvMsgSize)
		return false, nil, s.readErr
	}
	// Send a window update for the message this RPC is reading.
	if s.fc != nil { // HanderServer doesn't use our flow control.
		s.fc.onRead(uint32(m.Length + m.Overhead))
	}
	isCompressed := m.IsCompressed
	// Read the message.
	if m, err = s.rbuf.get(); err != nil {
		s.readErr = err
		return false, nil, err
	}
	if m.Err != nil {
		if m.Err == io.EOF {
			m.Err = io.ErrUnexpectedEOF
		}
		s.readErr = m.Err
		return false, nil, s.readErr
	}
	return isCompressed, m.Data, nil
}

func (s *Stream) swapState(st streamState) streamState {
	return streamState(atomic.SwapUint32((*uint32)(&s.state), uint32(st)))
}

func (s *Stream) compareAndSwapState(oldState, newState streamState) bool {
	return atomic.CompareAndSwapUint32((*uint32)(&s.state), uint32(oldState), uint32(newState))
}

func (s *Stream) getState() streamState {
	return streamState(atomic.LoadUint32((*uint32)(&s.state)))
}

func (s *Stream) waitOnHeader() error {
	if s.headerChan == nil {
		// On the server headerChan is always nil since a stream originates
		// only after having received headers.
		return nil
	}
	select {
	case <-s.ctx.Done():
		return ContextErr(s.ctx.Err())
	case <-s.headerChan:
		return nil
	}
}

// RecvCompress returns the compression algorithm applied to the inbound
// message. It is empty string if there is no compression applied.
func (s *Stream) RecvCompress() string {
	if err := s.waitOnHeader(); err != nil {
		return ""
	}
	return s.recvCompress
}

// SetSendCompress sets the compression algorithm to the stream.
func (s *Stream) SetSendCompress(str string) {
	s.sendCompress = str
}

// Done returns a channel which is closed when it receives the final status
// from the server.
func (s *Stream) Done() <-chan struct{} {
	return s.done
}

// Header acquires the key-value pairs of header metadata once it
// is available. It blocks until i) the metadata is ready or ii) there is no
// header metadata or iii) the stream is canceled/expired.
func (s *Stream) Header() (metadata.MD, error) {
	err := s.waitOnHeader()
	// Even if the stream is closed, header is returned if available.
	select {
	case <-s.headerChan:
		if s.header == nil {
			return nil, nil
		}
		return s.header.Copy(), nil
	default:
	}
	return nil, err
}

// Trailer returns the cached trailer metedata. Note that if it is not called
// after the entire stream is done, it could return an empty MD. Client
// side only.
// It can be safely read only after stream has ended that is either read
// or write have returned io.EOF.
func (s *Stream) Trailer() metadata.MD {
	c := s.trailer.Copy()
	return c
}

// ServerTransport returns the underlying ServerTransport for the stream.
// The client side stream always returns nil.
func (s *Stream) ServerTransport() ServerTransport {
	return s.st
}

// ContentSubtype returns the content-subtype for a request. For example, a
// content-subtype of "proto" will result in a content-type of
// "application/grpc+proto". This will always be lowercase.  See
// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md#requests for
// more details.
func (s *Stream) ContentSubtype() string {
	return s.contentSubtype
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
// Status can be read safely only after the stream has ended,
// that is, read or write has returned io.EOF.
func (s *Stream) Status() *status.Status {
	return s.status
}

// SetHeader sets the header metadata. This can be called multiple times.
// Server side only.
// This should not be called in parallel to other data writes.
func (s *Stream) SetHeader(md metadata.MD) error {
	if md.Len() == 0 {
		return nil
	}
	if s.headerOk || atomic.LoadUint32((*uint32)(&s.state)) == uint32(streamDone) {
		return ErrIllegalHeaderWrite
	}
	s.header = metadata.Join(s.header, md)
	return nil
}

// SendHeader sends the given header metadata. The given metadata is
// combined with any metadata set by previous calls to SetHeader and
// then written to the transport stream.
func (s *Stream) SendHeader(md metadata.MD) error {
	t := s.ServerTransport()
	return t.WriteHeader(s, md)
}

// SetTrailer sets the trailer metadata which will be sent with the RPC status
// by the server. This can be called multiple times. Server side only.
// This should not be called parallel to other data writes.
func (s *Stream) SetTrailer(md metadata.MD) error {
	if md.Len() == 0 {
		return nil
	}
	s.trailer = metadata.Join(s.trailer, md)
	return nil
}

// BytesReceived indicates whether any bytes have been received on this stream.
func (s *Stream) BytesReceived() bool {
	return atomic.LoadUint32(&s.bytesReceived) == 1
}

// Unprocessed indicates whether the server did not process this stream --
// i.e. it sent a refused stream or GOAWAY including this stream ID.
func (s *Stream) Unprocessed() bool {
	return atomic.LoadUint32(&s.unprocessed) == 1
}

// GoString is implemented by Stream so context.String() won't
// race when printing %#v.
func (s *Stream) GoString() string {
	return fmt.Sprintf("<stream: %p, %v>", s, s.method)
}
