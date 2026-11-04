/*
 *
 * Copyright 2026 gRPC authors.
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
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/resolver"
)

// writeTrackingConn is a net.Conn that tracks whether a call to Write is in
// progress. It assumes a single writer.
type writeTrackingConn struct {
	net.Conn
	mu      sync.Mutex
	writing bool // Set while a call to Write is in progress.
}

func (c *writeTrackingConn) Write(b []byte) (int, error) {
	c.setWriting(true)
	defer c.setWriting(false)
	return c.Conn.Write(b)
}

func (c *writeTrackingConn) setWriting(w bool) {
	c.mu.Lock()
	c.writing = w
	c.mu.Unlock()
}

// isWriting reports whether a call to Write is in progress.
func (c *writeTrackingConn) isWriting() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writing
}

// fakeServerHandshake performs the server side of the HTTP/2 connection
// preface on conn, and returns a framer to be used for the connection.
func fakeServerHandshake(conn net.Conn) (*http2.Framer, error) {
	if _, err := io.ReadFull(conn, make([]byte, len(clientPreface))); err != nil {
		return nil, fmt.Errorf("error reading client preface: %v", err)
	}
	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(); err != nil {
		return nil, fmt.Errorf("error writing server settings: %v", err)
	}
	return fr, nil
}

// countQueuedItems returns the number of items of type T currently queued in
// the given control buffer, in both list and consumerList.
//
// The consumer goroutine (loopy) accesses consumerList without holding cb.mu,
// so conn must be the connection loopy writes to, and loopy must be blocked
// writing to it. The test fails otherwise. While the count is taken, conn.mu
// is held, which keeps loopy from returning from Write, and so from touching
// consumerList.
func countQueuedItems[T cbItem](t *testing.T, cb *controlBuffer, conn *writeTrackingConn) int {
	t.Helper()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if !conn.writing {
		t.Fatal("Loopy is not blocked writing to the connection, cannot read consumerList")
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	n := 0
	// The items are stored in ring buffers, starting at index head and
	// wrapping around to the start of buf past its end.
	for _, l := range []*itemList[cbItem]{&cb.list, &cb.consumerList} {
		for i := range l.count {
			if _, ok := l.buf[(l.head+i)%len(l.buf)].(T); ok {
				n++
			}
		}
	}
	return n
}

// newClientTransportOverConn creates a client transport that uses conn as its
// connection, and throttleTimeout as its throttle timeout. The server side of
// the connection must be handled concurrently.
//
// The returned cleanup function closes the transport, and waits for its
// goroutines to exit.
func newClientTransportOverConn(ctx context.Context, t *testing.T, conn net.Conn, throttleTimeout time.Duration, onClose OnCloseFunc) (*http2Client, func()) {
	t.Helper()
	copts := ConnectOptions{
		BufferPool: mem.DefaultBufferPool(),
		Dialer:     func(context.Context, string) (net.Conn, error) { return conn, nil },
	}
	// clientThrottleTimeout is only read when creating the transport.
	origThrottleTimeout := clientThrottleTimeout
	clientThrottleTimeout = throttleTimeout
	ct, err := NewHTTP2Client(ctx, ctx, resolver.Address{Addr: "fake-server"}, copts, onClose)
	clientThrottleTimeout = origThrottleTimeout
	if err != nil {
		t.Fatalf("NewHTTP2Client() failed: %v", err)
	}
	client := ct.(*http2Client)
	return client, func() {
		client.Close(errors.New("test done"))
		<-client.Error()
		<-client.readerDone
		<-client.writerDone
	}
}

// Tests that a client transport does not queue an unbounded number of items in
// its control buffer when a server floods it with frames that the client must
// respond to (or must otherwise process in its writer), while not reading from
// the connection. The client is expected to stop reading from the connection
// once too many such items are queued, and to close the connection if reads
// stay throttled for clientThrottleTimeout.
func (s) TestClientTransport_ServerFloodWithoutReading(t *testing.T) {
	tests := []struct {
		name string
		// flood writes one round of the flood to the client.
		flood func(fr *http2.Framer) error
	}{
		{
			name:  "PING",
			flood: func(fr *http2.Framer) error { return fr.WritePing(false, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}) },
		},
		{
			name:  "SETTINGS",
			flood: func(fr *http2.Framer) error { return fr.WriteSettings() },
		},
		{
			name:  "WINDOW_UPDATE",
			flood: func(fr *http2.Framer) error { return fr.WriteWindowUpdate(0, 1) },
		},
		{
			// A DATA frame of any size results in the client
			// queuing a BDP ping if there is no BDP ping
			// outstanding. Since the BDP ping has a fixed payload,
			// the server can ack it before it is even written by
			// the client, to make the client queue another BDP
			// ping on the next DATA frame.
			name: "BDP_PING",
			flood: func(fr *http2.Framer) error {
				if err := fr.WriteData(1, false, nil); err != nil {
					return err
				}
				return fr.WritePing(true, bdpPing.data)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			sConn, cConn := net.Pipe()
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				fr, err := fakeServerHandshake(sConn)
				if err != nil {
					t.Error(err)
					return
				}
				// Read frames until the HEADERS frame of the
				// stream created by the test.
				for {
					f, err := fr.ReadFrame()
					if err != nil {
						t.Errorf("Error reading frame from the client: %v", err)
						return
					}
					if _, ok := f.(*http2.HeadersFrame); ok {
						break
					}
				}
				// Stop reading from the connection. Send a
				// PING to make the client's writer block on
				// writing the PING ack, and then flood the
				// client until it closes the connection.
				if err := fr.WritePing(false, [8]byte{}); err != nil {
					t.Errorf("Error writing PING frame: %v", err)
					return
				}
				for {
					if err := test.flood(fr); err != nil {
						return
					}
				}
			}()
			defer func() {
				sConn.Close()
				<-serverDone
			}()

			// Use a small throttle timeout to keep the test fast.
			closeErrCh := make(chan error, 1)
			ct, cleanup := newClientTransportOverConn(ctx, t, cConn, 100*time.Millisecond, func(info GoAwayInfo) {
				select {
				case closeErrCh <- info.Err:
				default:
				}
			})
			defer func() {
				// Closing the server side of the connection
				// unblocks the client's writer, if blocked.
				sConn.Close()
				cleanup()
			}()

			stream, err := ct.NewStream(ctx, &CallHdr{}, nil)
			if err != nil {
				t.Fatalf("NewStream() failed: %v", err)
			}

			// Verify that the client closes the connection because
			// the server is not reading.
			const wantErr = "server is not reading"
			select {
			case err := <-closeErrCh:
				if err == nil || !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("Client transport closed with error %v, want error containing %q", err, wantErr)
				}
			case <-ctx.Done():
				t.Fatal("Timeout when waiting for the client to close the connection")
			}
			// While closing, the client waits for up to
			// goAwayLoopyWriterTimeout for its writer to write a
			// GOAWAY frame, which it cannot do since the server is
			// not reading. Close the server side of the connection
			// to make the writer exit right away.
			sConn.Close()
			select {
			case <-stream.Done():
			case <-ctx.Done():
				t.Fatal("Timeout when waiting for the stream to be closed")
			}
			if st := stream.Status(); st.Code() != codes.Unavailable || !strings.Contains(st.Message(), wantErr) {
				t.Fatalf("Stream status = %v, want code %v and message containing %q", st, codes.Unavailable, wantErr)
			}

			// Verify that the number of queued items that count
			// towards the limit never exceeded the limit. The
			// count is not reset when the control buffer is
			// finished upon the writer exiting.
			select {
			case <-ct.writerDone:
			case <-ctx.Done():
				t.Fatal("Timeout when waiting for the client's writer to exit")
			}
			queued := ct.controlBuf.transportResponseFrames.Load()
			if queued > int64(maxQueuedTransportResponseFrames) {
				t.Fatalf("Number of queued transport response frames = %d, want <= %d", queued, maxQueuedTransportResponseFrames)
			}
		})
	}
}

// Tests that a client transport resumes reading, and keeps the connection
// open, when the server starts reading again before the throttle timeout.
func (s) TestClientTransport_ThrottleLiftedBeforeTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	sConn, cConn := net.Pipe()
	resumeReading := make(chan struct{})
	testDone := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		fr, err := fakeServerHandshake(sConn)
		if err != nil {
			t.Error(err)
			return
		}
		// Read frames until the HEADERS frame of the stream created by
		// the test.
		for {
			f, err := fr.ReadFrame()
			if err != nil {
				t.Errorf("Error reading frame from the client: %v", err)
				return
			}
			if _, ok := f.(*http2.HeadersFrame); ok {
				break
			}
		}
		// Stop reading from the connection, and flood the client with
		// PINGs until it is time to read again.
		floodDone := make(chan struct{})
		go func() {
			defer close(floodDone)
			for {
				select {
				case <-resumeReading:
					return
				default:
				}
				if err := fr.WritePing(false, [8]byte{}); err != nil {
					return
				}
			}
		}()
		select {
		case <-resumeReading:
			io.Copy(io.Discard, sConn)
		case <-testDone:
		}
		<-floodDone
	}()
	defer func() {
		close(testDone)
		sConn.Close()
		<-serverDone
	}()

	// Use a throttle timeout that is much longer than the test.
	closeErrCh := make(chan error, 1)
	ct, cleanup := newClientTransportOverConn(ctx, t, cConn, time.Hour, func(info GoAwayInfo) {
		select {
		case closeErrCh <- info.Err:
		default:
		}
	})
	defer func() {
		// Closing the server side of the connection unblocks the
		// client's writer, if blocked.
		sConn.Close()
		cleanup()
	}()

	if _, err := ct.NewStream(ctx, &CallHdr{}, nil); err != nil {
		t.Fatalf("NewStream() failed: %v", err)
	}
	for ct.controlBuf.trfChan.Load() == nil {
		if ctx.Err() != nil {
			t.Fatal("Timeout when waiting for the client to throttle reads")
		}
		time.Sleep(time.Millisecond)
	}

	// Have the server read again. The client must stop throttling reads,
	// and keep the connection open.
	close(resumeReading)
	for ct.controlBuf.trfChan.Load() != nil {
		if ctx.Err() != nil {
			t.Fatal("Timeout when waiting for the client to stop throttling reads")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-closeErrCh:
		t.Fatalf("Client transport closed with error %v, want it to stay open", err)
	case <-time.After(defaultTestShortTimeout):
	}
}

// Tests that a client transport does not stop reading from the connection when
// a large number of its streams are closed while its writer is blocked, which
// can happen with a high number of concurrent RPCs. Stopping to read in this
// case can cause a deadlock if the server stops reading as well. See
// https://github.com/grpc/grpc-go/issues/9330.
func (s) TestClientTransport_StreamCleanupDoesNotThrottleReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Use more streams than the limit, so that queuing a cleanupStream
	// item for each of them would throttle reads, had these items counted
	// towards the limit.
	numStreams := maxQueuedTransportResponseFrames + 100
	sConn, cConn := net.Pipe()
	headersRead := make(chan struct{})
	sendRSTs := make(chan struct{})
	testDone := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		fr, err := fakeServerHandshake(sConn)
		if err != nil {
			t.Error(err)
			return
		}
		// Read frames until the HEADERS frames of all the streams
		// created by the test have been read.
		for n := 0; n < numStreams; {
			f, err := fr.ReadFrame()
			if err != nil {
				t.Errorf("Error reading frame from the client: %v", err)
				return
			}
			if _, ok := f.(*http2.HeadersFrame); ok {
				n++
			}
		}
		close(headersRead)

		// Stop reading from the connection, and reset all the streams
		// once the client's writer is blocked.
		select {
		case <-sendRSTs:
		case <-testDone:
			return
		}
		for i := 0; i < numStreams; i++ {
			if err := fr.WriteRSTStream(uint32(2*i+1), http2.ErrCodeInternal); err != nil {
				t.Errorf("Error writing RST_STREAM frame: %v", err)
				return
			}
		}
	}()
	defer func() {
		close(testDone)
		sConn.Close()
		<-serverDone
	}()

	// Use a small throttle timeout for the client to quickly close the
	// connection (and fail the test) if it does throttle reads.
	wConn := &writeTrackingConn{Conn: cConn}
	ct, cleanup := newClientTransportOverConn(ctx, t, wConn, 100*time.Millisecond, func(GoAwayInfo) {})
	defer func() {
		// Closing the server side of the connection unblocks the
		// client's writer, if blocked.
		sConn.Close()
		cleanup()
	}()

	streams := make([]*ClientStream, numStreams)
	for i := range streams {
		stream, err := ct.NewStream(ctx, &CallHdr{}, nil)
		if err != nil {
			t.Fatalf("NewStream() failed: %v", err)
		}
		streams[i] = stream
	}
	select {
	case <-headersRead:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the server to read all the HEADERS frames")
	}

	// Create one more stream. Since the server is not reading anymore, the
	// client's writer blocks when writing its HEADERS frame.
	if _, err := ct.NewStream(ctx, &CallHdr{}, nil); err != nil {
		t.Fatalf("NewStream() failed: %v", err)
	}
	for !wConn.isWriting() {
		if ctx.Err() != nil {
			t.Fatal("Timeout when waiting for the client's writer to block")
		}
		time.Sleep(time.Millisecond)
	}

	// Have the server reset all the streams. A cleanupStream item is
	// queued for the client's blocked writer for every stream. The client
	// must keep reading regardless, and process all the RST_STREAM frames.
	close(sendRSTs)
	for i, stream := range streams {
		select {
		case <-stream.Done():
		case <-ctx.Done():
			t.Fatalf("Timeout when waiting for stream %d to be closed", i)
		}
		if st := stream.Status(); st.Code() != codes.Internal {
			t.Fatalf("Stream %d status = %v, want code %v", i, st, codes.Internal)
		}
	}

	// Verify that the test queued enough cleanupStream items to throttle
	// reads, had they counted towards the limit, and that the client
	// transport is still healthy.
	numCleanups := countQueuedItems[*cleanupStream](t, ct.controlBuf, wConn)
	if numCleanups < numStreams {
		t.Fatalf("Number of queued cleanupStream items = %d, want at least %d", numCleanups, numStreams)
	}
	if ct.controlBuf.trfChan.Load() != nil {
		t.Fatal("Client transport is throttling reads, want no throttling")
	}
	select {
	case <-ct.Error():
		t.Fatal("Client transport closed unexpectedly")
	default:
	}
}

// Tests that a server transport does not queue an unbounded number of BDP
// pings in its control buffer when a client floods it with DATA frames, each
// followed by an ack for the BDP ping, while not reading from the connection.
// The server queues a BDP ping for a DATA frame of any size, on any stream,
// whenever there is no BDP ping outstanding. And since the BDP ping has a
// fixed payload, the client can ack it before it is even written by the
// server. The server is expected to stop reading from the connection once too
// many pings are queued.
func (s) TestServerTransport_BDPPingFloodWithoutReading(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	sConn, cConn := net.Pipe()
	defer func() {
		sConn.Close()
		cConn.Close()
	}()
	// Track the server's writes, to count the queued pings while its writer
	// is blocked.
	wConn := &writeTrackingConn{Conn: sConn}

	type result struct {
		st  ServerTransport
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		st, err := NewServerTransport(wConn, &ServerConfig{
			MaxStreams: math.MaxUint32,
			BufferPool: mem.DefaultBufferPool(),
			// Disable write buffering, so that the server's writer
			// blocks on the first frame it writes once the client
			// stops reading.
			WriteBufferSize: 0,
		})
		resultCh <- result{st: st, err: err}
	}()

	// Act as the client. Read frames from the server until it acks the
	// client's settings, and stop reading from the connection after that.
	// Frames need to be read concurrently with the client preface being
	// written, since writes on a net.Pipe block until the data is read.
	settingsAckRead := make(chan error, 1)
	go func() {
		rfr := http2.NewFramer(nil, cConn)
		for {
			f, err := rfr.ReadFrame()
			if err != nil {
				settingsAckRead <- fmt.Errorf("error reading frame from the server: %v", err)
				return
			}
			if sf, ok := f.(*http2.SettingsFrame); ok && sf.IsAck() {
				settingsAckRead <- nil
				return
			}
		}
	}()
	// Fail instead of hanging if the server does not read the handshake.
	cConn.SetWriteDeadline(time.Now().Add(defaultTestTimeout))
	if _, err := cConn.Write(clientPreface); err != nil {
		t.Fatalf("Error writing client preface: %v", err)
	}
	fr := http2.NewFramer(cConn, nil)
	if err := fr.WriteSettings(); err != nil {
		t.Fatalf("Error writing client settings: %v", err)
	}
	cConn.SetWriteDeadline(time.Time{})

	var st *http2Server
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("NewServerTransport() failed: %v", r.err)
		}
		st = r.st.(*http2Server)
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the server transport to be created")
	}
	handleStreamsDone := make(chan struct{})
	go func() {
		defer close(handleStreamsDone)
		st.HandleStreams(ctx, func(*ServerStream) {})
	}()
	defer func() {
		st.Close(errors.New("test done"))
		<-handleStreamsDone
	}()

	select {
	case err := <-settingsAckRead:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the server to ack the client's settings")
	}

	// Flood the server. Since the client is not reading anymore, the
	// server's writer blocks when writing the first BDP ping, and the
	// subsequent ones are queued. Use well over the limit as the number of
	// rounds, so that the flood cannot complete if the server throttles
	// reads at the limit.
	numRounds := 2 * maxQueuedTransportResponseFrames
	var roundsWritten int
	var floodErr error
	floodDone := make(chan struct{})
	go func() {
		defer close(floodDone)
		for ; roundsWritten < numRounds; roundsWritten++ {
			if floodErr = fr.WriteData(1, false, nil); floodErr != nil {
				return
			}
			if floodErr = fr.WritePing(true, bdpPing.data); floodErr != nil {
				return
			}
		}
	}()
	defer func() {
		// Closing the client side of the connection unblocks the
		// flood, if blocked.
		cConn.Close()
		<-floodDone
	}()

	// Verify that the server stops reading from the connection before
	// reading all the frames of the flood, and that it does not queue more
	// pings than the limit.
	for st.controlBuf.trfChan.Load() == nil {
		select {
		case <-floodDone:
			t.Fatalf("Flood ended after %d of %d rounds (error: %v) without the server throttling reads", roundsWritten, numRounds, floodErr)
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for the server to throttle reads")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-floodDone:
		t.Fatalf("Flood ended after %d of %d rounds (error: %v) while the server was throttling reads", roundsWritten, numRounds, floodErr)
	case <-time.After(defaultTestShortTimeout):
	}
	if numPings := countQueuedItems[*ping](t, st.controlBuf, wConn); numPings > maxQueuedTransportResponseFrames {
		t.Fatalf("Number of queued pings = %d, want <= %d", numPings, maxQueuedTransportResponseFrames)
	}
}
