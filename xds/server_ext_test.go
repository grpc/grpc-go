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

package xds_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/xdsclient"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

func hostPortFromListener(lis net.Listener) (string, uint32, error) {
	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		return "", 0, fmt.Errorf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port), nil
}

// servingModeChangeHandler handles changes to the serving mode of an
// xDS-enabled gRPC server. It logs the changes and sends the new mode and any
// errors on appropriate channels for the test to consume.
type servingModeChangeHandler struct {
	logger interface {
		Logf(format string, args ...any)
	}
	modeCh chan connectivity.ServingMode
	errCh  chan error
}

func newServingModeChangeHandler(t *testing.T) *servingModeChangeHandler {
	return &servingModeChangeHandler{
		logger: t,
		modeCh: make(chan connectivity.ServingMode, 1),
		errCh:  make(chan error, 1),
	}
}

func (m *servingModeChangeHandler) modeChangeCallback(addr net.Addr, args xds.ServingModeChangeArgs) {
	m.logger.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
	m.modeCh <- args.Mode
	if args.Err != nil {
		m.errCh <- args.Err
	}
}

// createStubServer creates a new xDS-enabled gRPC server and returns a
// stubserver.StubServer that can be used for testing. The server is configured
// with the provided modeChangeOpt and xdsclient.Pool.
func createStubServer(t *testing.T, lis net.Listener, opts ...grpc.ServerOption) *stubserver.StubServer {
	stub := &stubserver.StubServer{
		Listener: lis,
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if _, err := stream.Recv(); err == io.EOF {
					return nil
				} else if err != nil {
					return err
				}
			}
		},
	}
	server, err := xds.NewGRPCServer(opts...)
	if err != nil {
		t.Fatalf("Failed to create an xDS enabled gRPC server: %v", err)
	}
	stub.S = server
	stubserver.StartTestService(t, stub)
	t.Cleanup(stub.Stop)
	return stub
}

// waitForSuccessfulRPC waits for an RPC to succeed, repeatedly calling the
// EmptyCall RPC on the provided client connection until the call succeeds.
// If the context is canceled or the expected error is not before the context
// timeout expires, the test will fail.
func waitForSuccessfulRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn, opts ...grpc.CallOption) {
	t.Helper()

	client := testgrpc.NewTestServiceClient(cc)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RPCs to succeed")
		case <-time.After(defaultTestShortTimeout):
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}, opts...); err == nil {
				return
			}
		}
	}
}

// waitForFailedRPCWithStatus waits for an RPC to fail with the expected status
// code, error message, and node ID.  It repeatedly calls the EmptyCall RPC on
// the provided client connection until the error matches the expected values.
// If the context is canceled or the expected error is not before the context
// timeout expires, the test will fail.
func waitForFailedRPCWithStatus(ctx context.Context, t *testing.T, cc *grpc.ClientConn, wantCode codes.Code, wantErr, wantNodeID string) {
	t.Helper()

	client := testgrpc.NewTestServiceClient(cc)
	var err error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("RPCs failed with most recent error: %v. Want status code %v, error: %s, node id: %s", err, wantCode, wantErr, wantNodeID)
		case <-time.After(defaultTestShortTimeout):
			_, err = client.EmptyCall(ctx, &testpb.Empty{})
			if gotCode := status.Code(err); gotCode != wantCode {
				continue
			}
			if gotErr := err.Error(); !strings.Contains(gotErr, wantErr) {
				continue
			}
			if !strings.Contains(err.Error(), wantNodeID) {
				continue
			}
			t.Logf("Most recent error happy case: %v", err.Error())
			return
		}
	}
}

// Tests the basic scenario for an xDS enabled gRPC server.
//
//   - Verifies that the xDS enabled gRPC server requests for the expected
//     listener resource.
//   - Once the listener resource is received from the management server, it
//     verifies that the xDS enabled gRPC server requests for the appropriate
//     route configuration name. Also verifies that at this point, the server has
//     not yet started serving RPCs
//   - Once the route configuration is received from the management server, it
//     verifies that the server can serve RPCs successfully.
func (s) TestServer_Basic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	listenerNamesCh := make(chan []string, 1)
	routeNamesCh := make(chan []string, 1)
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			switch req.GetTypeUrl() {
			case "type.googleapis.com/envoy.config.listener.v3.Listener":
				select {
				case listenerNamesCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			case "type.googleapis.com/envoy.config.route.v3.RouteConfiguration":
				select {
				case routeNamesCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			}
			return nil
		},
		AllowResourceSubset: true,
	})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Create a listener on a local port to act as the xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	// Configure the managegement server with a listener resource for the above
	// xDS enabled gRPC server.
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Wait for the expected listener resource to be requested.
	wantLisResourceNames := []string{fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port))))}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the expected listener resource to be requested")
	case gotLisResourceName := <-listenerNamesCh:
		if !cmp.Equal(gotLisResourceName, wantLisResourceNames) {
			t.Fatalf("Got unexpected listener resource names: %v, want %v", gotLisResourceName, wantLisResourceNames)
		}
	}

	// Wait for the expected route config resource to be requested.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the expected route config resource to be requested")
	case gotRouteNames := <-routeNamesCh:
		if !cmp.Equal(gotRouteNames, []string{routeConfigName}) {
			t.Fatalf("Got unexpected route config resource names: %v, want %v", gotRouteNames, []string{routeConfigName})
		}
	}

	// Ensure that the server is not serving RPCs yet.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case <-modeChangeHandler.modeCh:
		t.Fatal("Server started serving RPCs before the route config was received")
	}

	// Create a gRPC channel to the xDS enabled server.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()

	// Ensure that the server isnt't serving RPCs successfully.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("EmptyCall() returned %v, want %v", err, codes.Unavailable)
	}

	// Configure the management server with the expected route config resource,
	// and expext RPCs to succeed.
	resources.Routes = []*v3routepb.RouteConfiguration{e2e.RouteConfigNonForwardingAction(routeConfigName)}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the server to start serving RPCs")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}
	waitForSuccessfulRPC(ctx, t, cc)
}

// Tests that the xDS-enabled gRPC server cleans up all its resources when all
// connections to it are closed.
func (s) TestServer_ConnectionCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Create a listener on a local port to act as the xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	// Configure the managegement server with a listener and route configuration
	// resource for the above xDS enabled gRPC server.
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		Routes:         []*v3routepb.RouteConfiguration{e2e.RouteConfigNonForwardingAction(routeConfigName)},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
	})
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create a gRPC channel and verify that RPCs succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()
	waitForSuccessfulRPC(ctx, t, cc)

	// Create multiple channels to the server, and make an RPC on each one. When
	// everything is closed, the server should have cleaned up all its resources
	// as well (and this will be verified by the leakchecker).
	const numConns = 100
	var wg sync.WaitGroup
	wg.Add(numConns)
	for range numConns {
		go func() {
			defer wg.Done()
			cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Errorf("grpc.NewClient failed with err: %v", err)
			}
			client := testgrpc.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Errorf("EmptyCall() failed: %v", err)
			}
			cc.Close()
		}()
	}
	wg.Wait()
}

// Tests that multiple xDS-enabled gRPC servers can be created with different
// bootstrap configurations, and that they correctly request different LDS
// resources from the management server based on their respective listening
// ports.  It also ensures that gRPC clients can connect to the intended server
// and that RPCs function correctly. The test uses the grpc.Peer() call option
// to validate that the client is connected to the correct server.
func (s) TestServer_MultipleServers_DifferentBootstrapConfigurations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create two bootstrap configurations pointing to the above management server.
	nodeID1 := uuid.New().String()
	bootstrapContents1 := e2e.DefaultBootstrapContents(t, nodeID1, mgmtServer.Address)
	nodeID2 := uuid.New().String()
	bootstrapContents2 := e2e.DefaultBootstrapContents(t, nodeID2, mgmtServer.Address)

	// Create two xDS-enabled gRPC servers using the above bootstrap configs.
	lis1, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis2, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	modeChangeHandler1 := newServingModeChangeHandler(t)
	modeChangeOpt1 := xds.ServingModeCallback(modeChangeHandler1.modeChangeCallback)
	modeChangeHandler2 := newServingModeChangeHandler(t)
	modeChangeOpt2 := xds.ServingModeCallback(modeChangeHandler2.modeChangeCallback)
	config1, err := bootstrap.NewConfigFromContents(bootstrapContents1)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents1), err)
	}
	pool1 := xdsclient.NewPool(config1)
	config2, err := bootstrap.NewConfigFromContents(bootstrapContents2)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents2), err)
	}
	pool2 := xdsclient.NewPool(config2)
	createStubServer(t, lis1, modeChangeOpt1, xds.ClientPoolForTesting(pool1))
	createStubServer(t, lis2, modeChangeOpt2, xds.ClientPoolForTesting(pool2))

	// Update the management server with the listener resources pointing to the
	// corresponding gRPC servers.
	host1, port1, err := hostPortFromListener(lis1)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	host2, port2, err := hostPortFromListener(lis2)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	resources1 := e2e.UpdateOptions{
		NodeID:    nodeID1,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultServerListener(host1, port1, e2e.SecurityLevelNone, "routeName")},
	}
	if err := mgmtServer.Update(ctx, resources1); err != nil {
		t.Fatal(err)
	}

	resources2 := e2e.UpdateOptions{
		NodeID:    nodeID2,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultServerListener(host2, port2, e2e.SecurityLevelNone, "routeName")},
	}
	if err := mgmtServer.Update(ctx, resources2); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go SERVING")
	case gotMode := <-modeChangeHandler1.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go SERVING")
	case gotMode := <-modeChangeHandler2.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}

	// Create two gRPC clients, one for each server.
	cc1, err := grpc.NewClient(lis1.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client for test server 1: %s, %v", lis1.Addr().String(), err)
	}
	defer cc1.Close()

	cc2, err := grpc.NewClient(lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client for test server 2: %s, %v", lis2.Addr().String(), err)
	}
	defer cc2.Close()

	// Both unary RPCs should work once the servers transitions into serving.
	var peer1 peer.Peer
	waitForSuccessfulRPC(ctx, t, cc1, grpc.Peer(&peer1))
	if peer1.Addr.String() != lis1.Addr().String() {
		t.Errorf("Connected to wrong peer: %s, want %s", peer1.Addr, lis1.Addr())
	}

	var peer2 peer.Peer
	waitForSuccessfulRPC(ctx, t, cc2, grpc.Peer(&peer2))
	if peer2.Addr.String() != lis2.Addr().String() {
		t.Errorf("Connected to wrong peer: %s, want %s", peer2.Addr, lis2.Addr())
	}
}
