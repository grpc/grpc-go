/*
 *
 * Copyright 2025 gRPC authors.
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

package resolver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	rinternal "google.golang.org/grpc/internal/xds/resolver/internal"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds" // Register all required xDS components
)

const (
	filterCfgPathFieldName  = "path"
	filterCfgErrorFieldName = "new_stream_error"
	filterCfgMetadataKey    = "test-filter-config"
)

// testFilterCfg is the internal representation of the filter config proto. It
// is returned by filter's config parsing methods.
type testFilterCfg struct {
	httpfilter.FilterConfig
	path         string
	newStreamErr string
}

// filterConfigFromProto parses filter config specified as a v3.TypedStruct into
// a testFilterCfg.
func filterConfigFromProto(cfg proto.Message) (httpfilter.FilterConfig, error) {
	ts, ok := cfg.(*v3xdsxdstypepb.TypedStruct)
	if !ok {
		return nil, fmt.Errorf("unsupported filter config type: %T, want %T", cfg, &v3xdsxdstypepb.TypedStruct{})
	}

	if ts.GetValue() == nil {
		return testFilterCfg{}, nil
	}
	ret := testFilterCfg{}
	if v := ts.GetValue().GetFields()[filterCfgPathFieldName]; v != nil {
		ret.path = v.GetStringValue()
	}
	if v := ts.GetValue().GetFields()[filterCfgErrorFieldName]; v != nil {
		ret.newStreamErr = v.GetStringValue()
	}
	return ret, nil
}

type logger interface {
	Logf(format string, args ...any)
}

// testHTTPFilterWithRPCMetadata is a HTTP filter used for testing purposes.
//
// This filter is used to verify that the xDS resolver and filter stack
// correctly propagate filter configuration (both base and override) to RPCs. It
// does this by injecting the config paths from its base and override configs as
// JSON-encoded metadata into outgoing RPCs.  The metadata can then be observed
// by the backend, allowing tests to assert that the correct filter
// configuration was applied for each RPC.
type testHTTPFilterWithRPCMetadata struct {
	logger        logger
	typeURL       string
	newStreamChan *testutils.Channel // If set, filter config is written to this field from NewStream()
}

func (fb *testHTTPFilterWithRPCMetadata) TypeURLs() []string { return []string{fb.typeURL} }

func (*testHTTPFilterWithRPCMetadata) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfigFromProto(cfg)
}

func (*testHTTPFilterWithRPCMetadata) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfigFromProto(override)
}

func (*testHTTPFilterWithRPCMetadata) IsTerminal() bool { return false }

// ClientInterceptorBuilder is an optional interface for filters to implement.
// This compile time check ensures the test filter implements it.
var _ httpfilter.ClientInterceptorBuilder = &testHTTPFilterWithRPCMetadata{}

func (fb *testHTTPFilterWithRPCMetadata) BuildClientInterceptor(config, override httpfilter.FilterConfig) (iresolver.ClientInterceptor, error) {
	fb.logger.Logf("BuildClientInterceptor called with config: %+v, override: %+v", config, override)

	if config == nil {
		return nil, fmt.Errorf("unexpected missing config")
	}

	baseCfg := config.(testFilterCfg)
	basePath := baseCfg.path
	newStreamErr := baseCfg.newStreamErr

	var overridePath string
	if override != nil {
		overrideCfg := override.(testFilterCfg)
		overridePath = overrideCfg.path
		if overrideCfg.newStreamErr != "" {
			newStreamErr = overrideCfg.newStreamErr
		}
	}

	return &testFilterInterceptor{
		logger: fb.logger,
		cfg: overallFilterConfig{
			BasePath:     basePath,
			OverridePath: overridePath,
			Error:        newStreamErr,
		},
		newStreamChan: fb.newStreamChan,
	}, nil
}

// overallFilterConfig is a JSON representation of the filter config.
// It is sent as RPC metadata and written to a channel for test verification.
type overallFilterConfig struct {
	BasePath     string `json:"base_path,omitempty"`
	OverridePath string `json:"override_path,omitempty"`
	Error        string `json:"error,omitempty"`
}

// testFilterInterceptor is a client interceptor that injects RPC metadata
// corresponding to its filter config.
type testFilterInterceptor struct {
	logger        logger
	cfg           overallFilterConfig
	newStreamChan *testutils.Channel // If set, filter config is written to this field from NewStream()
}

func (fi *testFilterInterceptor) NewStream(ctx context.Context, _ iresolver.RPCInfo, done func(), newStream func(ctx context.Context, done func()) (iresolver.ClientStream, error)) (iresolver.ClientStream, error) {
	// Write the config to the channel, if set. This allows tests to verify that
	// the filter was invoked at RPC time. This is useful for tests where the
	// RPC is expected to fail, and therefore the RPC metadata cannot be
	// observed from the backend.
	if fi.newStreamChan != nil {
		fi.newStreamChan.Send(fi.cfg)
	}

	if fi.cfg.Error != "" {
		return nil, status.Error(codes.Unavailable, fi.cfg.Error)
	}

	// Marshal the filter config to JSON and inject it as metadata.
	bytes, err := json.Marshal(fi.cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filter config: %w", err)
	}
	cfg := string(bytes)
	fi.logger.Logf("Injecting filter config metadata: %v", cfg)

	return newStream(metadata.AppendToOutgoingContext(ctx, filterCfgMetadataKey, fmt.Sprintf("%v", cfg)), done)
}

func newHTTPFilter(t *testing.T, name, typeURL, path, err string) *v3httppb.HttpFilter {
	return &v3httppb.HttpFilter{
		Name: name,
		ConfigType: &v3httppb.HttpFilter_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
				TypeUrl: typeURL,
				Value: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						filterCfgPathFieldName:  {Kind: &structpb.Value_StringValue{StringValue: path}},
						filterCfgErrorFieldName: {Kind: &structpb.Value_StringValue{StringValue: err}},
					},
				},
			}),
		},
	}
}

// newStubServer returns a stub server that sends any filter config metadata
// received as part of incoming RPCs to the provided channel.
func newStubServer(metadataCh chan []string) *stubserver.StubServer {
	return &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "missing metadata")
			}
			select {
			case metadataCh <- md.Get(filterCfgMetadataKey):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &testpb.Empty{}, nil
		},
		UnaryCallF: func(ctx context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "missing metadata")
			}
			select {
			case metadataCh <- md.Get(filterCfgMetadataKey):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &testpb.SimpleResponse{Payload: req.GetPayload()}, nil
		},
	}
}

// Tests HTTP filters with the xDS resolver. The test exercises various levels
// of filter config overrides (base, virtual host-level, route-level and
// cluster-level), and verifies that the correct config is applied for each RPC.
func (s) TestXDSResolverHTTPFilters_AllOverrides(t *testing.T) {
	// Override default WRR with a deterministic test version.
	origNewWRR := rinternal.NewWRR
	rinternal.NewWRR = testutils.NewTestWRR
	defer func() { rinternal.NewWRR = origNewWRR }()

	// Register a custom httpFilter builder for the test.
	testFilterName := t.Name()
	fb := &testHTTPFilterWithRPCMetadata{logger: t, typeURL: testFilterName}
	httpfilter.Register(fb)
	defer httpfilter.UnregisterForTesting(fb.typeURL)

	// Spin up an xDS management server
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a couple of test backends.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	const chBufSize = 4 // Expecting 4 metadata entries (2 RPCs, each with 2 filters).
	metadataCh := make(chan []string, chBufSize)
	backend1 := stubserver.StartTestService(t, newStubServer(metadataCh))
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, newStubServer(metadataCh))
	defer backend2.Stop()

	// Configure resources on the management server.
	//
	// The route configuration contains two routes, matching two different RPCs.
	// The route for the UnaryCall RPC does not contain any cluster-level or
	// route-level per-filter config overrides. A virtual host-level per-filter
	// config override exists and it should apply for RPCs matching this route.
	//
	// The route for the EmptyCall RPC contains a route-level per-filter config
	// override that should apply for RPCs routed to cluster "A" since it does
	// not have any cluster-level overrides. For RPCs matching cluster "B"
	// though, a cluster-level per-filter config override should take
	// precedence.
	const testServiceName = "service-name"
	const routeConfigName = "route-config"
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeConfigName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{
								{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"},
									},
									Action: &v3routepb.Route_Route{
										Route: &v3routepb.RouteAction{
											ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
												WeightedClusters: &v3routepb.WeightedCluster{
													Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
														{Name: "A", Weight: wrapperspb.UInt32(1)},
														{Name: "B", Weight: wrapperspb.UInt32(1)},
													},
												},
											},
										},
									},
								},
								{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
									},
									Action: &v3routepb.Route_Route{
										Route: &v3routepb.RouteAction{
											ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
												WeightedClusters: &v3routepb.WeightedCluster{
													Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
														{
															Name:   "A",
															Weight: wrapperspb.UInt32(1),
														},
														{
															Name:   "B",
															Weight: wrapperspb.UInt32(1),
															TypedPerFilterConfig: map[string]*anypb.Any{
																"foo": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
																	TypeUrl: testFilterName,
																	Value: &structpb.Struct{
																		Fields: map[string]*structpb.Value{
																			filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "foo4"}},
																		},
																	},
																}),
																"bar": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
																	TypeUrl: testFilterName,
																	Value: &structpb.Struct{
																		Fields: map[string]*structpb.Value{
																			filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "bar4"}},
																		},
																	},
																}),
															},
														},
													},
												},
											},
										},
									},
									TypedPerFilterConfig: map[string]*anypb.Any{
										"foo": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
											TypeUrl: testFilterName,
											Value: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "foo3"}},
												},
											},
										}),
										"bar": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
											TypeUrl: testFilterName,
											Value: &structpb.Struct{
												Fields: map[string]*structpb.Value{
													filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "bar3"}},
												},
											},
										}),
									},
								},
							},
							TypedPerFilterConfig: map[string]*anypb.Any{
								"foo": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
									TypeUrl: testFilterName,
									Value: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "foo2"}},
										},
									},
								}),
								"bar": testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
									TypeUrl: testFilterName,
									Value: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											filterCfgPathFieldName: {Kind: &structpb.Value_StringValue{StringValue: "bar2"}},
										},
									},
								}),
							},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "foo", testFilterName, "foo1", ""),
					newHTTPFilter(t, "bar", testFilterName, "bar1", ""),
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelNone),
			e2e.DefaultCluster("B", "endpoint_B", e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint("endpoint_A", "localhost", []uint32{testutils.ParsePort(t, backend1.Address)}),
			e2e.DefaultEndpoint("endpoint_B", "localhost", []uint32{testutils.ParsePort(t, backend2.Address)}),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a gRPC client using the xDS resolver.
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	// Helper to make an RPC twice and collect filter configs from metadata. We
	// make the RPC two times to ensure that we hit both clusters (because of
	// the deterministic WRR). The returned filter configs are in the order in
	// which the RPCs were made.
	collectFilterConfigs := func(rpc func() error) []overallFilterConfig {
		t.Helper()
		var gotFilterCfgs []overallFilterConfig
		for i := 0; i < 2; i++ {
			if err := rpc(); err != nil {
				t.Fatalf("Unexpected RPC error: %v", err)
			}
			select {
			case cfg := <-metadataCh:
				if len(cfg) != 2 {
					t.Fatalf("Unexpected number of filter config metadata, got: %d, want: 2", len(cfg))
				}
				for _, c := range cfg {
					var ofc overallFilterConfig
					if err := json.Unmarshal([]byte(c), &ofc); err != nil {
						t.Fatalf("Failed to unmarshal filter config JSON %q: %v", c, err)
					}
					gotFilterCfgs = append(gotFilterCfgs, ofc)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for metadata from backend")
			}
		}
		return gotFilterCfgs
	}

	// Test base filter config (UnaryCall). Because of the deterministic WRR, we
	// know the expected order of clusters for the two RPCs.
	wantFilterCfgs := []overallFilterConfig{
		{BasePath: "foo1", OverridePath: "foo2"}, // Routed to cluster A
		{BasePath: "bar1", OverridePath: "bar2"}, // Routed to cluster A
		{BasePath: "foo1", OverridePath: "foo2"}, // Routed to cluster B
		{BasePath: "bar1", OverridePath: "bar2"}, // Routed to cluster B
	}
	client := testgrpc.NewTestServiceClient(cc)
	gotFilterCfgs := collectFilterConfigs(func() error {
		_, err := client.UnaryCall(ctx, &testpb.SimpleRequest{})
		return err
	})
	if diff := cmp.Diff(wantFilterCfgs, gotFilterCfgs); diff != "" {
		t.Fatalf("Unexpected filter configs (-want +got):\n%s", diff)
	}

	// Test per-route and per-cluster overrides (EmptyCall).
	wantFilterCfgs = []overallFilterConfig{
		{BasePath: "foo1", OverridePath: "foo3"}, // Routed to cluster A
		{BasePath: "bar1", OverridePath: "bar3"}, // Routed to cluster A
		{BasePath: "foo1", OverridePath: "foo4"}, // Routed to cluster B
		{BasePath: "bar1", OverridePath: "bar4"}, // Routed to cluster B
	}
	gotFilterCfgs = collectFilterConfigs(func() error {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		return err
	})
	if diff := cmp.Diff(wantFilterCfgs, gotFilterCfgs); diff != "" {
		t.Fatalf("Unexpected filter configs (-want +got):\n%s", diff)
	}
}

// Tests that if a filter returns an error from its NewStream method, the RPC
// fails with that error. It also verifies that subsequent filters in the chain
// are not run.
func (s) TestXDSResolverHTTPFilters_NewStreamError(t *testing.T) {
	// Register a custom httpFilter builder for the test and use a channel to
	// get notified when the interceptor is invoked.
	testFilterName := t.Name()
	fb := &testHTTPFilterWithRPCMetadata{
		logger:        t,
		typeURL:       testFilterName,
		newStreamChan: testutils.NewChannelWithSize(3), // We have three filters.
	}
	httpfilter.Register(fb)
	defer httpfilter.UnregisterForTesting(fb.typeURL)

	// Spin up an xDS management server
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a test backend, but we expect the filter to fail the RPC before it
	// ever gets to the backend. The test is designed to fail if the RPC
	// *succeeds* (i.e., if the backend is reached). A large channel buffer is
	// used to prevent blocking in the unexpected case where the filter fails to
	// reject the RPC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	metadataCh := make(chan []string, 10)
	backend := stubserver.StartTestService(t, newStubServer(metadataCh))
	defer backend.Stop()

	// Configure resources on the management server.
	//
	// The route configuration contains two routes, matching two different RPCs.
	// The route for the UnaryCall RPC does not contain any cluster-level or
	// route-level per-filter config overrides. A virtual host-level per-filter
	// config override exists and it should apply for RPCs matching this route.
	//
	// The route for the EmptyCall RPC contains a route-level per-filter config
	// override that should apply for RPCs routed to cluster "A" since it does
	// not have any cluster-level overrides. For RPCs matching cluster "B"
	// though, a cluster-level per-filter config override should take
	// precedence.
	const testServiceName = "service-name"
	const routeConfigName = "route-config"
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeConfigName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{
								{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
									},
									Action: &v3routepb.Route_Route{
										Route: &v3routepb.RouteAction{
											ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
												WeightedClusters: &v3routepb.WeightedCluster{
													Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
														{Name: "A", Weight: wrapperspb.UInt32(1)},
													},
												},
											},
										},
									},
								},
							},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					newHTTPFilter(t, "foo-good", testFilterName, "foo-good", ""),
					newHTTPFilter(t, "foo-failing", testFilterName, "foo-failing", "filter interceptor error"),
					newHTTPFilter(t, "bar-good", testFilterName, "bar-good", ""),
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint("endpoint_A", "localhost", []uint32{testutils.ParsePort(t, backend.Address)})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a gRPC client using the xDS resolver.
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatalf("EmptyCall() RPC succeeded when expected to fail")
	}
	if got, want := status.Code(err), codes.Unavailable; got != want {
		t.Fatalf("EmptyCall() RPC error code, got: %v, want: %v", got, want)
	}
	if got, want := err.Error(), "filter interceptor error"; !strings.Contains(got, want) {
		t.Fatalf("Unexpected RPC error, got: %v, want: %v", err, "rpc error: code = Unavailable desc = filter interceptor error")
	}

	// Verify that the first good filter was invoked
	cfg, err := fb.newStreamChan.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout waiting for first filter to be invoked")
	}
	ofc := cfg.(overallFilterConfig)
	wantCfg := overallFilterConfig{BasePath: "foo-good"}
	if diff := cmp.Diff(wantCfg, ofc); diff != "" {
		t.Fatalf("Unexpected first filter config (-want +got):\n%s", diff)
	}

	// Verify that the failing filter was invoked too.
	cfg, err = fb.newStreamChan.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout waiting for second filter to be invoked")
	}
	ofc = cfg.(overallFilterConfig)
	wantCfg = overallFilterConfig{BasePath: "foo-failing", Error: "filter interceptor error"}
	if diff := cmp.Diff(wantCfg, ofc); diff != "" {
		t.Fatalf("Unexpected second filter config (-want +got):\n%s", diff)
	}

	// Verify that the last good filter was not invoked.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err = fb.newStreamChan.Receive(sCtx); err == nil {
		t.Fatal("Last filter was invoked when expected not to be")
	}
}
