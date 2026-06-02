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

// Package session implements client-side support for Virtual RPCs in gRPC-Go.
// It allows multiplexing multiple virtual RPCs over a single physical gRPC bidirectional stream.
//
// # Architecture & Data Flow
//
// The session package establishes a "virtual gRPC stack" on top of a standard gRPC bidirectional
// stream (referred to as the physical stream). This enables applications to amortize the one-time
// setup costs (TCP handshake, TLS negotiation, authentication) across many lightweight virtual
// RPCs.
//
//  1. Session RPC (StartSessionCall): Initiates the physical stream to the server. If stream ID
//     slots are available, it returns a SessionClient immediately with an active virtual ClientConn
//     (allowing zero-RTT virtual RPC queuing) and an Ack channel for asynchronous handshake
//     verification. (If max concurrent streams is reached, stream initiation blocks).
//  2. streamConnAdapter: An internal bridge that wraps the physical gRPC stream and implements the
//     standard net.Conn interface via background read and write pumps.
//  3. Virtual ClientConn: A standard grpc.ClientConn configured with a custom dialer that returns
//     the streamConnAdapter. Virtual RPCs invoked on this connection are serialized into frames and
//     transmitted as messages over the physical stream.
package session

import (
	"context"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc"
)

// SessionClient represents an active multiplexed session over a physical gRPC stream.
type SessionClient struct {
	// VirtualConn is the fully functional HTTP/2-over-HTTP/2 channel.
	VirtualConn *grpc.ClientConn

	// Ack is closed or receives an error when the server sends initial metadata.
	Ack <-chan error

	// Done receives the final error context when the session stream terminates.
	Done <-chan error
}

// StartSessionCall starts a generic BiDi session RPC and returns the SessionClient.
// virtualOpts are applied to the inner HTTP/2 virtual channel.
// opts are applied to the outer session RPC.
func StartSessionCall(ctx context.Context, cc *grpc.ClientConn, method string, req any, virtualOpts []grpc.DialOption, opts ...grpc.CallOption) (*SessionClient, error) {
	desc := &grpc.StreamDesc{
		StreamName:    "Session",
		ClientStreams: true,
		ServerStreams: true,
	}

	ctx, cancel := context.WithCancel(ctx)
	stream, err := cc.NewStream(ctx, desc, method, opts...)
	if err != nil {
		cancel()
		return nil, err
	}

	if err := stream.SendMsg(req); err != nil {
		cancel()
		return nil, err
	}

	adapter := newStreamConnAdapter(stream, cancel)
	vcc, err := createVirtualChannel(adapter, virtualOpts...)
	if err != nil {
		adapter.shutdown(err)
		return nil, err
	}

	ackCh := make(chan error, 1)
	doneCh := make(chan error, 1)

	// Background lifecycle manager: coordinates initial handshake, monitors stream health,
	// and broadcasts final session termination status.
	go func() {
		// Phase 1: Asynchronous Handshake. Block this background goroutine until the server sends
		// its initial metadata confirming session acceptance. If the application initiates virtual
		// RPCs before this completes, outgoing frames are transmitted to the server's receive buffer
		// up to the HTTP/2 flow control window; any remaining backlog queues locally in client memory.
		_, ackErr := stream.Header()
		ackCh <- ackErr
		close(ackCh)

		if ackErr != nil {
			adapter.shutdown(ackErr)
			doneCh <- ackErr
			close(doneCh)
			return
		}

		// Phase 2: Active Session. Block until the adapter shuts down (via stream error or user close).
		<-adapter.closeCh

		// Phase 3: Termination. Determine primary failure cause and broadcast on Done channel.
		adapter.mu.Lock()
		err := adapter.shutdownErr
		adapter.mu.Unlock()
		if err == nil {
			err = stream.Context().Err()
		}
		doneCh <- err
		close(doneCh)
	}()

	return &SessionClient{
		VirtualConn: vcc,
		Ack:         ackCh,
		Done:        doneCh,
	}, nil
}

// streamInterface abstracts gRPC stream operations to support both client and server streams.
type streamInterface interface {
	RecvMsg(m any) error
	SendMsg(m any) error
}

// streamConnAdapter wraps a physical gRPC stream (ClientStream or ServerStream) to implement
// the standard net.Conn interface. It acts as the transport layer for the virtual ClientConn.
type streamConnAdapter struct {
	stream  streamInterface
	cancel  context.CancelFunc
	readCh  chan []byte
	writeCh chan []byte
	currBuf []byte // Remnant of previous Read that exceeded user buffer capacity.

	// mu protects internal state flags, shutdown error, and deadline timestamps.
	mu            sync.Mutex
	closed        bool
	shutdownErr   error
	readDeadline  time.Time
	writeDeadline time.Time

	// readMu serializes concurrent Read invocations to prevent races on currBuf.
	readMu        sync.Mutex

	// streamMu serializes outbound gRPC stream operations (SendMsg, CloseSend) to prevent
	// concurrent write panics. RecvMsg is called exclusively by pumpReads without streamMu.
	streamMu sync.Mutex

	closeCh      chan struct{}
	shutdownOnce sync.Once
	updateRead   chan struct{}
	updateWrite  chan struct{}
}

func newStreamConnAdapter(stream streamInterface, cancel context.CancelFunc) *streamConnAdapter {
	a := &streamConnAdapter{
		stream:      stream,
		cancel:      cancel,
		readCh:      make(chan []byte, 16), // flow control bound
		writeCh:     make(chan []byte, 16),
		closeCh:     make(chan struct{}),
		updateRead:  make(chan struct{}, 1),
		updateWrite: make(chan struct{}, 1),
	}
	go a.pumpWrites()
	go a.pumpReads()
	return a
}

func (a *streamConnAdapter) shutdown(err error) {
	a.shutdownOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.shutdownErr = err
		a.mu.Unlock()
		close(a.closeCh)
		if a.cancel != nil {
			a.cancel()
		}
	})
}

func (a *streamConnAdapter) pumpReads() {
	defer close(a.readCh)
	for {
		var msgBuf []byte
		err := a.stream.RecvMsg(&msgBuf)
		if err != nil {
			a.shutdown(err)
			break
		}

		a.mu.Lock()
		closed := a.closed
		a.mu.Unlock()

		if closed {
			break
		}

		select {
		case a.readCh <- msgBuf:
		case <-a.closeCh:
			return
		}
	}
}

func (a *streamConnAdapter) pumpWrites() {
	for {
		select {
		case msg, ok := <-a.writeCh:
			if !ok {
				return
			}
			a.streamMu.Lock()
			err := a.stream.SendMsg(msg)
			a.streamMu.Unlock()
			if err != nil {
				a.shutdown(err)
				return
			}
		case <-a.closeCh:
			return
		}
	}
}

// deadlineTimer encapsulates time.Timer management for net.Conn deadline compliance.
// It reuses a single underlying timer across internal loop iterations (such as mid-read deadline
// updates) to prevent timer leaks and reduce allocation churn.
type deadlineTimer struct {
	timer *time.Timer
}

func (t *deadlineTimer) reset(deadline time.Time) (<-chan time.Time, error) {
	if deadline.IsZero() {
		if t.timer != nil && !t.timer.Stop() {
			select {
			case <-t.timer.C:
			default:
			}
		}
		return nil, nil
	}
	d := time.Until(deadline)
	if d <= 0 {
		return nil, os.ErrDeadlineExceeded
	}
	if t.timer == nil {
		t.timer = time.NewTimer(d)
	} else {
		if !t.timer.Stop() {
			select {
			case <-t.timer.C:
			default:
			}
		}
		t.timer.Reset(d)
	}
	return t.timer.C, nil
}

func (t *deadlineTimer) stop() {
	if t.timer != nil {
		t.timer.Stop()
	}
}

// popCurrBuf drains available bytes from the internal remnant buffer into b.
func (a *streamConnAdapter) popCurrBuf(b []byte) int {
	n := copy(b, a.currBuf)
	if n < len(a.currBuf) {
		a.currBuf = a.currBuf[n:]
	} else {
		a.currBuf = nil
	}
	return n
}

func (a *streamConnAdapter) Read(b []byte) (n int, err error) {
	a.readMu.Lock()
	defer a.readMu.Unlock()

	var dt deadlineTimer
	defer dt.stop()

	for {
		a.mu.Lock()
		closed := a.closed
		deadline := a.readDeadline
		a.mu.Unlock()

		if len(a.currBuf) == 0 && closed && len(a.readCh) == 0 {
			return 0, io.EOF
		}

		if len(a.currBuf) > 0 {
			return a.popCurrBuf(b), nil
		}

		timerCh, err := dt.reset(deadline)
		if err != nil {
			return 0, err
		}

		select {
		case msg, ok := <-a.readCh:
			if !ok {
				return 0, io.EOF
			}
			a.currBuf = msg
			return a.popCurrBuf(b), nil
		case <-timerCh:
			return 0, os.ErrDeadlineExceeded
		case <-a.updateRead:
			continue
		}
	}
}

func (a *streamConnAdapter) Write(b []byte) (n int, err error) {
	// Asynchronous write pumping requires copying b to the heap. The caller retains ownership
	// of b and may mutate or reuse its backing array immediately after Write returns.
	msg := make([]byte, len(b))
	copy(msg, b)

	var dt deadlineTimer
	defer dt.stop()

	for {
		a.mu.Lock()
		closed := a.closed
		deadline := a.writeDeadline
		a.mu.Unlock()

		if closed {
			return 0, io.ErrClosedPipe
		}

		timerCh, err := dt.reset(deadline)
		if err != nil {
			return 0, err
		}

		select {
		case a.writeCh <- msg:
			return len(msg), nil
		case <-timerCh:
			return 0, os.ErrDeadlineExceeded
		case <-a.closeCh:
			return 0, io.ErrClosedPipe
		case <-a.updateWrite:
			continue
		}
	}
}

func (a *streamConnAdapter) Close() error {
	a.shutdown(io.ErrClosedPipe)
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if cs, ok := a.stream.(interface{ CloseSend() error }); ok {
		return cs.CloseSend()
	}
	return nil
}

func (a *streamConnAdapter) LocalAddr() net.Addr  { return addr{} }
func (a *streamConnAdapter) RemoteAddr() net.Addr { return addr{} }

func (a *streamConnAdapter) SetDeadline(t time.Time) error {
	err1 := a.SetReadDeadline(t)
	err2 := a.SetWriteDeadline(t)
	if err1 != nil {
		return err1
	}
	return err2
}

func (a *streamConnAdapter) SetReadDeadline(t time.Time) error {
	a.mu.Lock()
	a.readDeadline = t
	a.mu.Unlock()

	select {
	case a.updateRead <- struct{}{}:
	default:
	}
	return nil
}

func (a *streamConnAdapter) SetWriteDeadline(t time.Time) error {
	a.mu.Lock()
	a.writeDeadline = t
	a.mu.Unlock()

	select {
	case a.updateWrite <- struct{}{}:
	default:
	}
	return nil
}

type addr struct{}

func (a addr) Network() string { return "virtual" }
func (a addr) String() string  { return "virtual" }

// createVirtualChannel initializes a standard grpc.ClientConn configured to dial over the
// provided streamConnAdapter. It establishes the virtual gRPC transport layer.
func createVirtualChannel(adapter *streamConnAdapter, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	var dialed bool
	var dialMu sync.Mutex

	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		if dialed {
			return nil, net.ErrClosed
		}
		dialed = true
		return adapter, nil
	}

	dialOpts := append([]grpc.DialOption{
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // Plaintext
		grpc.WithDisableServiceConfig(),
		grpc.WithDisableHealthCheck(),
		grpc.WithNoProxy(),
		grpc.WithIdleTimeout(0),
	}, opts...)

	return grpc.NewClient("passthrough:///virtual_target", dialOpts...)
}

