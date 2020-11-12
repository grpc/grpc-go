/*
 *
 * Copyright 2020 gRPC authors.
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

package xds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestServeOptions_Validate(t *testing.T) {
	tests := []struct {
		desc    string
		opts    ServeOptions
		wantErr bool
	}{
		{
			desc:    "empty options",
			opts:    ServeOptions{},
			wantErr: true,
		},
		{
			desc:    "bad address",
			opts:    ServeOptions{Address: "I'm a bad IP address"},
			wantErr: true,
		},
		{
			desc:    "no port",
			opts:    ServeOptions{Address: "1.2.3.4"},
			wantErr: true,
		},
		{
			desc:    "empty hostname",
			opts:    ServeOptions{Address: ":1234"},
			wantErr: true,
		},
		{
			desc:    "localhost",
			opts:    ServeOptions{Address: "localhost:1234"},
			wantErr: true,
		},
		{
			desc: "ipv4",
			opts: ServeOptions{Address: "1.2.3.4:1234"},
		},
		{
			desc: "ipv6",
			opts: ServeOptions{Address: "[1:2::3:4]:1234"},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			err := test.opts.validate()
			if (err != nil) != test.wantErr {
				t.Errorf("ServeOptions.validate(%+v) returned err %v, wantErr: %v", test.opts, err, test.wantErr)
			}
		})
	}
}

type fakeGRPCServer struct {
	done              chan struct{}
	registerServiceCh *testutils.Channel
	serveCh           *testutils.Channel
	stopCh            *testutils.Channel
	gracefulStopCh    *testutils.Channel
}

func (f *fakeGRPCServer) RegisterService(*grpc.ServiceDesc, interface{}) {
	f.registerServiceCh.Send(nil)
}

func (f *fakeGRPCServer) Serve(net.Listener) error {
	f.serveCh.Send(nil)
	<-f.done
	return nil
}

func (f *fakeGRPCServer) Stop() {
	close(f.done)
	f.stopCh.Send(nil)
}
func (f *fakeGRPCServer) GracefulStop() {
	close(f.done)
	f.gracefulStopCh.Send(nil)
}

func newFakeGRPCServer() *fakeGRPCServer {
	return &fakeGRPCServer{
		done:              make(chan struct{}),
		registerServiceCh: testutils.NewChannel(),
		serveCh:           testutils.NewChannel(),
		stopCh:            testutils.NewChannel(),
		gracefulStopCh:    testutils.NewChannel(),
	}
}

func (s) TestNewServer(t *testing.T) {
	// The xds package adds a couple of server options (unary and stream
	// interceptors) to the server options passed in by the user.
	serverOpts := []grpc.ServerOption{grpc.Creds(insecure.NewCredentials())}
	wantServerOpts := len(serverOpts) + 2

	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface {
		if got := len(opts); got != wantServerOpts {
			t.Fatalf("%d ServerOptions passed to grpc.Server, want %d", got, wantServerOpts)
		}
		// Verify that the user passed ServerOptions are forwarded as is.
		if !reflect.DeepEqual(opts[2:], serverOpts) {
			t.Fatalf("got ServerOptions %v, want %v", opts[2:], serverOpts)
		}
		return newFakeGRPCServer()
	}
	defer func() {
		newGRPCServer = origNewGRPCServer
	}()

	s := NewGRPCServer(serverOpts...)
	defer s.Stop()
}

func (s) TestRegisterService(t *testing.T) {
	fs := newFakeGRPCServer()

	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface { return fs }
	defer func() { newGRPCServer = origNewGRPCServer }()

	s := NewGRPCServer()
	defer s.Stop()

	s.RegisterService(&grpc.ServiceDesc{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fs.registerServiceCh.Receive(ctx); err != nil {
		t.Fatalf("timeout when expecting RegisterService() to called on grpc.Server: %v", err)
	}
}

// setupOverrides sets up overrides for bootstrap config, new xdsClient creation
// and new gRPC.Server creation.
func setupOverrides(t *testing.T) (*fakeGRPCServer, *testutils.Channel, func()) {
	t.Helper()

	origNewXDSConfig := newXDSConfig
	newXDSConfig = func() (*bootstrap.Config, error) {
		return &bootstrap.Config{
			BalancerName: "dummyBalancer",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:    xdstestutils.EmptyNodeProtoV3,
		}, nil
	}

	clientCh := testutils.NewChannel()
	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		c := fakeclient.NewClient()
		clientCh.Send(c)
		return c, nil
	}

	fs := newFakeGRPCServer()
	origNewGRPCServer := newGRPCServer
	newGRPCServer = func(opts ...grpc.ServerOption) grpcServerInterface { return fs }

	return fs, clientCh, func() {
		newXDSConfig = origNewXDSConfig
		newXDSClient = origNewXDSClient
		newGRPCServer = origNewGRPCServer
	}
}

// TestServeSuccess tests the successful case of calling Serve().
// The following sequence of events happen:
// 1. Create a new GRPCServer and call Serve() in a goroutine.
// 2. Make sure an xdsClient is created, and an LDS watch is registered.
// 3. Push an error response from the xdsClient, and make sure that Serve() does
//    not exit.
// 4. Push a good response from the xdsClient, and make sure that Serve() on the
// 	  underlying grpc.Server is called.
func (s) TestServeSuccess(t *testing.T) {
	fs, clientCh, cleanup := setupOverrides(t)
	defer cleanup()

	server := NewGRPCServer()
	defer server.Stop()

	localAddr, err := xdstestutils.AvailableHostPort()
	if err != nil {
		t.Fatalf("testutils.AvailableHostPort() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(ServeOptions{Address: localAddr}); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("grpc/server?udpa.resource.listening_address=%s", localAddr)
	if name != wantName {
		t.Fatalf("LDS watch registered for name %q, want %q", name, wantName)
	}

	// Push an error to the registered listener watch callback and make sure
	// that Serve does not return.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{}, errors.New("LDS error"))
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := serveDone.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() returned after a bad LDS response")
	}

	// Push a good LDS response, and wait for Serve() to be invoked on the
	// underlying grpc.Server.
	client.InvokeWatchListenerCallback(xdsclient.ListenerUpdate{RouteConfigName: "routeconfig"}, nil)
	if _, err := fs.serveCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to be invoked on the grpc.Server")
	}
}

// TestServeWithStop tests the case where Stop() is called before an LDS update
// is received. This should cause Serve() to exit before calling Serve() on the
// underlying grpc.Server.
func (s) TestServeWithStop(t *testing.T) {
	fs, clientCh, cleanup := setupOverrides(t)
	defer cleanup()

	// Note that we are not deferring the Stop() here since we explicitly call
	// it after the LDS watch has been registered.
	server := NewGRPCServer()

	localAddr, err := xdstestutils.AvailableHostPort()
	if err != nil {
		t.Fatalf("testutils.AvailableHostPort() failed: %v", err)
	}

	// Call Serve() in a goroutine, and push on a channel when Serve returns.
	serveDone := testutils.NewChannel()
	go func() {
		if err := server.Serve(ServeOptions{Address: localAddr}); err != nil {
			t.Error(err)
		}
		serveDone.Send(nil)
	}()

	// Wait for an xdsClient to be created.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := clientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for new xdsClient to be created: %v", err)
	}
	client := c.(*fakeclient.Client)

	// Wait for a listener watch to be registered on the xdsClient.
	name, err := client.WaitForWatchListener(ctx)
	if err != nil {
		server.Stop()
		t.Fatalf("error when waiting for a ListenerWatch: %v", err)
	}
	wantName := fmt.Sprintf("grpc/server?udpa.resource.listening_address=%s", localAddr)
	if name != wantName {
		server.Stop()
		t.Fatalf("LDS watch registered for name %q, wantPrefix %q", name, wantName)
	}

	// Call Stop() on the server before a listener update is received, and
	// expect Serve() to exit.
	server.Stop()
	if _, err := serveDone.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Serve() to exit")
	}

	// Make sure that Serve() on the underlying grpc.Server is not called.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := fs.serveCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Serve() called on underlying grpc.Server")
	}
}

// TestServeBootstrapFailure tests the case where xDS bootstrap fails and
// verifies that Serve() exits with a non-nil error.
func (s) TestServeBootstrapFailure(t *testing.T) {
	// Since we have not setup fakes for anything, this will attempt to do real
	// xDS bootstrap and that will fail because the bootstrap environment
	// variable is not set.
	server := NewGRPCServer()
	defer server.Stop()

	localAddr, err := xdstestutils.AvailableHostPort()
	if err != nil {
		t.Fatalf("testutils.AvailableHostPort() failed: %v", err)
	}

	serveDone := testutils.NewChannel()
	go func() {
		err := server.Serve(ServeOptions{Address: localAddr})
		serveDone.Send(err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}
}

// TestServeNewClientFailure tests the case where xds client creation fails and
// verifies that Server() exits with a non-nil error.
func (s) TestServeNewClientFailure(t *testing.T) {
	origNewXDSConfig := newXDSConfig
	newXDSConfig = func() (*bootstrap.Config, error) {
		return &bootstrap.Config{
			BalancerName: "dummyBalancer",
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			NodeProto:    xdstestutils.EmptyNodeProtoV3,
		}, nil
	}
	defer func() { newXDSConfig = origNewXDSConfig }()

	origNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		return nil, errors.New("xdsClient creation failed")
	}
	defer func() { newXDSClient = origNewXDSClient }()

	server := NewGRPCServer()
	defer server.Stop()

	localAddr, err := xdstestutils.AvailableHostPort()
	if err != nil {
		t.Fatalf("testutils.AvailableHostPort() failed: %v", err)
	}

	serveDone := testutils.NewChannel()
	go func() {
		err := server.Serve(ServeOptions{Address: localAddr})
		serveDone.Send(err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := serveDone.Receive(ctx)
	if err != nil {
		t.Fatalf("error when waiting for Serve() to exit: %v", err)
	}
	if err, ok := v.(error); !ok || err == nil {
		t.Fatal("Serve() did not exit with error")
	}
}
