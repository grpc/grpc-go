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
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/leakcheck"

	testpb "google.golang.org/grpc/test/grpc_testing"
)

type delayListener struct {
	net.Listener
	closeCalled  chan struct{}
	acceptCalled chan struct{}
	allowCloseCh chan struct{}
	cc           *delayConn
}

func (d *delayListener) Accept() (net.Conn, error) {
	select {
	case <-d.acceptCalled:
		fmt.Println("acceptCalled 2nd time")
		// On the second call, block until closed, then return an error.
		<-d.closeCalled
		fmt.Println("closeCalled")
		<-d.allowCloseCh
		fmt.Println("allowClose")
		return nil, fmt.Errorf("listener is closed")
	default:
		close(d.acceptCalled)
		return d.Listener.Accept()
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

func (d *delayListener) allowClientRead() {
	d.cc.allowRead()
}

func (d *delayListener) Dial(to time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", d.Listener.Addr().String(), to)
	if err != nil {
		return nil, err
	}
	fmt.Println("connected")
	d.cc = &delayConn{Conn: c, blockRead: make(chan struct{})}
	return d.cc, nil
}

func (d *delayListener) clientWriteCalledChan() <-chan struct{} {
	return d.cc.writeCalledChan()
}

type delayConn struct {
	net.Conn
	blockRead   chan struct{}
	mu          sync.Mutex
	writeCalled chan struct{}
}

func (d *delayConn) writeCalledChan() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeCalled = make(chan struct{})
	return d.writeCalled
}
func (d *delayConn) allowRead() {
	close(d.blockRead)
}
func (d *delayConn) Read(b []byte) (n int, err error) {
	<-d.blockRead
	return d.Conn.Read(b)
}
func (d *delayConn) Write(b []byte) (n int, err error) {
	d.mu.Lock()
	if d.writeCalled != nil {
		close(d.writeCalled)
		d.writeCalled = nil
	}
	d.mu.Unlock()
	fmt.Println("Writing: ", b[:10], "... (len: ", len(b), ")")
	return d.Conn.Write(b)
}

func TestGracefulStop(t *testing.T) {
	defer leakcheck.Check(t)
	// This test ensures GracefulStop cannot race and break RPCs on new
	// connections created after GracefulStop was called but before
	// listener.Accept() returns a "closing" error.
	//
	// Steps of this test:
	// 1. Start Server
	// 2. GracefulStop() Server after listener's Accept is called, but don't
	//    allow Accept() to exit when Close() is called on it.
	// 3. Create a new connection to the server after listener.Close() is called.
	//    Server will want to send a GoAway on the new conn, but we delay client
	//    reads until 5.
	// 4. Send an RPC on the new connection.
	// 5. Allow the client to read the GoAway.  The RPC should complete
	//    successfully.

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
	d := func(_ string, to time.Duration) (net.Conn, error) { return dlis.Dial(to) }

	ss := &stubServer{
		emptyCall: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	s := grpc.NewServer()
	testpb.RegisterTestServiceServer(s, ss)

	// 1. Start Server
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		s.Serve(dlis)
		fmt.Println("done serve")
		wg.Done()
	}()

	// 2. GracefulStop() Server after listener's Accept is called, but don't
	//    allow Accept() to exit when Close() is called on it.
	<-dlis.acceptCalled
	wg.Add(1)
	go func() {
		s.GracefulStop()
		fmt.Println("done gracefulstop")
		wg.Done()
	}()

	// 3. Create a new connection to the server after listener.Close() is called.
	//    Server will want to send a GoAway on the new conn, but we delay it
	//    until 5.

	<-dlis.closeCalled // Block until GracefulStop calls dlis.Close()

	// Now dial.  The listener's Accept method will return a valid connection,
	// even though GracefulStop has closed the listener.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cc, err := grpc.DialContext(ctx, "", grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDialer(d))
	if err != nil {
		t.Fatalf("grpc.Dial(%q) = %v", lis.Addr().String(), err)
	}
	cancel()
	client := testpb.NewTestServiceClient(cc)
	defer cc.Close()

	dlis.allowClose()

	fmt.Println("connected; getting write chan")
	wcch := dlis.clientWriteCalledChan()
	go func() {
		// 5. Allow the client to read the GoAway.  The RPC should complete
		//    successfully.
		<-wcch
		fmt.Println("wrote client data; allowing reads")
		dlis.allowClientRead()
	}()

	fmt.Println("calling RPC")
	time.Sleep(time.Second)

	// 4. Send an RPC on the new connection.
	// The server would send a GOAWAY first, but we are delaying the server's
	// writes for now until the client writes more than the preface.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = %v; want <nil>", err)
	}

	// 5. happens above, then we finish the call.
	cancel()
	fmt.Println("waiting for things to exit")
	wg.Wait()
}
