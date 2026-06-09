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

package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type rawCodec struct{}

func (c rawCodec) Marshal(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return nil, fmt.Errorf("rawCodec: expected []byte, got %T", v)
}

func (c rawCodec) Unmarshal(data []byte, v any) error {
	if b, ok := v.(*[]byte); ok {
		*b = append((*b)[:0], data...)
		return nil
	}
	return fmt.Errorf("rawCodec: expected *[]byte, got %T", v)
}

func (c rawCodec) Name() string { return "rawtest" }

func init() {
	encoding.RegisterCodec(rawCodec{})
}

// singleListener implements a net.Listener that accepts exactly one connection from a channel.
// It is used to wire the mock gRPC server directly to our custom streamConnAdapter.
type singleListener struct {
	connChan chan net.Conn
	closed   bool
	mu       sync.Mutex
}

func newSingleListener() *singleListener {
	return &singleListener{connChan: make(chan net.Conn, 1)}
}

func (l *singleListener) Accept() (net.Conn, error) {
	conn, ok := <-l.connChan
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (l *singleListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.connChan)
	}
	return nil
}

func (l *singleListener) Addr() net.Addr { return addr{} }

// setupTestVirtualServer establishes an in-memory mock gRPC server ecosystem for virtual session testing.
// It starts an "outer" server representing the physical connection and an "inner" server representing
// the virtual RPC handler. If customInnerHandler is provided, it overrides the default Echo behavior.
// It returns a ClientConn connected to the outer server and a cleanup function.
func setupTestVirtualServer(t *testing.T, customInnerHandler grpc.StreamHandler) (*grpc.ClientConn, func()) {
	innerLis := newSingleListener()

	innerHandler := customInnerHandler
	if innerHandler == nil {
		innerHandler = func(_ any, stream grpc.ServerStream) error {
			var msg []byte
			err := stream.RecvMsg(&msg)
			if err != nil {
				return err
			}
			return stream.SendMsg(bytes.ToUpper(msg))
		}
	}

	innerServer := grpc.NewServer(grpc.UnknownServiceHandler(innerHandler))
	go innerServer.Serve(innerLis)

	outerLis := bufconn.Listen(1024 * 1024)
	outerServer := grpc.NewServer(grpc.ForceServerCodec(hybridCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var appReq []byte
		err := stream.RecvMsg(&appReq)
		if err != nil {
			return err
		}
		if string(appReq) != "MyInitReq" {
			return status.Errorf(codes.InvalidArgument, "wrong app req")
		}

		stream.SendHeader(nil)

		adapter := newStreamConnAdapter(stream, nil)
		innerLis.connChan <- adapter

		<-stream.Context().Done()
		return nil
	}))
	go outerServer.Serve(outerLis)

	outerConn, err := grpc.NewClient("passthrough:///outer", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return outerLis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}

	cleanup := func() {
		outerConn.Close()
		outerServer.Stop()
		innerServer.Stop()
	}

	return outerConn, cleanup
}

func TestStartSessionCall_EndToEnd(t *testing.T) {
	outerConn, cleanup := setupTestVirtualServer(t, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := StartSessionCall(ctx, outerConn, "/MyService/Session", []byte("MyInitReq"), nil)
	if err != nil {
		t.Fatalf("StartSessionCall failed: %v", err)
	}
	defer sess.VirtualConn.Close()

	select {
	case err := <-sess.Ack:
		if err != nil {
			t.Fatalf("Failed to ack session: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Timeout waiting for Ack")
	}

	innerCtx, innerCancel := context.WithTimeout(context.Background(), time.Second)
	defer innerCancel()

	desc := &grpc.StreamDesc{
		StreamName:    "InnerRPC",
		ClientStreams: true,
		ServerStreams: true,
	}

	innerStream, err := sess.VirtualConn.NewStream(innerCtx, desc, "/TestService/Echo", grpc.CallContentSubtype("rawtest"))
	if err != nil {
		t.Fatalf("Virtual stream create failed: %v", err)
	}

	err = innerStream.SendMsg([]byte("hello"))
	if err != nil {
		t.Fatalf("Virtual stream send failed: %v", err)
	}

	var reply []byte
	err = innerStream.RecvMsg(&reply)
	if err != nil {
		t.Fatalf("Virtual stream recv failed: %v", err)
	}

	if string(reply) != "HELLO" {
		t.Fatalf("innerStream.RecvMsg() = %q, want HELLO", string(reply))
	}

	sess.VirtualConn.Close()

	select {
	case <-sess.Done:
	case <-time.After(time.Second):
	}
}

// fakeClientStream satisfies grpc.ClientStream for testing
type fakeClientStream struct {
	grpc.ClientStream
}

func (f *fakeClientStream) RecvMsg(_ any) error {
	// Block forever to simulate a quiet connection
	select {}
}

func (f *fakeClientStream) SendMsg(_ any) error {
	// Block forever to simulate network flow control exhausted
	select {}
}

func (f *fakeClientStream) CloseSend() error {
	return nil
}

func TestStreamConnAdapter_ReadDeadline(t *testing.T) {
	adapter := newStreamConnAdapter(&fakeClientStream{}, func() {})

	adapter.SetReadDeadline(time.Now().Add(10 * time.Millisecond))

	b := make([]byte, 10)
	_, err := adapter.Read(b)
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("got %v, want ErrDeadlineExceeded", err)
	}
}

func TestStreamConnAdapter_WriteDeadline(t *testing.T) {
	adapter := newStreamConnAdapter(&fakeClientStream{}, func() {})

	// Fill the bounded write channel (capacity 16).
	// pumpWrites pulls 1 and blocks on SendMsg. The channel holds 16.
	// So 17 Writes will fill it completely, and the 18th will block.
	for i := 0; i < 17; i++ {
		adapter.Write([]byte("fill"))
	}

	adapter.SetWriteDeadline(time.Now().Add(10 * time.Millisecond))

	// This write should block and hit the deadline
	_, err := adapter.Write([]byte("block"))
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("got %v, want ErrDeadlineExceeded", err)
	}
}

func TestStreamConnAdapter_CloseCancelsContext(t *testing.T) {
	called := false
	cancel := func() { called = true }
	adapter := newStreamConnAdapter(&fakeClientStream{}, cancel)

	adapter.Close()

	if !called {
		t.Fatalf("expected context cancel to be called on adapter.Close()")
	}
}

func TestStartSessionCall_HandshakeFailure(t *testing.T) {
	// 1. Start an outer server that immediately rejects the connection
	outerLis := bufconn.Listen(1024 * 1024)
	outerServer := grpc.NewServer(grpc.ForceServerCodec(hybridCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var msg []byte
		if err := stream.RecvMsg(&msg); err != nil {
			return err
		}
		return status.Errorf(codes.PermissionDenied, "go away")
	}))
	go outerServer.Serve(outerLis)
	defer outerServer.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outerConn, err := grpc.NewClient("passthrough:///outer", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return outerLis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer outerConn.Close()

	sess, err := StartSessionCall(ctx, outerConn, "/MyService/Session", []byte("MyInitReq"), nil)
	if err != nil {
		t.Fatalf("StartSessionCall failed: %v", err)
	}

	// Wait for the server to reject the stream
	select {
	case <-sess.Done:
		// Ignore exact mapping validation, only care about not hanging.
	case <-time.After(time.Second * 2):
		t.Fatalf("Timeout waiting for Done: channels likely leaked")
	}
}

func TestStartSessionCall_ImmediateVirtualRpc(t *testing.T) {
	innerLis := newSingleListener()
	virtualRPCReceived := make(chan struct{})

	innerServer := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var msg []byte
		err := stream.RecvMsg(&msg)
		if err != nil {
			return err
		}
		// Signal that the virtual RPC has been successfully received BEFORE handshake ack!
		close(virtualRPCReceived)

		return stream.SendMsg(bytes.ToUpper(msg))
	}))
	go innerServer.Serve(innerLis)
	defer innerServer.Stop()

	outerLis := bufconn.Listen(1024 * 1024)
	outerServer := grpc.NewServer(grpc.ForceServerCodec(hybridCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var appReq []byte
		err := stream.RecvMsg(&appReq)
		if err != nil {
			return err
		}
		if string(appReq) != "MyInitReq" {
			return status.Errorf(codes.InvalidArgument, "wrong app req")
		}

		// DO NOT SEND HEADER YET.
		// Pass the adapter to the inner server first.
		adapter := newStreamConnAdapter(stream, nil)
		innerLis.connChan <- adapter

		// Wait until the inner server confirms it received the virtual RPC.
		select {
		case <-virtualRPCReceived:
		case <-time.After(5 * time.Second):
			t.Errorf("Timeout waiting for virtual RPC signal on server")
		}

		// Now send the handshake acknowledgement!
		stream.SendHeader(nil)

		<-stream.Context().Done()
		return nil
	}))
	go outerServer.Serve(outerLis)
	defer outerServer.Stop()

	outerConn, err := grpc.NewClient("passthrough:///outer", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return outerLis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer outerConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := StartSessionCall(ctx, outerConn, "/MyService/Session", []byte("MyInitReq"), nil)
	if err != nil {
		t.Fatalf("StartSessionCall failed: %v", err)
	}
	defer sess.VirtualConn.Close()

	// 1. IMMEDIATELY start virtual RPC before checking Ack!
	desc := &grpc.StreamDesc{
		StreamName:    "InnerRPC",
		ClientStreams: true,
		ServerStreams: true,
	}
	innerCtx, innerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer innerCancel()

	innerStream, err := sess.VirtualConn.NewStream(innerCtx, desc, "/TestService/Echo", grpc.CallContentSubtype("rawtest"))
	if err != nil {
		t.Fatalf("Virtual stream create failed: %v", err)
	}

	// Send the virtual RPC message. This will be queued and received by the server.
	err = innerStream.SendMsg([]byte("hello"))
	if err != nil {
		t.Fatalf("Virtual stream send failed: %v", err)
	}

	// 2. Now wait for the handshake Ack to arrive.
	select {
	case err := <-sess.Ack:
		if err != nil {
			t.Fatalf("Failed to ack session: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for Ack")
	}

	// 3. Finally receive the response.
	var reply []byte
	err = innerStream.RecvMsg(&reply)
	if err != nil {
		t.Fatalf("Virtual stream recv failed: %v", err)
	}

	if string(reply) != "HELLO" {
		t.Fatalf("innerStream.RecvMsg() = %q, want HELLO", string(reply))
	}
}

func TestStartSessionCall_ClientCancelsSession(t *testing.T) {
	blockingHandler := func(_ any, stream grpc.ServerStream) error {
		var msg []byte
		err := stream.RecvMsg(&msg)
		if err != nil {
			return err
		}
		// Block forever (until stream is canceled)
		<-stream.Context().Done()
		return stream.Context().Err()
	}

	outerConn, cleanup := setupTestVirtualServer(t, blockingHandler)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	sess, err := StartSessionCall(ctx, outerConn, "/MyService/Session", []byte("MyInitReq"), nil)
	if err != nil {
		t.Fatalf("StartSessionCall failed: %v", err)
	}
	defer sess.VirtualConn.Close()

	select {
	case err := <-sess.Ack:
		if err != nil {
			t.Fatalf("Failed to ack session: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Timeout waiting for Ack")
	}

	desc := &grpc.StreamDesc{
		StreamName:    "InnerRPC",
		ClientStreams: true,
		ServerStreams: true,
	}
	vctx, vcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer vcancel()
	innerStream, err := sess.VirtualConn.NewStream(vctx, desc, "/TestService/Echo", grpc.CallContentSubtype("rawtest"))
	if err != nil {
		t.Fatalf("Virtual stream create failed: %v", err)
	}

	// Start a virtual RPC
	err = innerStream.SendMsg([]byte("hello"))
	if err != nil {
		t.Fatalf("Virtual stream send failed: %v", err)
	}

	// Cancel the outer session context!
	cancel()

	// Attempting to read from virtual RPC should immediately fail
	var reply []byte
	err = innerStream.RecvMsg(&reply)
	if err == nil {
		t.Fatalf("Expected virtual stream to fail after session cancel, but it succeeded")
	}

	// Done channel must fire with non-nil error
	select {
	case err := <-sess.Done:
		if err == nil {
			t.Fatalf("Expected non-nil error on Done after cancel")
		}
	case <-time.After(time.Second):
		t.Fatalf("Timeout waiting for Done")
	}
}

func TestStartSessionCall_SetupTransportFails(t *testing.T) {
	// Spin up a server that will immediately close the stream after handshake, simulating failure
	outerLis := bufconn.Listen(1024 * 1024)
	outerServer := grpc.NewServer(grpc.ForceServerCodec(hybridCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var appReq []byte
		if err := stream.RecvMsg(&appReq); err != nil {
			return err
		}
		stream.SendHeader(nil)
		// Return nil immediately to terminate the stream, breaking the virtual connection
		return nil
	}))
	go outerServer.Serve(outerLis)
	defer outerServer.Stop()

	outerConn, err := grpc.NewClient("passthrough:///outer", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return outerLis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer outerConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := StartSessionCall(ctx, outerConn, "/MyService/Session", []byte("MyInitReq"), nil)
	if err != nil {
		t.Fatalf("StartSessionCall failed: %v", err)
	}
	defer sess.VirtualConn.Close()

	// Wait for stream failure (Done channel fires)
	select {
	case <-sess.Done:
	case <-time.After(time.Second * 2):
		t.Fatalf("Timeout waiting for Done")
	}

	// Attempting to invoke a virtual RPC must fail immediately with Unavailable status
	desc := &grpc.StreamDesc{
		StreamName:    "InnerRPC",
		ClientStreams: true,
		ServerStreams: true,
	}
	innerStream, err := sess.VirtualConn.NewStream(ctx, desc, "/TestService/Echo", grpc.CallContentSubtype("rawtest"))
	if err != nil {
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("Expected status Unavailable on NewStream, got: %v", err)
		}
		return // Test passed!
	}

	// SendMsg itself might succeed in queuing, but RecvMsg must fail.
	_ = innerStream.SendMsg([]byte("hello"))

	var reply []byte
	err = innerStream.RecvMsg(&reply)
	if err == nil {
		t.Fatalf("Expected virtual stream to fail, but it succeeded")
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Expected status Unavailable on RecvMsg, got: %v", err)
	}
}

func TestStreamConnAdapter_ConcurrentReads(_ *testing.T) {
	adapter := newStreamConnAdapter(&fakeClientStream{}, func() {})

	// Populate the read channel asynchronously to support a large volume of packets
	// without deadlocking, forcing heavy concurrent slicing of currBuf.
	go func() {
		for i := 0; i < 5000; i++ {
			adapter.readCh <- []byte("some very long data packet that forces multiple slices")
		}
		close(adapter.readCh)
	}()

	var wg sync.WaitGroup
	numGoroutines := 50
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			b := make([]byte, 1) // Use a 1-byte buffer to force maximum slicing and interleaving!
			for {
				_, err := adapter.Read(b)
				if err == io.EOF {
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}

	wg.Wait()
}
