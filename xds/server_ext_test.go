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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var errAcceptAndClose = status.New(codes.Unavailable, "")

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

// TestServingModeChanges tests the Server's logic as it transitions from Not
// Ready to Ready, then to Not Ready. Before it goes Ready, connections should
// be accepted and closed. After it goes ready, RPC's should proceed as normal
// according to matched route configuration. After it transitions back into not
// ready (through an explicit LDS Resource Not Found), previously running RPC's
// should be gracefully closed and still work, and new RPC's should fail.
func (s) TestServingModeChanges(t *testing.T) {
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	// Setup the management server to respond with a listener resource that
	// specifies a route name to watch. Due to not having received the full
	// configuration, this should cause the server to be in mode Serving.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}

	listener := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	serving := grpcsync.NewEvent()
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		if args.Mode == connectivity.ServingModeServing {
			serving.Fire()
		}
	})

	stub := &stubserver.StubServer{
		Listener: lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv() // hangs here forever if stream doesn't shut down...doesn't receive EOF without any errors
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	config, err := bootstrap.NewConfigForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create an bootstrap config from contents: %v, %v", bootstrapContents, err)
	}
	pool := xdsclient.NewPool(config)
	sopts := []grpc.ServerOption{grpc.Creds(insecure.NewCredentials()), modeChangeOpt, xds.ClientPoolForTesting(pool)}
	if stub.S, err = xds.NewGRPCServer(sopts...); err != nil {
		t.Fatalf("Failed to create an xDS enabled gRPC server: %v", err)
	}
	stubserver.StartTestService(t, stub)
	defer stub.S.Stop()
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	waitForFailedRPCWithStatus(ctx, t, cc, errAcceptAndClose)
	routeConfig := e2e.RouteConfigNonForwardingAction("routeName")
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
		Routes:    []*v3routepb.RouteConfiguration{routeConfig},
	}
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatal("timeout waiting for the xDS Server to go Serving")
	case <-serving.Done():
	}

	// A unary RPC should work once it transitions into serving. (need this same
	// assertion from LDS resource not found triggering it).
	waitForSuccessfulRPC(ctx, t, cc)

	// Start a stream before switching the server to not serving. Due to the
	// stream being created before the graceful stop of the underlying
	// connection, it should be able to continue even after the server switches
	// to not serving.
	c := testgrpc.NewTestServiceClient(cc)
	stream, err := c.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("cc.FullDuplexCall failed: %f", err)
	}

	// Lookup the xDS client in use based on the dedicated well-known key, as
	// defined in A71, used by the xDS enabled gRPC server.
	xdsC, close, err := pool.GetClientForTesting(xdsclient.NameForServer)
	if err != nil {
		t.Fatalf("Failed to find xDS client for configuration: %v", string(bootstrapContents))
	}
	defer close()

	// Invoke LDS Resource not found here (tests graceful close).
	triggerResourceNotFound := internal.TriggerXDSResourceNotFoundForTesting.(func(xdsclient.XDSClient, xdsresource.Type, string) error)
	listenerResourceType := xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)
	if err := triggerResourceNotFound(xdsC, listenerResourceType, listener.GetName()); err != nil {
		t.Fatalf("Failed to trigger resource name not found for testing: %v", err)
	}

	// New RPCs on that connection should eventually start failing. Due to
	// Graceful Stop any started streams continue to work.
	if err = stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v, should continue to work due to graceful stop", err)
	}
	if err = stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v, should continue to work due to graceful stop", err)
	}
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	// New RPCs on that connection should eventually start failing.
	waitForFailedRPCWithStatus(ctx, t, cc, errAcceptAndClose)
}

// TestServingModeChanges_MultipleServers tests two servers with unique
// bootstrap configuration are able to serve two different clients with same
// name if they are in different xDS client pools.
//
// It tests the Server's logic as it transitions from Not Ready to Ready, then
// then to Not Ready. Before it goes Ready, connections should be accepted and
// closed. After it goes ready, RPC's should proceed as normal according to
// matched route configuration. After it transitions back into not ready
// (through an explicit LDS Resource Not Found), previously running RPC's
// should be gracefully closed and still work, and new RPC's should fail.
func (s) TestServingModeChanges_MultipleServers(t *testing.T) {
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create two bootstrap configurations pointing to the above management server.
	nodeID1 := uuid.New().String()
	bootstrapContents1 := e2e.DefaultBootstrapContents(t, nodeID1, managementServer.Address)
	nodeID2 := uuid.New().String()
	bootstrapContents2 := e2e.DefaultBootstrapContents(t, nodeID2, managementServer.Address)

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	// Setup the management server to respond with a listener resource that
	// specifies a route name to watch. Due to not having received the full
	// configuration, this should cause the server to be in mode Serving.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}

	listener := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")
	resources1 := e2e.UpdateOptions{
		NodeID:         nodeID1,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}
	resources2 := e2e.UpdateOptions{
		NodeID:         nodeID2,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources1); err != nil {
		t.Fatal(err)
	}
	if err := managementServer.Update(ctx, resources2); err != nil {
		t.Fatal(err)
	}

	serving := grpcsync.NewEvent()
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		if args.Mode == connectivity.ServingModeServing {
			serving.Fire()
		}
	})

	stub1 := &stubserver.StubServer{
		Listener: lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv() // hangs here forever if stream doesn't shut down...doesn't receive EOF without any errors
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	stub2 := &stubserver.StubServer{
		Listener: lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv() // hangs here forever if stream doesn't shut down...doesn't receive EOF without any errors
				if err == io.EOF {
					return nil
				}
			}
		},
	}

	// Create two xDS client pools with different bootstrap contents.
	config1, err := bootstrap.NewConfigForTesting(bootstrapContents1)
	if err != nil {
		t.Fatalf("Failed to create an bootstrap config from contents: %v, %v", bootstrapContents1, err)
	}
	pool1 := xdsclient.NewPool(config1)
	sopts1 := []grpc.ServerOption{grpc.Creds(insecure.NewCredentials()), modeChangeOpt, xds.ClientPoolForTesting(pool1)}
	if stub1.S, err = xds.NewGRPCServer(sopts1...); err != nil {
		t.Fatalf("Failed to create an xDS enabled gRPC server: %v", err)
	}
	config2, err := bootstrap.NewConfigForTesting(bootstrapContents2)
	if err != nil {
		t.Fatalf("Failed to create an bootstrap config from contents: %v, %v", bootstrapContents2, err)
	}
	pool2 := xdsclient.NewPool(config2)
	sopts2 := []grpc.ServerOption{grpc.Creds(insecure.NewCredentials()), modeChangeOpt, xds.ClientPoolForTesting(pool2)}
	if stub2.S, err = xds.NewGRPCServer(sopts2...); err != nil {
		t.Fatalf("Failed to create an xDS enabled gRPC server: %v", err)
	}

	// Start the servers and make calls to them.
	stubserver.StartTestService(t, stub1)
	defer stub1.S.Stop()
	cc1, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc1.Close()

	stubserver.StartTestService(t, stub2)
	defer stub2.S.Stop()
	cc2, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc2.Close()

	waitForFailedRPCWithStatus(ctx, t, cc1, errAcceptAndClose)
	waitForFailedRPCWithStatus(ctx, t, cc2, errAcceptAndClose)

	routeConfig := e2e.RouteConfigNonForwardingAction("routeName")
	resources1 = e2e.UpdateOptions{
		NodeID:    nodeID1,
		Listeners: []*v3listenerpb.Listener{listener},
		Routes:    []*v3routepb.RouteConfiguration{routeConfig},
	}
	resources2 = e2e.UpdateOptions{
		NodeID:    nodeID2,
		Listeners: []*v3listenerpb.Listener{listener},
		Routes:    []*v3routepb.RouteConfiguration{routeConfig},
	}
	defer cancel()
	if err := managementServer.Update(ctx, resources1); err != nil {
		t.Fatal(err)
	}
	if err := managementServer.Update(ctx, resources2); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatal("timeout waiting for the xDS Server to go Serving")
	case <-serving.Done():
	}

	// A unary RPC should work once it transitions into serving. (need this same
	// assertion from LDS resource not found triggering it).
	waitForSuccessfulRPC(ctx, t, cc1)
	waitForSuccessfulRPC(ctx, t, cc2)

	// Start a stream before switching the server to not serving. Due to the
	// stream being created before the graceful stop of the underlying
	// connection, it should be able to continue even after the server switches
	// to not serving.
	c1 := testgrpc.NewTestServiceClient(cc1)
	stream1, err := c1.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("cc.FullDuplexCall failed: %f", err)
	}
	c2 := testgrpc.NewTestServiceClient(cc2)
	stream2, err := c2.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("cc.FullDuplexCall failed: %f", err)
	}

	// Lookup the xDS client in use based on the dedicated well-known key, as
	// defined in A71, used by the xDS enabled gRPC server.
	xdsC1, close1, err := pool1.GetClientForTesting(xdsclient.NameForServer)
	if err != nil {
		t.Fatalf("Failed to find xDS client for configuration: %v", string(bootstrapContents1))
	}
	defer close1()
	xdsC2, close2, err := pool2.GetClientForTesting(xdsclient.NameForServer)
	if err != nil {
		t.Fatalf("Failed to find xDS client for configuration: %v", string(bootstrapContents1))
	}
	defer close2()

	// Compare the bootstrap configurations for both xDS clients.
	if want, _ := bootstrap.NewConfigForTesting(bootstrapContents1); !cmp.Equal(want, xdsC1.BootstrapConfig()) {
		t.Fatalf("want %v bootstrap config from xdsC1, got %v", want, xdsC1.BootstrapConfig())
	}
	if want, _ := bootstrap.NewConfigForTesting(bootstrapContents2); !cmp.Equal(want, xdsC2.BootstrapConfig()) {
		t.Fatalf("want %v bootstrap config from xdsC2, got %v", want, xdsC2.BootstrapConfig())
	}

	// Invoke LDS Resource not found here (tests graceful close).
	triggerResourceNotFound := internal.TriggerXDSResourceNotFoundForTesting.(func(xdsclient.XDSClient, xdsresource.Type, string) error)
	listenerResourceType := xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL].(xdsresource.Type)
	if err := triggerResourceNotFound(xdsC1, listenerResourceType, listener.GetName()); err != nil {
		t.Fatalf("Failed to trigger resource name not found for testing: %v", err)
	}
	if err := triggerResourceNotFound(xdsC2, listenerResourceType, listener.GetName()); err != nil {
		t.Fatalf("Failed to trigger resource name not found for testing: %v", err)
	}

	// New RPCs on that connection should eventually start failing. Due to
	// Graceful Stop any started streams continue to work.
	if err = stream1.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v, should continue to work due to graceful stop", err)
	}
	if err = stream1.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v, should continue to work due to graceful stop", err)
	}
	if _, err = stream1.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	if err = stream2.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v, should continue to work due to graceful stop", err)
	}
	if err = stream2.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v, should continue to work due to graceful stop", err)
	}
	if _, err = stream2.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	// New RPCs on that connection should eventually start failing.
	waitForFailedRPCWithStatus(ctx, t, cc1, errAcceptAndClose)
	waitForFailedRPCWithStatus(ctx, t, cc2, errAcceptAndClose)
}

// TestResourceNotFoundRDS tests the case where an LDS points to an RDS which
// returns resource not found. Before getting the resource not found, the xDS
// Server has not received all configuration needed, so it should Accept and
// Close any new connections. After it has received the resource not found
// error, the server should move to serving, successfully Accept Connections,
// and fail at the L7 level with resource not found specified.
func (s) TestResourceNotFoundRDS(t *testing.T) {
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	// Setup the management server to respond with a listener resource that
	// specifies a route name to watch, and no RDS resource corresponding to
	// this route name.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}

	const routeConfigResourceName = "routeName"
	listener := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, routeConfigResourceName)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	serving := grpcsync.NewEvent()
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		if args.Mode == connectivity.ServingModeServing {
			serving.Fire()
		}
	})

	stub := &stubserver.StubServer{
		Listener: lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv() // hangs here forever if stream doesn't shut down...doesn't receive EOF without any errors
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	config, err := bootstrap.NewConfigForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create an bootstrap config from contents: %v, %v", bootstrapContents, err)
	}
	pool := xdsclient.NewPool(config)
	sopts := []grpc.ServerOption{grpc.Creds(insecure.NewCredentials()), modeChangeOpt, xds.ClientPoolForTesting(pool)}
	if stub.S, err = xds.NewGRPCServer(sopts...); err != nil {
		t.Fatalf("Failed to create an xDS enabled gRPC server: %v", err)
	}
	stubserver.StartTestService(t, stub)
	defer stub.S.Stop()

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	waitForFailedRPCWithStatus(ctx, t, cc, errAcceptAndClose)

	// Lookup the xDS client in use based on the dedicated well-known key, as
	// defined in A71, used by the xDS enabled gRPC server.
	xdsC, close, err := pool.GetClientForTesting(xdsclient.NameForServer)
	if err != nil {
		t.Fatalf("Failed to find xDS client for configuration: %v", string(bootstrapContents))
	}
	defer close()

	// Invoke resource not found - this should result in L7 RPC error with
	// unavailable receive on serving as a result, should trigger it to go
	// serving. Poll as watch might not be started yet to trigger resource not
	// found.
	triggerResourceNotFound := internal.TriggerXDSResourceNotFoundForTesting.(func(xdsclient.XDSClient, xdsresource.Type, string) error)
	routeConfigResourceType := xdsinternal.ResourceTypeMapForTesting[version.V3RouteConfigURL].(xdsresource.Type)
loop:
	for {
		if err := triggerResourceNotFound(xdsC, routeConfigResourceType, routeConfigResourceName); err != nil {
			t.Fatalf("Failed to trigger resource name not found for testing: %v", err)
		}
		select {
		case <-serving.Done():
			break loop
		case <-ctx.Done():
			t.Fatalf("timed out waiting for serving mode to go serving")
		case <-time.After(time.Millisecond):
		}
	}
	waitForFailedRPCWithStatus(ctx, t, cc, status.New(codes.Unavailable, "error from xDS configuration for matched route configuration"))
}

func waitForSuccessfulRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn) {
	t.Helper()

	c := testgrpc.NewTestServiceClient(cc)
	if _, err := c.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// waitForFailedRPCWithStatus makes unary RPC's until it receives the expected
// status in a polling manner. Fails if the RPC made does not return the
// expected status before the context expires.
func waitForFailedRPCWithStatus(ctx context.Context, t *testing.T, cc *grpc.ClientConn, st *status.Status) {
	t.Helper()

	c := testgrpc.NewTestServiceClient(cc)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var err error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("failure when waiting for RPCs to fail with certain status %v: %v. most recent error received from RPC: %v", st, ctx.Err(), err)
		case <-ticker.C:
			_, err = c.EmptyCall(ctx, &testpb.Empty{})
			if status.Code(err) == st.Code() && strings.Contains(err.Error(), st.Message()) {
				t.Logf("most recent error happy case: %v", err.Error())
				return
			}
		}
	}
}
