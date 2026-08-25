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

package xds_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/proto"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type dummyFilterBuilder struct {
	httpfilter.Builder
	typeURL           string
	interceptRPCCount *atomic.Int32
	recvMsgCount      *atomic.Int32
	sendMsgCount      *atomic.Int32
	interceptRPCFunc  func(ss grpc.ServerStream) (grpc.ServerStream, error)
}

func (b *dummyFilterBuilder) IsTerminal() bool   { return false }
func (b *dummyFilterBuilder) TypeURLs() []string { return []string{b.typeURL} }
func (b *dummyFilterBuilder) ParseFilterConfig(proto.Message, httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	return dummyFilterConfig{}, nil
}
func (b *dummyFilterBuilder) ParseFilterConfigOverride(proto.Message, httpfilter.ParseOptions) (httpfilter.FilterConfig, error) {
	return dummyFilterConfig{}, nil
}
func (b *dummyFilterBuilder) BuildServerFilter() httpfilter.ServerFilter {
	return b
}
func (b *dummyFilterBuilder) Close() {}

type dummyFilterConfig struct {
	httpfilter.FilterConfig
}

func (b *dummyFilterBuilder) BuildServerInterceptor(httpfilter.FilterConfig, httpfilter.FilterConfig) (httpfilter.ServerInterceptor, error) {
	return &dummyInterceptor{
		builder: b,
	}, nil
}

type dummyInterceptor struct {
	builder *dummyFilterBuilder
}

func (i *dummyInterceptor) InterceptRPC(ss grpc.ServerStream) (grpc.ServerStream, error) {
	if i.builder.interceptRPCCount != nil {
		i.builder.interceptRPCCount.Add(1)
	}
	if i.builder.interceptRPCFunc != nil {
		return i.builder.interceptRPCFunc(ss)
	}
	return &wrappedServerStream{
		ServerStream: ss,
		builder:      i.builder,
	}, nil
}

func (i *dummyInterceptor) Close() {}

// wrappedServerStream wraps grpc.ServerStream to intercept and count
// RecvMsg and SendMsg invocations for testing.
type wrappedServerStream struct {
	grpc.ServerStream
	builder *dummyFilterBuilder
}

func (w *wrappedServerStream) RecvMsg(m any) error {
	err := w.ServerStream.RecvMsg(m)
	if err == nil && w.builder.recvMsgCount != nil {
		w.builder.recvMsgCount.Add(1)
	}
	return err
}

func (w *wrappedServerStream) SendMsg(m any) error {
	err := w.ServerStream.SendMsg(m)
	if err == nil && w.builder.sendMsgCount != nil {
		w.builder.sendMsgCount.Add(1)
	}
	return err
}

type rpcType int

const (
	rpcTypeUnary rpcType = iota
	rpcTypeStreaming
)

func makeRPC(ctx context.Context, client testgrpc.TestServiceClient, rType rpcType) error {
	switch rType {
	case rpcTypeUnary:
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err != nil {
			return fmt.Errorf("EmptyCall() failed: %w", err)
		}
		return nil
	case rpcTypeStreaming:
		stream, err := client.FullDuplexCall(ctx)
		if err != nil {
			return fmt.Errorf("FullDuplexCall() failed: %w", err)
		}
		if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
			return fmt.Errorf("stream.Send() failed: %w", err)
		}
		if err := stream.CloseSend(); err != nil {
			return fmt.Errorf("stream.CloseSend() failed: %w", err)
		}
		if _, err := stream.Recv(); err != io.EOF {
			return err
		}
		return nil
	}
	return nil
}

// setupXDSListenerAndClient starts an xDS management server and a gRPC server,
// configures the management server with an inbound listener containing the
// specified httpFilters, dials the server using xDS, waits for the server to
// enter SERVING mode, and returns a TestServiceClient.
func setupXDSListenerAndClient(t *testing.T, httpFilters ...*v3httppb.HttpFilter) testgrpc.TestServiceClient {
	t.Helper()
	const serviceName = "my-service"
	managementServer, nodeID, bootstrapContents, xdsResolver := setup.ManagementServerAndResolver(t)

	// Wait for the server to enter SERVING mode before making RPCs to avoid
	// flakes due to the server closing connections.
	servingCh := make(chan struct{})
	opt := xds.ServingModeCallback(func(_ net.Addr, args xds.ServingModeChangeArgs) {
		if args.Mode == connectivity.ServingModeServing {
			close(servingCh)
		}
	})
	lis, stopServer := setupGRPCServer(t, bootstrapContents, opt)
	t.Cleanup(stopServer)

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	vhs := []*v3routepb.VirtualHost{{
		Domains: []string{"*"},
		Routes: []*v3routepb.Route{{
			Match: &v3routepb.RouteMatch{
				PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
			},
			Action: &v3routepb.Route_NonForwardingAction{},
		}},
	}}

	filters := append([]*v3httppb.HttpFilter{}, httpFilters...)
	filters = append(filters, e2e.HTTPFilter("router", &v3routerpb.Router{}))

	networkFilters := []*v3listenerpb.Filter{{
		Name: "hcm",
		ConfigType: &v3listenerpb.Filter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				HttpFilters: filters,
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name:         "routeName",
						VirtualHosts: vhs,
					},
				},
			}),
		},
	}}

	inboundLis := &v3listenerpb.Listener{
		Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		DefaultFilterChain: &v3listenerpb.FilterChain{Filters: networkFilters},
	}
	resources.Listeners = append(resources.Listeners, inboundLis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatalf("managementServer.Update() failed: %v", err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	select {
	case <-servingCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for server to enter SERVING mode")
	}

	return testgrpc.NewTestServiceClient(cc)
}

// Test verifies that the xDS server-side HTTP filter chain correctly executes
// InterceptRPC and wraps grpc.ServerStream for both Unary and Streaming RPCs.
func (s) TestServerSideXDSHTTPFilter_InterceptRPC(t *testing.T) {
	dummyTypeURL := t.Name()
	fb := &dummyFilterBuilder{
		typeURL:           dummyTypeURL,
		interceptRPCCount: &atomic.Int32{},
		recvMsgCount:      &atomic.Int32{},
		sendMsgCount:      &atomic.Int32{},
	}
	httpfilter.Register(fb)
	defer httpfilter.UnregisterForTesting(fb.typeURL)

	dummyFilter := newHTTPFilter(t, "dummy-filter", dummyTypeURL, "")
	client := setupXDSListenerAndClient(t, dummyFilter)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify Unary RPC execution.
	if err := makeRPC(ctx, client, rpcTypeUnary); err != nil {
		t.Fatalf("makeRPC() failed: %v", err)
	}
	if got := fb.interceptRPCCount.Load(); got != 1 {
		t.Fatalf("Unexpected interceptRPCCount for unary RPC, got %d, want 1", got)
	}
	if got := fb.recvMsgCount.Load(); got != 1 {
		t.Fatalf("Unexpected recvMsgCount for unary RPC, got %d, want 1", got)
	}
	if got := fb.sendMsgCount.Load(); got != 1 {
		t.Fatalf("Unexpected sendMsgCount for unary RPC, got %d, want 1", got)
	}

	// Reset counters for streaming rpc.
	fb.interceptRPCCount.Store(0)
	fb.recvMsgCount.Store(0)
	fb.sendMsgCount.Store(0)

	// Verify Streaming RPC execution.
	if err := makeRPC(ctx, client, rpcTypeStreaming); err != nil {
		t.Fatalf("makeRPC() failed: %v", err)
	}
	if got := fb.interceptRPCCount.Load(); got != 1 {
		t.Fatalf("Unexpected interceptRPCCount for streaming RPC, got %d, want 1", got)
	}
	if got := fb.recvMsgCount.Load(); got != 1 {
		t.Fatalf("Unexpected recvMsgCount for streaming RPC, got %d, want 1", got)
	}
}

// Test verifies that when a server-side xDS HTTP filter returns an error from
// InterceptRPC, the RPC is rejected early with the expected error for both
// Unary and Streaming RPCs.
func (s) TestServerSideXDSHTTPFilter_InterceptRPCEarlyRejection(t *testing.T) {
	typeURL := t.Name()
	const wantErr = "access denied by xDS server filter"
	fb := &dummyFilterBuilder{
		typeURL:           typeURL,
		interceptRPCCount: &atomic.Int32{},
		interceptRPCFunc: func(grpc.ServerStream) (grpc.ServerStream, error) {
			return nil, status.Error(codes.PermissionDenied, wantErr)
		},
	}
	httpfilter.Register(fb)
	defer httpfilter.UnregisterForTesting(fb.typeURL)

	rejectFilter := newHTTPFilter(t, "reject-filter", typeURL, "")
	client := setupXDSListenerAndClient(t, rejectFilter)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify Unary RPC rejection.
	if err := makeRPC(ctx, client, rpcTypeUnary); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("makeRPC() returned error = %v, want error containing %q", err, wantErr)
	}
	if got := fb.interceptRPCCount.Load(); got != 1 {
		t.Fatalf("Unexpected interceptRPCCount for unary RPC, got %d, want 1", got)
	}

	// Verify Streaming RPC rejection.
	fb.interceptRPCCount.Store(0) // reset counter for streaming rpc
	if err := makeRPC(ctx, client, rpcTypeStreaming); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("makeRPC() returned error = %v, want error containing %q", err, wantErr)
	}
	if got := fb.interceptRPCCount.Load(); got != 1 {
		t.Fatalf("Unexpected interceptRPCCount for streaming RPC, got %d, want 1", got)
	}
}

// Test verifies that multiple server-side xDS HTTP filters in a chain execute
// their InterceptRPC methods sequentially in the configured order for both
// Unary and Streaming RPCs.
func (s) TestServerSideXDS_InterceptRPCMultiFilterChaining(t *testing.T) {
	typeURL1, typeURL2 := fmt.Sprintf("%s-1", t.Name()), fmt.Sprintf("%s-2", t.Name())
	eventsCh := make(chan string, 2)
	const filter1, filter2 = "intercepted-filter-1", "intercepted-filter-2"

	fb1 := &dummyFilterBuilder{
		typeURL: typeURL1,
		interceptRPCFunc: func(ss grpc.ServerStream) (grpc.ServerStream, error) {
			eventsCh <- filter1
			return ss, nil
		},
	}
	fb2 := &dummyFilterBuilder{
		typeURL: typeURL2,
		interceptRPCFunc: func(ss grpc.ServerStream) (grpc.ServerStream, error) {
			eventsCh <- filter2
			return ss, nil
		},
	}

	httpfilter.Register(fb1)
	defer httpfilter.UnregisterForTesting(fb1.typeURL)
	httpfilter.Register(fb2)
	defer httpfilter.UnregisterForTesting(fb2.typeURL)

	f1 := newHTTPFilter(t, "filter-1", typeURL1, "")
	f2 := newHTTPFilter(t, "filter-2", typeURL2, "")

	client := setupXDSListenerAndClient(t, f1, f2)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify Unary RPC filter chaining.
	if err := makeRPC(ctx, client, rpcTypeUnary); err != nil {
		t.Fatalf("makeRPC() failed for unary RPC: %v", err)
	}
	select {
	case e := <-eventsCh:
		if e != filter1 {
			t.Fatalf("First intercept event = %q, want %q", e, filter1)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for %q", filter1)
	}

	select {
	case e := <-eventsCh:
		if e != filter2 {
			t.Fatalf("Second intercept event = %q, want %q", e, filter2)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for %q", filter2)
	}

	// Verify Streaming RPC filter chaining.
	if err := makeRPC(ctx, client, rpcTypeStreaming); err != nil {
		t.Fatalf("makeRPC() failed for streaming RPC: %v", err)
	}
	select {
	case e := <-eventsCh:
		if e != filter1 {
			t.Fatalf("First intercept event = %q, want %q", e, filter1)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for %q", filter1)
	}

	select {
	case e := <-eventsCh:
		if e != filter2 {
			t.Fatalf("Second intercept event = %q, want %q", e, filter2)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for %q", filter2)
	}
}

type errorWrappedStream struct {
	grpc.ServerStream
	recvErr string
}

func (w *errorWrappedStream) RecvMsg(m any) error {
	if w.recvErr != "" {
		return status.Error(codes.Internal, w.recvErr)
	}
	return w.ServerStream.RecvMsg(m)
}

// Test verifies that when multiple filters wrap the server stream and a
// filter's overrided RecvMsg returns an error, that error is properly
// propagated back to the client.
func (s) TestServerSideXDS_InterceptRPCMultiFilterErrorPropagation(t *testing.T) {
	typeURL1, typeURL2 := fmt.Sprintf("%s-1", t.Name()), fmt.Sprintf("%s-2", t.Name())

	fb1 := &dummyFilterBuilder{
		typeURL: typeURL1,
		interceptRPCFunc: func(ss grpc.ServerStream) (grpc.ServerStream, error) {
			return &wrappedServerStream{ServerStream: ss}, nil
		},
	}

	wantErr := "recvMsg failed in filter 2"
	fb2 := &dummyFilterBuilder{
		typeURL: typeURL2,
		interceptRPCFunc: func(ss grpc.ServerStream) (grpc.ServerStream, error) {
			return &errorWrappedStream{ServerStream: ss, recvErr: wantErr}, nil
		},
	}

	httpfilter.Register(fb1)
	defer httpfilter.UnregisterForTesting(fb1.typeURL)
	httpfilter.Register(fb2)
	defer httpfilter.UnregisterForTesting(fb2.typeURL)

	filter1 := newHTTPFilter(t, "filter-1", typeURL1, "")
	filter2 := newHTTPFilter(t, "filter-2", typeURL2, "")

	client := setupXDSListenerAndClient(t, filter1, filter2)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed to start stream: %v", err)
	}

	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v", err)
	}
	if _, err := stream.Recv(); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("stream.Recv() returned error = %v, want error containing %q", err, wantErr)
	}
}
