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

package gcpauthn_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	// gcpauthn "google.golang.org/grpc/internal/xds/httpfilter/gcp_authn"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
	wrapperspb "google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/internal/xds/httpfilter/router" // Register router filter
	_ "google.golang.org/grpc/internal/xds/resolver"          // Register xDS resolver
	_ "google.golang.org/grpc/xds"                            // Register all xDS components
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var (
	audienceTypeURL       = "type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.Audience"
	tokenValue            = "token"
	gceMetadataHostEnvVar = "GCE_METADATA_HOST"
	testServiceName       = "service-name"
	routeConfigName       = "route-config"
	clusterName           = "cluster_A"
	endpointName          = "endpoint_A"
	filterName            = "com.google.grpc.gcp_authn"
	url                   = "https://example.com"
)

func setupGCPAuthnTest(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.GCPAuthenticationFilterEnabled, true)
	xdsresource.RegisterMetadataConverter(audienceTypeURL, xdsresource.AudienceConverter{})
	t.Cleanup(func() {
		xdsresource.UnregisterMetadataConverterForTesting(audienceTypeURL)
	})
}

// TestGCPAuthnFilter_SuccessCase verifies the basic end-to-end flow. It ensures
// that the gcp_authn filter successfully fetches a token from the mock
// metadata server and attaches it to the outgoing gRPC request metadata.
func (s) TestGCPAuthnFilter_SuccessCase(t *testing.T) {
	setupGCPAuthnTest(t)

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a test backend.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	metadataCh := make(chan []string, 1)

	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			metadataCh <- md.Get("authorization")
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeConfigName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
								Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
									ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
								}},
							}},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: filterName,
						ConfigType: &v3httppb.HttpFilter_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
								CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
									CacheSize: &wrapperspb.UInt64Value{Value: 10},
								},
							}),
						},
					},
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}

	cluster := e2e.DefaultCluster(clusterName, endpointName, e2e.SecurityLevelTLS)
	cluster.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{
				Url: url,
			}),
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Clusters:       []*v3clusterpb.Cluster{cluster},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointName, "localhost", []uint32{testutils.ParsePort(t, backend.Address)})},
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
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the token was attached to the RPC
	select {
	case md := <-metadataCh:
		found := false
		for _, val := range md {
			if strings.Contains(val, "Bearer "+tokenValue) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected token not found in metadata: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for metadata from backend")
	}
}

// TestGCPAuthnFilter_TokenCaching verifies that the filter correctly caches
// tokens and reuses them for subsequent RPCs, avoiding redundant network
// calls to the metadata server.
func (s) TestGCPAuthnFilter_TokenCaching(t *testing.T) {
	setupGCPAuthnTest(t)

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	var requestCount int32
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	metadataCh := make(chan []string, 10)

	// Start a test backend.
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			metadataCh <- md.Get("authorization")
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeConfigName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
								Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
									ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
								}},
							}},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: filterName,
						ConfigType: &v3httppb.HttpFilter_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
								CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
									CacheSize: &wrapperspb.UInt64Value{Value: 10},
								},
							}),
						},
					},
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}

	cluster := e2e.DefaultCluster(clusterName, endpointName, e2e.SecurityLevelTLS)
	cluster.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{
				Url: url,
			}),
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Clusters:       []*v3clusterpb.Cluster{cluster},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointName, "localhost", []uint32{testutils.ParsePort(t, backend.Address)})},
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

	const numCalls = 3
	for i := 0; i < numCalls; i++ {
		if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
	}

	// Verify that the token was attached to the RPC
	select {
	case md := <-metadataCh:
		found := false
		for _, val := range md {
			if strings.Contains(val, "Bearer "+tokenValue) {
				// Verify request count is 1
				if count := atomic.LoadInt32(&requestCount); count != 1 {
					t.Errorf("Expected exactly 1 request to metadata server, got %d", count)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected token not found in metadata: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for metadata from backend")
	}
}

// TestGCPAuthnFilter_InsecureTransport verifies that the filter refuses to
// attach credentials when the target cluster is configured without transport
// security.
func (s) TestGCPAuthnFilter_InsecureTransport(t *testing.T) {
	setupGCPAuthnTest(t)

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a test backend.
	backend := &stubserver.StubServer{}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						Name: routeConfigName,
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
								Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
									ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: "A"},
								}},
							}},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: filterName,
						ConfigType: &v3httppb.HttpFilter_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
								CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
									CacheSize: &wrapperspb.UInt64Value{Value: 10},
								},
							}),
						},
					},
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}

	// SecurityLevelNone causes SecurityCfg to be nil
	cluster := e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelNone)
	cluster.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{
				Url: url,
			}),
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Clusters:       []*v3clusterpb.Cluster{cluster},
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
		t.Fatalf("EmptyCall() succeeded when insecure transport was used, want error")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("EmptyCall() failed with code %v, want Unauthenticated", status.Code(err))
	}
	if !strings.Contains(err.Error(), "cannot send secure credentials on an insecure connection") {
		t.Errorf("EmptyCall() failed with error message %q, want it to contain %q", err.Error(), "cannot send secure credentials on an insecure connection")
	}
}

// TestGCPAuthnFilter_CacheSharingConfigUpdate verifies that the credential cache of
// the gcp_authn filter correctly handles cache resizing across xDS updates.
// It performs RPC calls to 3 different clusters to fill a cache of size 3.
// Then it updates the xDS configuration to reduce the cache size to 1.
// Finally, it verifies that only the Most Recently Used element survives
// in the cache, and making calls to the other two results in cache misses
// (forcing a new token fetch).
func (s) TestGCPAuthnFilter_CacheSharingConfigUpdate(t *testing.T) {
	setupGCPAuthnTest(t)

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	var requestCount int32
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("token-%d", count)))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	metadataCh := make(chan []string, 10)

	// Start a test backend.
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			metadataCh <- md.Get("authorization")
			return &testpb.Empty{}, nil
		},
	}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	// Configure resources on the management server.
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
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""},
										Headers: []*v3routepb.HeaderMatcher{{
											Name:                 "match-id",
											HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "a"},
										}},
									},
									Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: "A"},
									}},
								},
								{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""},
										Headers: []*v3routepb.HeaderMatcher{{
											Name:                 "match-id",
											HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "b"},
										}},
									},
									Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: "B"},
									}},
								},
								{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""},
										Headers: []*v3routepb.HeaderMatcher{{
											Name:                 "match-id",
											HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "c"},
										}},
									},
									Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: "C"},
									}},
								},
							},
						}},
					},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: filterName,
						ConfigType: &v3httppb.HttpFilter_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
								CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
									CacheSize: &wrapperspb.UInt64Value{Value: 10},
								},
							}),
						},
					},
					e2e.RouterHTTPFilter,
				},
			}),
		},
	}

	clusterA := e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelTLS)
	clusterA.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{Url: "url-1"}),
		},
	}

	clusterB := e2e.DefaultCluster("B", "endpoint_B", e2e.SecurityLevelTLS)
	clusterB.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{Url: "url-2"}),
		},
	}

	clusterC := e2e.DefaultCluster("C", "endpoint_C", e2e.SecurityLevelTLS)
	clusterC.Metadata = &v3corepb.Metadata{
		TypedFilterMetadata: map[string]*anypb.Any{
			filterName: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{Url: "url-3"}),
		},
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
		Clusters:  []*v3clusterpb.Cluster{clusterA, clusterB, clusterC},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint("endpoint_A", "localhost", []uint32{testutils.ParsePort(t, backend.Address)}),
			e2e.DefaultEndpoint("endpoint_B", "localhost", []uint32{testutils.ParsePort(t, backend.Address)}),
			e2e.DefaultEndpoint("endpoint_C", "localhost", []uint32{testutils.ParsePort(t, backend.Address)}),
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

	client := testgrpc.NewTestServiceClient(cc)
	makeCall := func(ctx context.Context, rpcName string) {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err != nil {
			t.Fatalf("RPC %s failed: %v", rpcName, err)
		}
		<-metadataCh
	}

	// Make initial calls to all 3 clusters to populate the cache with 3 entries.
	// The access order is A, then B, then C (leaving C as the most recently used).
	ctxA := metadata.AppendToOutgoingContext(ctx, "match-id", "a")
	ctxB := metadata.AppendToOutgoingContext(ctx, "match-id", "b")
	ctxC := metadata.AppendToOutgoingContext(ctx, "match-id", "c")

	makeCall(ctxA, "A")
	makeCall(ctxB, "B")
	makeCall(ctxC, "C")

	// Verify request count is 3!
	if count := atomic.LoadInt32(&requestCount); count != 3 {
		t.Errorf("Expected 3 requests to metadata server, got %d", count)
	}

	// Update cache config to size 1!
	hcm := &v3httppb.HttpConnectionManager{}
	if err := anypb.UnmarshalTo(listener.ApiListener.ApiListener, hcm, proto.UnmarshalOptions{}); err != nil {
		t.Fatalf("Failed to unmarshal HCM: %v", err)
	}
	hcm.HttpFilters[0].ConfigType = &v3httppb.HttpFilter_TypedConfig{
		TypedConfig: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
			CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
				CacheSize: &wrapperspb.UInt64Value{Value: 1},
			},
		}),
	}

	// Add a new route matching "match-id": "d" pointing to Cluster D as a canary.
	routeD := &v3routepb.Route{
		Match: &v3routepb.RouteMatch{
			PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""},
			Headers: []*v3routepb.HeaderMatcher{{
				Name:                 "match-id",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "d"},
			}},
		},
		Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
			ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: "D"},
		}},
	}
	hcm.GetRouteConfig().GetVirtualHosts()[0].Routes = append(hcm.GetRouteConfig().GetVirtualHosts()[0].Routes, routeD)
	resources.Listeners[0].ApiListener.ApiListener = testutils.MarshalAny(t, hcm)
	resources.Clusters = append(resources.Clusters, e2e.DefaultCluster("D", "endpoint_D", e2e.SecurityLevelTLS))
	resources.Endpoints = append(resources.Endpoints, e2e.DefaultEndpoint("endpoint_D", "localhost", []uint32{testutils.ParsePort(t, backend.Address)}))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Active Probing: Wait for the new route "d" to become live. Because it points
	// to Cluster D with no metadata, a successful call here won't trigger
	// a new fetch. This guarantees that the resource update is propagated.
	ctxD := metadata.AppendToOutgoingContext(ctx, "match-id", "d")
	for {
		if _, err = client.EmptyCall(ctxD, &testpb.Empty{}); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("Timeout waiting for xDS update to propagate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-metadataCh

	// Make calls for all 3 clusters again. Resizing the cache from 3 to 1 in the
	// previous step leaves only the token for Cluster C in the cache (as it was
	// the most recently used). Therefore, calls to clusters A and B will miss
	// the cache and trigger new metadata server fetches, while C will hit.
	makeCall(ctxC, "C on second pass")
	makeCall(ctxB, "B on second pass")
	makeCall(ctxA, "A on second pass")

	// Verify that the total requests to the metadata server is exactly 5
	// This proves that token for cluster C was successfully cached in the
	// second pass, while A and B were not.
	if count := atomic.LoadInt32(&requestCount); count != 5 {
		t.Errorf("Expected exactly 5 requests to metadata server, got %d", count)
	}
}
