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
	"testing"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/httpfilter"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func (s) TestDefaultStreamInterceptor_InteractionWithXDSFilters(t *testing.T) {
	// Register a custom xDS filter builder for the test.
	testFilterTypeURL := t.Name()
	filterBuilder := newTrackingHTTPFilterBuilder(testFilterTypeURL)
	httpfilter.Register(filterBuilder)
	defer httpfilter.UnregisterForTesting(testFilterTypeURL)

	// Setup a test backend implementing both all four RPC types.
	testServer := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		StreamingOutputCallF: func(_ *testpb.StreamingOutputCallRequest, stream testgrpc.TestService_StreamingOutputCallServer) error {
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
		StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
			}
			return stream.SendAndClose(&testpb.StreamingInputCallResponse{})
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{}); err != nil {
					return err
				}
			}
		},
	}
	if err := testServer.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer testServer.Stop()

	// Start an xDS management server.
	mgmtServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const serviceName = "my-service-xds"
	clusterSpec := &v3routepb.RouteAction_Cluster{Cluster: "cluster-A"}
	hcm := &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: &v3routepb.RouteConfiguration{
				Name: "route-" + serviceName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{serviceName},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: clusterSpec,
						}},
					}},
				}},
			},
		},
		HttpFilters: []*v3httppb.HttpFilter{
			{
				Name: "tracking-filter",
				ConfigType: &v3httppb.HttpFilter_TypedConfig{
					TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
						TypeUrl: testFilterTypeURL,
					}),
				},
			},
			e2e.RouterHTTPFilter,
		},
	}
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{{Name: serviceName, ApiListener: &v3listenerpb.ApiListener{ApiListener: testutils.MarshalAny(t, hcm)}}},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster("cluster-A", "cluster-A", e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint("cluster-A", "localhost", []uint32{testutils.ParsePort(t, testServer.Address)})},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	// Make a unary RPC.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := filterBuilder.recvMsgCount.Load(), int32(2); got != want { // One for the request, one for the trailers.
		t.Fatalf("%d calls to RecvMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.sendMsgCount.Load(), int32(1); got != want { // One for the response.
		t.Fatalf("%d calls to SendMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.closeSendCount.Load(), int32(1); got != want { // CloseSend() is called once for the unary RPC.
		t.Fatalf("%d calls to CloseSend(), want %d", got, want)
	}

	// Make a client-streaming RPC, sending two messages and receiving one.
	clientStreamingRPC, err := client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("StreamingInputCall() failed: %v", err)
	}
	if err := clientStreamingRPC.Send(&testpb.StreamingInputCallRequest{}); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}
	if err := clientStreamingRPC.Send(&testpb.StreamingInputCallRequest{}); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}
	if _, err := clientStreamingRPC.CloseAndRecv(); err != nil {
		t.Fatalf("CloseAndRecv() failed: %v", err)
	}
	if got, want := filterBuilder.recvMsgCount.Load(), int32(4); got != want { // One for the request, one for the trailers.
		t.Fatalf("%d calls to RecvMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.sendMsgCount.Load(), int32(3); got != want { // Two for responses.
		t.Fatalf("%d calls to SendMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.closeSendCount.Load(), int32(2); got != want { // CloseSend() is called as part of CloseAndRecv().
		t.Fatalf("%d calls to CloseSend(), want %d", got, want)
	}

	// Make a server-streaming RPC, sending one message and receiving one.
	serverStreamingRPC, err := client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatalf("StreamingOutputCall() failed: %v", err)
	}
	if _, err := serverStreamingRPC.Recv(); err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}
	if got, want := filterBuilder.recvMsgCount.Load(), int32(5); got != want { // One for the request, none for the trailers.
		t.Fatalf("%d calls to RecvMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.sendMsgCount.Load(), int32(4); got != want { // One for the response.
		t.Fatalf("%d calls to SendMsg(), want %d", got, want)
	}
	// CloseSend() is invoked by the proto generated code after sending the
	// request message. It is also invoked by the defaultStreamInterceptor to
	// handle cases where the user is using the ClientStream API instead of the
	// proto generated code.
	if got, want := filterBuilder.closeSendCount.Load(), int32(4); got != want {
		t.Fatalf("%d calls to CloseSend(), want %d", got, want)
	}

	// Make a bidirectional-streaming RPC, sending one message and receiving one.
	bidiStreamingRPC, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall() failed: %v", err)
	}
	if err := bidiStreamingRPC.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}
	if _, err := bidiStreamingRPC.Recv(); err != nil {
		t.Fatalf("Recv() failed: %v", err)
	}
	if got, want := filterBuilder.recvMsgCount.Load(), int32(6); got != want { // One for the request, none for the trailers.
		t.Fatalf("%d calls to RecvMsg(), want %d", got, want)
	}
	if got, want := filterBuilder.sendMsgCount.Load(), int32(5); got != want { // One for the response.
		t.Fatalf("%d calls to SendMsg(), want %d", got, want)
	}
	// Bidi stream is still open, so CloseSend() is yet to be called.
	if got, want := filterBuilder.closeSendCount.Load(), int32(4); got != want {
		t.Fatalf("%d calls to CloseSend(), want %d", got, want)
	}
	bidiStreamingRPC.CloseSend()
	if got, want := filterBuilder.closeSendCount.Load(), int32(5); got != want {
		t.Fatalf("%d calls to CloseSend(), want %d", got, want)
	}
}
