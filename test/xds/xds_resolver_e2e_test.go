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
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	_ "google.golang.org/grpc/xds"
)

type interceptingBuilder struct {
	resolver.Builder
	jsonCh chan string
}

func (ib *interceptingBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	icc := &interceptingClientConn{
		ClientConn: cc,
		jsonCh:     ib.jsonCh,
	}
	return ib.Builder.Build(target, icc, opts)
}

type interceptingClientConn struct {
	resolver.ClientConn
	jsonCh chan string
}

func (icc *interceptingClientConn) ParseServiceConfig(js string) *serviceconfig.ParseResult {
	select {
	case icc.jsonCh <- js:
	default:
	}
	return icc.ClientConn.ParseServiceConfig(js)
}

func (icc *interceptingClientConn) UpdateState(state resolver.State) error {
	return icc.ClientConn.UpdateState(state)
}

type dummyFilterCfg struct {
	httpfilter.FilterConfig
}

type testFilterBuilder struct {
	httpfilter.Builder
	typeURL      string
	blockChan    chan struct{}
	enteredChan  chan struct{}
	newStreamErr error
}

func (tb *testFilterBuilder) TypeURLs() []string { return []string{tb.typeURL} }

func (*testFilterBuilder) ParseFilterConfig(proto.Message) (httpfilter.FilterConfig, error) {
	return dummyFilterCfg{}, nil
}

func (*testFilterBuilder) ParseFilterConfigOverride(proto.Message) (httpfilter.FilterConfig, error) {
	return dummyFilterCfg{}, nil
}

func (*testFilterBuilder) IsTerminal() bool { return false }

func (tb *testFilterBuilder) BuildClientFilter() httpfilter.ClientFilter { return tb }

func (tb *testFilterBuilder) Close() {}

func (tb *testFilterBuilder) BuildClientInterceptor(httpfilter.FilterConfig, httpfilter.FilterConfig) (httpfilter.ClientInterceptor, error) {
	return &testInterceptor{
		blockChan:    tb.blockChan,
		enteredChan:  tb.enteredChan,
		newStreamErr: tb.newStreamErr,
	}, nil
}

type testInterceptor struct {
	blockChan    chan struct{}
	enteredChan  chan struct{}
	newStreamErr error
}

func (i *testInterceptor) NewStream(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	// Signal that we have entered the filter
	close(i.enteredChan)

	select {
	case <-i.blockChan:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if i.newStreamErr != nil {
		return nil, i.newStreamErr
	}
	return newStream(ctx, opts...)
}

func (i *testInterceptor) Close() {}

// wantServiceConfig returns a JSON representation of a service config with
// xds_cluster_manager_experimental LB policy with child policies of
// cds_experimental for the provided cluster names.
func wantServiceConfig(clusters ...string) string {
	var children []string
	for _, cluster := range clusters {
		children = append(children, fmt.Sprintf(`"cluster:%s": {
			"childPolicy": [{
				"cds_experimental": {
					"cluster": "%s"
				}
			}]
		}`, cluster, cluster))
	}
	return fmt.Sprintf(`{
		"loadBalancingConfig": [{
			"xds_cluster_manager_experimental": {
				"children": {
					%s
				}
			}
		}]
	}`, strings.Join(children, ","))
}

// compareJSONConfigs parses gotJSON and wantJSON using the internal gRPC
// Service Config parser and asserts that they represent the same configuration.
func compareJSONConfigs(t *testing.T, gotJSON, wantJSON string) {
	gotParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(gotJSON)
	if gotParsed.Err != nil {
		t.Fatalf("Failed to parse got service config %q: %v", gotJSON, gotParsed.Err)
	}
	wantParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(wantJSON)
	if wantParsed.Err != nil {
		t.Fatalf("Failed to parse want service config %q: %v", wantJSON, wantParsed.Err)
	}
	if !internal.EqualServiceConfigForTesting(gotParsed.Config, wantParsed.Config) {
		t.Fatalf("Service config mismatch.\nGot:\n%s\nWant:\n%s", gotJSON, wantJSON)
	}
}

// Test verifies that if an RPC is in flight, the old cluster remains in
// the service config until the stream is committed.
func (s) TestResolverDelayedOnCommitted(t *testing.T) {
	testFilterTypeURL := "type.googleapis.com/test.delayingFilter-"
	blockChan, enteredChan := make(chan struct{}), make(chan struct{})
	tb := &testFilterBuilder{
		typeURL:     testFilterTypeURL,
		blockChan:   blockChan,
		enteredChan: enteredChan,
	}
	httpfilter.Register(tb)
	defer httpfilter.UnregisterForTesting(tb.typeURL)

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	// Create the xDS resolver builder.
	underlyingResolver, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Create an intercepting resolver builder.
	jsonCh := make(chan string, 10)
	ib := &interceptingBuilder{
		Builder: underlyingResolver,
		jsonCh:  jsonCh,
	}

	// Start test backends.
	serverA := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			<-stream.Context().Done()
			return nil
		},
	})
	defer serverA.Stop()
	serverB := stubserver.StartTestService(t, &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			<-stream.Context().Done()
			return nil
		},
	})
	defer serverB.Stop()

	const (
		serviceName = "my-service-xds"
		clusterA    = "cluster-A"
		clusterB    = "cluster-B"
	)
	clusterSpec := &v3routepb.RouteAction_Cluster{Cluster: clusterA}
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
				Name: "delaying-filter",
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
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{{Name: serviceName, ApiListener: &v3listenerpb.ApiListener{ApiListener: testutils.MarshalAny(t, hcm)}}},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterA, clusterA, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(clusterA, "localhost", []uint32{testutils.ParsePort(t, serverA.Address)})},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a gRPC client using the intercepting resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(ib))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	// Trigger a RPC to cluster A. This RPC should reach our custom HTTP filter,
	// and get blocked.
	client := testgrpc.NewTestServiceClient(cc)
	streamErrCh := make(chan error, 1)
	go func() {
		s, err := client.FullDuplexCall(ctx)
		if err != nil {
			streamErrCh <- err
			return
		}
		// Force commit by calling Context()
		s.Context()
		streamErrCh <- nil
	}()

	// Read the first state update from the intercepting resolver.
	// This should contain cluster-A.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for first resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterA))
	}

	// Verify that the RPC has reached the filter's NewStream.
	select {
	case <-enteredChan:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for RPC to reach HTTP filter")
	}

	// Now update the route configuration on the management server to point to cluster B.
	clusterSpec.Cluster = clusterB
	resources.Listeners = []*v3listenerpb.Listener{{Name: serviceName, ApiListener: &v3listenerpb.ApiListener{ApiListener: testutils.MarshalAny(t, hcm)}}}
	resources.Clusters = append(resources.Clusters, e2e.DefaultCluster(clusterB, clusterB, e2e.SecurityLevelNone))
	resources.Endpoints = append(resources.Endpoints, e2e.DefaultEndpoint(clusterB, "localhost", []uint32{testutils.ParsePort(t, serverB.Address)}))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the second state update from the intercepting resolver.
	// This should contain BOTH cluster-A and cluster-B, since the blocked
	// RPC holds a reference to cluster-A.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for second resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterA, clusterB))
	}

	// Unblock the filter's NewStream, allowing it to complete successfully.
	close(blockChan)

	// Wait for the stream to be successfully created.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for stream creation to succeed")
	case err := <-streamErrCh:
		if err != nil {
			t.Fatalf("First RPC failed with unexpected error: %v", err)
		}
	}

	// Once the stream is created and committed, the resolver should unsubscribe
	// from cluster A and trigger a service config update that deletes the old
	// cluster. Read the third state update and ensure cluster-A is pruned.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for third resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterB))
	}
}

// Test verifies that if stream creation fails early inside an HTTP filter or
// interceptor (before the stream is created), the OnFinish CallOption is still
// executed. This decrements the cluster reference count to 0, allowing the
// resolver to unsubscribe and prune the old cluster from the service config.
func (s) TestResolverPrunesCluster_StreamCreationFailure(t *testing.T) {
	const (
		testFilterTypeURL = "type.googleapis.com/test.blockingFilter"
		wantErr           = "blocking filter error"
	)
	blockChan, enteredChan := make(chan struct{}), make(chan struct{})
	tb := &testFilterBuilder{
		typeURL:      testFilterTypeURL,
		blockChan:    blockChan,
		enteredChan:  enteredChan,
		newStreamErr: status.Error(codes.Unavailable, wantErr),
	}
	httpfilter.Register(tb)
	defer httpfilter.UnregisterForTesting(tb.typeURL)

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	// Create the underlying xDS resolver builder.
	underlyingResolver, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Intercept resolver builder.
	jsonCh := make(chan string, 10)
	ib := &interceptingBuilder{
		Builder: underlyingResolver,
		jsonCh:  jsonCh,
	}

	// Start test backends.
	serverA := stubserver.StartTestService(t, nil)
	defer serverA.Stop()
	serverB := stubserver.StartTestService(t, nil)
	defer serverB.Stop()

	const (
		serviceName = "my-service-xds"
		clusterA    = "cluster-A"
		clusterB    = "cluster-B"
	)
	clusterSpec := &v3routepb.RouteAction_Cluster{Cluster: clusterA}
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
				Name: "blocking-filter",
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
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{{Name: serviceName, ApiListener: &v3listenerpb.ApiListener{ApiListener: testutils.MarshalAny(t, hcm)}}},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterA, clusterA, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(clusterA, "localhost", []uint32{testutils.ParsePort(t, serverA.Address)})},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a gRPC client using the xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(ib))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	// Trigger a RPC to cluster A. This RPC should reach our custom HTTP filter,
	// and get blocked.
	client := testgrpc.NewTestServiceClient(cc)
	rpcErrCh := make(chan error, 1)
	go func() {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		rpcErrCh <- err
	}()

	// Read the first state update from the intercepting resolver.
	// This should contain cluster-A.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for first resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterA))
	}

	// Verify that the RPC has entered the HTTP filter's NewStream and is
	// currently blocked.
	select {
	case <-enteredChan:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for RPC to reach HTTP filter")
	}

	// Update the route configuration on the management server to point
	// to cluster B.
	clusterSpec.Cluster = clusterB
	resources.Listeners = []*v3listenerpb.Listener{{Name: serviceName, ApiListener: &v3listenerpb.ApiListener{ApiListener: testutils.MarshalAny(t, hcm)}}}
	resources.Clusters = append(resources.Clusters, e2e.DefaultCluster(clusterB, clusterB, e2e.SecurityLevelNone))
	resources.Endpoints = append(resources.Endpoints, e2e.DefaultEndpoint(clusterB, "localhost", []uint32{testutils.ParsePort(t, serverB.Address)}))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the second state update from the intercepting resolver.
	// This should contain both cluster-A and cluster-B, since the blocked
	// RPC holds a reference to cluster-A.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for second resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterA, clusterB))
	}

	// Unblock the filter's NewStream.
	close(blockChan)

	// Verify the RPC returns the expected blocking filter error.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for RPC to fail")
	case err := <-rpcErrCh:
		if status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("RPC failed with error %v, want code %v with desc containing %q", err, codes.Unavailable, wantErr)
		}
	}

	// Once the stream fails early, the resolver should unsubscribe from
	// cluster A and trigger a service config update that deletes the old
	// cluster. Read the third state update and ensure cluster-A is pruned.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for third resolver state update")
	case js := <-jsonCh:
		compareJSONConfigs(t, js, wantServiceConfig(clusterB))
	}
}
