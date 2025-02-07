/*
 *
 * Copyright 2017 gRPC authors.
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

package test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type delayListener struct {
	net.Listener
	closeCalled  chan struct{}
	acceptCalled chan struct{}
	allowCloseCh chan struct{}
	dialed       bool
}

func (d *delayListener) Accept() (net.Conn, error) {
	select {
	case <-d.acceptCalled:
		// On the second call, block until closed, then return an error.
		<-d.closeCalled
		<-d.allowCloseCh
		return nil, fmt.Errorf("listener is closed")
	default:
		close(d.acceptCalled)
		conn, err := d.Listener.Accept()
		if err != nil {
			return nil, err
		}
		// Allow closing of listener only after accept.
		// Note: Dial can return successfully, yet Accept
		// might now have finished.
		d.allowClose()
		return conn, nil
	}
}

func (d *delayListener) allowClose() {
	close(d.allowCloseCh)
}
func (d *delayListener) Close() error {
	close(d.closeCalled)
	go func() {
		<-d.allowCloseCh
		d.Listener.Close()
	}()
	return nil
}

func (d *delayListener) Dial(ctx context.Context) (net.Conn, error) {
	if d.dialed {
		// Only hand out one connection (net.Dial can return more even after the
		// listener is closed).  This is not thread-safe, but Dial should never be
		// called concurrently in this environment.
		return nil, fmt.Errorf("no more conns")
	}
	d.dialed = true
	return (&net.Dialer{}).DialContext(ctx, "tcp", d.Listener.Addr().String())
}

// TestGracefulStop ensures GracefulStop causes new connections to fail.
//
// Steps of this test:
//  1. Start Server
//  2. GracefulStop() Server after listener's Accept is called, but don't
//     allow Accept() to exit when Close() is called on it.
//  3. Create a new connection to the server after listener.Close() is called.
//     Server should close this connection immediately, before handshaking.
//  4. Send an RPC on the new connection.  Should see Unavailable error
//     because the ClientConn is in transient failure.
func (s) TestGracefulStop(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error listenening: %v", err)
	}
	dlis := &delayListener{
		Listener:     lis,
		acceptCalled: make(chan struct{}),
		closeCalled:  make(chan struct{}),
		allowCloseCh: make(chan struct{}),
	}

	ss := &stubserver.StubServer{
		Listener: dlis,
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
		S: grpc.NewServer(),
	}
	// 1. Start Server and start serving by calling Serve().
	stubserver.StartTestService(t, ss)

	// 2. Call GracefulStop from a goroutine. It will trigger Close on the
	// listener, but the listener will not actually close until a connection
	// is accepted.
	gracefulStopDone := make(chan struct{})
	<-dlis.acceptCalled
	go func() {
		ss.S.GracefulStop()
		close(gracefulStopDone)
	}()

	// 3. Create a new connection to the server after listener.Close() is called.
	// Server should close this connection immediately, before handshaking.

	<-dlis.closeCalled // Block until GracefulStop calls dlis.Close()

	// Dial the server. This will cause a connection to be accepted. This will
	// also unblock the Close method .
	ctx, dialCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer dialCancel()
	dialer := func(ctx context.Context, _ string) (net.Conn, error) { return dlis.Dial(ctx) }
	cc, err := grpc.DialContext(ctx, "", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(dialer))
	if err != nil {
		t.Fatalf("grpc.DialContext(_, %q, _) = %v", lis.Addr().String(), err)
	}
	client := testgrpc.NewTestServiceClient(cc)
	defer cc.Close()

	// 4. Make an RPC.
	// The server would send a GOAWAY first, but we are delaying the server's
	// writes for now until the client writes more than the preface.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	if _, err = client.FullDuplexCall(ctx); err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("FullDuplexCall= _, %v; want _, <status code Unavailable>", err)
	}
	cancel()
	<-gracefulStopDone
}

// TestGracefulStopClosesConnAfterLastStream ensures that a server closes the
// connections to its clients when the final stream has completed after
// a GOAWAY.
func (s) TestGracefulStopClosesConnAfterLastStream(t *testing.T) {

	handlerCalled := make(chan struct{})
	gracefulStopCalled := make(chan struct{})

	ts := &funcServer{streamingInputCall: func(testgrpc.TestService_StreamingInputCallServer) error {
		close(handlerCalled) // Initiate call to GracefulStop.
		<-gracefulStopCalled // Wait for GOAWAYs to be received by the client.
		return nil
	}}

	te := newTest(t, tcpClearEnv)
	te.startServer(ts)
	defer te.tearDown()

	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/StreamingInputCall", false)

		<-handlerCalled // Wait for the server to invoke its handler.

		// Gracefully stop the server.
		gracefulStopDone := make(chan struct{})
		go func() {
			te.srv.GracefulStop()
			close(gracefulStopDone)
		}()
		st.wantGoAway(http2.ErrCodeNo) // Server sends a GOAWAY due to GracefulStop.
		pf := st.wantPing()            // Server sends a ping to verify client receipt.
		st.writePing(true, pf.Data)    // Send ping ack to confirm.
		st.wantGoAway(http2.ErrCodeNo) // Wait for subsequent GOAWAY to indicate no new stream processing.

		close(gracefulStopCalled) // Unblock server handler.

		fr := st.wantAnyFrame() // Wait for trailer.
		hdr, ok := fr.(*http2.MetaHeadersFrame)
		if !ok {
			t.Fatalf("Received unexpected frame of type (%T) from server: %v; want HEADERS", fr, fr)
		}
		if !hdr.StreamEnded() {
			t.Fatalf("Received unexpected HEADERS frame from server: %v; want END_STREAM set", fr)
		}

		st.wantRSTStream(http2.ErrCodeNo) // Server should send RST_STREAM because client did not half-close.

		<-gracefulStopDone // Wait for GracefulStop to return.
	})
}

// TestGracefulStopBlocksUntilGRPCConnectionsTerminate ensures that
// GracefulStop() blocks until all ongoing RPCs finished.
func (s) TestGracefulStopBlocksUntilGRPCConnectionsTerminate(t *testing.T) {
	unblockGRPCCall := make(chan struct{})
	grpcCallExecuting := make(chan struct{})
	ss := &stubserver.StubServer{
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			close(grpcCallExecuting)
			<-unblockGRPCCall
			return &testpb.SimpleResponse{}, nil
		},
	}

	err := ss.Start(nil)
	if err != nil {
		t.Fatalf("StubServer.start failed: %s", err)
	}
	t.Cleanup(ss.Stop)

	grpcClientCallReturned := make(chan struct{})
	go func() {
		clt := ss.Client
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		_, err := clt.UnaryCall(ctx, &testpb.SimpleRequest{})
		if err != nil {
			t.Errorf("rpc failed with error: %s", err)
		}
		close(grpcClientCallReturned)
	}()

	gracefulStopReturned := make(chan struct{})
	<-grpcCallExecuting
	go func() {
		ss.S.GracefulStop()
		close(gracefulStopReturned)
	}()

	select {
	case <-gracefulStopReturned:
		t.Error("GracefulStop returned before rpc method call ended")
	case <-time.After(defaultTestShortTimeout):
	}

	unblockGRPCCall <- struct{}{}
	<-grpcClientCallReturned
	<-gracefulStopReturned
}

// TestStopAbortsBlockingGRPCCall ensures that when Stop() is called while an ongoing RPC
// is blocking that:
// - Stop() returns
// - and the RPC fails with an connection  closed error on the client-side
func (s) TestStopAbortsBlockingGRPCCall(t *testing.T) {
	unblockGRPCCall := make(chan struct{})
	grpcCallExecuting := make(chan struct{})
	ss := &stubserver.StubServer{
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			close(grpcCallExecuting)
			<-unblockGRPCCall
			return &testpb.SimpleResponse{}, nil
		},
	}

	err := ss.Start(nil)
	if err != nil {
		t.Fatalf("StubServer.start failed: %s", err)
	}
	t.Cleanup(ss.Stop)

	grpcClientCallReturned := make(chan struct{})
	go func() {
		clt := ss.Client
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		_, err := clt.UnaryCall(ctx, &testpb.SimpleRequest{})
		if err == nil || !isConnClosedErr(err) {
			t.Errorf("expected rpc to fail with connection closed error, got: %v", err)
		}
		close(grpcClientCallReturned)
	}()

	<-grpcCallExecuting
	ss.S.Stop()

	unblockGRPCCall <- struct{}{}
	<-grpcClientCallReturned
}
