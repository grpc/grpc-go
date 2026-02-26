/*
 *
 * Copyright 2024 gRPC authors.
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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// nonGRPCDataMaxLen is the maximum length of nonGRPCDataBuf.
//
// NOTE: If changed this value, you MUST update the corresponding test in:
//   - /test/end2end_test.go:TestHTTPServerSendsNonGRPCHeaderSurfaceFurtherData
const nonGRPCDataMaxLen = 1024

// nonGRPCDataCollectionTimeout is the timeout for collecting non-gRPC data.
const nonGRPCDataCollectionTimeout = 3 * time.Second

// ClientStream implements streaming functionality for a gRPC client.
type ClientStream struct {
	Stream // Embed for common stream functionality.

	ct       *http2Client
	done     chan struct{} // closed at the end of stream to unblock writers.
	doneFunc func()        // invoked at the end of stream.

	headerChan chan struct{} // closed to indicate the end of header metadata.
	header     metadata.MD   // the received header metadata

	status *status.Status // the status error received from the server

	// Non-pointer fields are at the end to optimize GC allocations.

	// headerValid indicates whether a valid header was received.  Only
	// meaningful after headerChan is closed (always call waitOnHeader() before
	// reading its value).
	headerValid bool

	collectionMu    sync.Mutex
	collecting      bool        // indicates if stream entered the stage of non-gRPC data collection.
	collectionTimer *time.Timer // used to limit the time spent on collecting non-gRPC error details.
	nonGRPCDataBuf  []byte      // stores the data of a non-gRPC response.

	noHeaders        bool          // set if the client never received headers (set only after the stream is done).
	headerChanClosed uint32        // set when headerChan is closed. Used to avoid closing headerChan multiple times.
	bytesReceived    atomic.Bool   // indicates whether any bytes have been received on this stream
	unprocessed      atomic.Bool   // set if the server sends a refused stream or GOAWAY including this stream
	statsHandler     stats.Handler // nil for internal streams (e.g., health check, ORCA) where telemetry is not supported.
}

func (s *ClientStream) startNonGRPCDataCollection(st *status.Status, onTimeout func()) {
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()
	if s.collecting {
		return
	}
	s.status = st
	s.collecting = true
	s.nonGRPCDataBuf = make([]byte, 0, nonGRPCDataMaxLen)
	s.collectionTimer = time.AfterFunc(nonGRPCDataCollectionTimeout, onTimeout)
}

// tryHandleNonGRPCData tries to collect non-gRPC body from the given data frame.
// It returns two booleans:
// handle indicates whether the frame should be handled as a non-gRPC response body,
// end indicates whether the stream should be closed after handling this frame.
func (s *ClientStream) tryHandleNonGRPCData(f *parsedDataFrame) (handle bool, end bool) {
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()
	if !s.collecting {
		return false, false
	}

	n := min(f.data.Len(), nonGRPCDataMaxLen-len(s.nonGRPCDataBuf))
	s.nonGRPCDataBuf = append(s.nonGRPCDataBuf, f.data.ReadOnlyData()[0:n]...)
	if len(s.nonGRPCDataBuf) >= nonGRPCDataMaxLen || f.StreamEnded() {
		return true, true
	}
	return true, false
}

// stopNonGRPCBodyCollection stops collecting non-gRPC body and appends the collected.
// Should only be called in closeStream.
func (s *ClientStream) stopNonGRPCDataCollectionLocked() {
	if !s.collecting {
		return
	}
	if s.collectionTimer != nil {
		s.collectionTimer.Stop()
		s.collectionTimer = nil
	}
	data := "\ndata: " + strconv.Quote(string(s.nonGRPCDataBuf))
	s.status = status.New(s.status.Code(), s.status.Message()+data)
}

// Read reads an n byte message from the input stream.
func (s *ClientStream) Read(n int) (mem.BufferSlice, error) {
	b, err := s.Stream.read(n)
	if err == nil {
		s.ct.incrMsgRecv()
	}
	return b, err
}

// Close closes the stream and propagates err to any readers.
func (s *ClientStream) Close(err error) {
	var (
		rst     bool
		rstCode http2.ErrCode
	)
	if err != nil {
		rst = true
		rstCode = http2.ErrCodeCancel
	}
	s.ct.closeStream(s, err, rst, rstCode, status.Convert(err), nil, false)
}

// Write writes the hdr and data bytes to the output stream.
func (s *ClientStream) Write(hdr []byte, data mem.BufferSlice, opts *WriteOptions) error {
	return s.ct.write(s, hdr, data, opts)
}

// BytesReceived indicates whether any bytes have been received on this stream.
func (s *ClientStream) BytesReceived() bool {
	return s.bytesReceived.Load()
}

// Unprocessed indicates whether the server did not process this stream --
// i.e. it sent a refused stream or GOAWAY including this stream ID.
func (s *ClientStream) Unprocessed() bool {
	return s.unprocessed.Load()
}

func (s *ClientStream) waitOnHeader() {
	select {
	case <-s.ctx.Done():
		// Close the stream to prevent headers/trailers from changing after
		// this function returns.
		s.Close(ContextErr(s.ctx.Err()))
		// headerChan could possibly not be closed yet if closeStream raced
		// with operateHeaders; wait until it is closed explicitly here.
		<-s.headerChan
	case <-s.headerChan:
	}
}

// RecvCompress returns the compression algorithm applied to the inbound
// message. It is empty string if there is no compression applied.
func (s *ClientStream) RecvCompress() string {
	s.waitOnHeader()
	return s.recvCompress
}

// Done returns a channel which is closed when it receives the final status
// from the server.
func (s *ClientStream) Done() <-chan struct{} {
	return s.done
}

// Header returns the header metadata of the stream. Acquires the key-value
// pairs of header metadata once it is available. It blocks until i) the
// metadata is ready or ii) there is no header metadata or iii) the stream is
// canceled/expired.
func (s *ClientStream) Header() (metadata.MD, error) {
	s.waitOnHeader()

	if !s.headerValid || s.noHeaders {
		s.collectionMu.Lock()
		defer s.collectionMu.Unlock()
		return nil, s.status.Err()
	}

	return s.header.Copy(), nil
}

// TrailersOnly blocks until a header or trailers-only frame is received and
// then returns true if the stream was trailers-only.  If the stream ends
// before headers are received, returns true, nil.
func (s *ClientStream) TrailersOnly() bool {
	s.waitOnHeader()
	return s.noHeaders
}

// Status returns the status received from the server.
// Status can be read safely only after the stream has ended,
// that is, after Done() is closed.
func (s *ClientStream) Status() *status.Status {
	return s.status
}

func (s *ClientStream) requestRead(n int) {
	s.ct.adjustWindow(s, uint32(n))
}

func (s *ClientStream) updateWindow(n int) {
	s.ct.updateWindow(s, uint32(n))
}
