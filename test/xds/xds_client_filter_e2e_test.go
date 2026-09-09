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

// Tests the interaction between the defaultStreamInterceptor and xDS filters.
// It verifies that the SendMsg, RecvMsg, and CloseSend methods are called the
// expected number of times for different RPC types (unary, client streaming,
// server streaming, and bidi streaming) when an xDS filter is registered and
// used in the call chain.
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
	client := testgrpc.NewTestServiceClient(cc)

	tests := []struct {
		name          string
		run           func(ctx context.Context, client testgrpc.TestServiceClient)
		wantSendMsg   int32
		wantRecvMsg   int32
		wantCloseSend int32
	}{
		{
			name: "unary",
			run: func(ctx context.Context, client testgrpc.TestServiceClient) {
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
					t.Fatalf("EmptyCall() failed: %v", err)
				}
			},
			wantSendMsg:   1, // 1 request
			wantRecvMsg:   2, // 1 response + 1 trailers
			wantCloseSend: 1, // CloseSend called by invoke
		},
		{
			name: "client_streaming",
			run: func(ctx context.Context, client testgrpc.TestServiceClient) {
				stream, err := client.StreamingInputCall(ctx)
				if err != nil {
					t.Fatalf("StreamingInputCall() failed: %v", err)
				}
				for i := 0; i < 2; i++ {
					if err := stream.Send(&testpb.StreamingInputCallRequest{}); err != nil {
						t.Fatalf("Send() failed: %v", err)
					}
				}
				if _, err := stream.CloseAndRecv(); err != nil {
					t.Fatalf("CloseAndRecv() failed: %v", err)
				}
			},
			wantSendMsg:   2, // 2 requests
			wantRecvMsg:   2, // 1 response + 1 trailers
			wantCloseSend: 1, // CloseSend called by CloseAndRecv
		},
		{
			name: "server_streaming",
			run: func(ctx context.Context, client testgrpc.TestServiceClient) {
				stream, err := client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
				if err != nil {
					t.Fatalf("StreamingOutputCall() failed: %v", err)
				}
				if _, err := stream.Recv(); err != nil {
					t.Fatalf("Recv() failed: %v", err)
				}
			},
			wantSendMsg:   1, // 1 request
			wantRecvMsg:   1, // 1 response (trailers not yet consumed since stream not read to EOF)
			wantCloseSend: 2, // 1 from defaultStreamInterceptor + 1 from proto generated code
		},
		{
			name: "bidi_streaming",
			run: func(ctx context.Context, client testgrpc.TestServiceClient) {
				stream, err := client.FullDuplexCall(ctx)
				if err != nil {
					t.Fatalf("FullDuplexCall() failed: %v", err)
				}
				if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
					t.Fatalf("Send() failed: %v", err)
				}
				if _, err := stream.Recv(); err != nil {
					t.Fatalf("Recv() failed: %v", err)
				}
				if err := stream.CloseSend(); err != nil {
					t.Fatalf("CloseSend() failed: %v", err)
				}
			},
			wantSendMsg:   1, // 1 request
			wantRecvMsg:   1, // 1 response
			wantCloseSend: 1, // 1 explicit CloseSend
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filterBuilder.sendMsgCount.Store(0)
			filterBuilder.recvMsgCount.Store(0)
			filterBuilder.closeSendCount.Store(0)

			tc.run(ctx, client)

			if got := filterBuilder.sendMsgCount.Load(); got != tc.wantSendMsg {
				t.Fatalf("SendMsg() count = %d, want %d", got, tc.wantSendMsg)
			}
			if got := filterBuilder.recvMsgCount.Load(); got != tc.wantRecvMsg {
				t.Fatalf("RecvMsg() count = %d, want %d", got, tc.wantRecvMsg)
			}
			if got := filterBuilder.closeSendCount.Load(); got != tc.wantCloseSend {
				t.Fatalf("CloseSend() count = %d, want %d", got, tc.wantCloseSend)
			}
		})
	}
}
