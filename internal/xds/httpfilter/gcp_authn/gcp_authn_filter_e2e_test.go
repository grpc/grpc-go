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
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	anypb "google.golang.org/protobuf/types/known/anypb"
	wrapperspb "google.golang.org/protobuf/types/known/wrapperspb"

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

const (
	gceMetadataHostEnvVar   = "GCE_METADATA_HOST"
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

// setupGCPAuthnTest enables the GCP authn filter and registers the xDS
// metadata converter for parsing the Audience configuration.
func setupGCPAuthnTest(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.GCPAuthenticationFilterEnabled, true)
	audienceTypeURL := "type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.Audience"
	xdsresource.RegisterMetadataConverter(audienceTypeURL, xdsresource.AudienceConverter{})
	t.Cleanup(func() {
		xdsresource.UnregisterMetadataConverterForTesting(audienceTypeURL)
	})
}

// TestGCPAuthnFilter_SuccessCase verifies the basic end-to-end flow. It
// ensures that the gcp_authn filter successfully fetches a token from the mock
// metadata server and attaches it to the outgoing gRPC request metadata.
func (s) TestGCPAuthnFilter_SuccessCase(t *testing.T) {
	setupGCPAuthnTest(t)
	tokenValue := "token"

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
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
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.NoClientCert)
	stubserver.StartTestService(t, backend, grpc.Creds(serverCreds))
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		clusterName     = "cluster_A"
		endpointName    = "endpoint_A"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
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
				Url: "https://example.com",
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
	clientCreds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create client credentials: %v", err)
	}
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to create a gRPC client: %v", err)
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
			t.Errorf("expected token not found in metadata: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for metadata from backend")
	}
}

// TestGCPAuthnFilter_TokenCaching verifies that the filter correctly caches
// tokens and reuses them for subsequent RPCs, avoiding redundant network
// calls to the metadata server.
func (s) TestGCPAuthnFilter_TokenCaching(t *testing.T) {
	setupGCPAuthnTest(t)
	tokenValue := "token"

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
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
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
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.NoClientCert)
	stubserver.StartTestService(t, backend, grpc.Creds(serverCreds))
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		clusterName     = "cluster_A"
		endpointName    = "endpoint_A"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
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
				Url: "https://example.com",
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
	clientCreds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create client credentials: %v", err)
	}
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to create a gRPC client: %v", err)
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
					t.Errorf("expected exactly 1 request to metadata server, got %d", count)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected token not found in metadata: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for metadata from backend")
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
		w.Write([]byte("token"))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a test backend.
	backend := &stubserver.StubServer{}
	stubserver.StartTestService(t, backend)
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
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
				Url: "https://example.com",
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
		t.Fatalf("failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	const wantErr = "cannot send secure credentials on an insecure connection"
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("EmptyCall() failed with error: %v; want error containing: %v", err, wantErr)
	}
}

// TestGCPAuthnFilter_CacheSharingConfigUpdate verifies that the credential
// cache of the gcp_authn filter correctly handles cache resizing across xDS
// updates. It performs RPC calls to 3 different clusters to fill a cache of
// size 3. Then it updates the xDS configuration to reduce the cache size to 1.
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
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
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
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.NoClientCert)
	stubserver.StartTestService(t, backend, grpc.Creds(serverCreds))
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
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
	clientCreds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create client credentials: %v", err)
	}
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	makeCall := func(ctx context.Context, rpcName string) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall(%s) failed: %v", rpcName, err)
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
		t.Errorf("expected 3 requests to metadata server, got %d", count)
	}

	// Update cache config to size 1!
	hcm := &v3httppb.HttpConnectionManager{}
	if err := anypb.UnmarshalTo(listener.ApiListener.ApiListener, hcm, proto.UnmarshalOptions{}); err != nil {
		t.Fatalf("failed to unmarshal HCM: %v", err)
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
			t.Fatal("timeout waiting for xDS update to propagate")
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
		t.Errorf("expected exactly 5 requests to metadata server, got %d", count)
	}
}

// TestGCPAuthnFilter_ConcurrentRPCWithShortAndLongContext verifies the
// scenario where two RPCs are made: the first with a short context which
// triggers a token fetch but times out (exceeding deadline) while waiting
// for the metadata server to respond; and the second with a long context,
// which blocks waiting for the first token fetch to finish, and then succeeds
// by using the token fetched by the first.
func (s) TestGCPAuthnFilter_ConcurrentRPCWithShortAndLongContext(t *testing.T) {
	setupGCPAuthnTest(t)
	var once sync.Once
	tokenValue := "token"
	requestStarted := make(chan struct{})
	proceedCh := make(chan struct{})

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	var requestCount int32
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Close requestStarted channel to signal that a request has started.
		once.Do(func() {
			close(requestStarted)
		})
		atomic.AddInt32(&requestCount, 1)
		// Block until signaled to proceed.
		<-proceedCh
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
	}

	// Start a test backend.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	metadataCh := make(chan []string, 1)

	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			metadataCh <- md.Get("authorization")
			return &testpb.Empty{}, nil
		},
	}
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.NoClientCert)
	stubserver.StartTestService(t, backend, grpc.Creds(serverCreds))
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		clusterName     = "cluster_A"
		endpointName    = "endpoint_A"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
						VirtualHosts: []*v3routepb.VirtualHost{{
							Domains: []string{testServiceName},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{
									PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""},
								},
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
				Url: "https://example.com",
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
	clientCreds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create client credentials: %v", err)
	}
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Create a short context for the first RPC call.
	shortCtx, shortBufCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortBufCancel()

	errCh1 := make(chan error, 1)
	errCh2 := make(chan error, 1)

	// First RPC call with short context (triggers token fetch request to metadata server).
	go func() {
		_, err := client.EmptyCall(shortCtx, &testpb.Empty{})
		errCh1 <- err
	}()

	// Wait for the first RPC to hit the local metadata server.
	select {
	case <-requestStarted:
	case <-ctx.Done():
		t.Fatal("timeout while waiting for token fetch to start in metadata server")
	}

	// Second RPC call with long context.
	go func() {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		errCh2 <- err
	}()

	// Wait for the duration of first RPC's short context timeout (100ms).
	// Because the token fetch is synchronously blocked in the metadata server
	// during GetRequestMetadata, the first RPC must remain blocked and not have
	// completed or failed yet.
	select {
	case err := <-errCh1:
		t.Fatalf("first RPC completed early with error: %v, expected it to remain blocked", err)
	case <-time.After(100 * time.Millisecond):
		// Success: RPC 1 remained blocked.
	}

	// Now allow the metadata server to complete and return token.
	close(proceedCh)

	// Verify that the first RPC now fails with context deadline exceeded.
	select {
	case err := <-errCh1:
		if err == nil || status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("first RPC failed with error code %v, want DeadlineExceeded", status.Code(err))
		}
	case <-ctx.Done():
		t.Fatal("timeout while waiting for first RPC to fail")
	}

	// Verify that the second RPC successfully completes.
	select {
	case err := <-errCh2:
		if err != nil {
			t.Fatalf("second RPC failed unexpectedly with: %v, want success", err)
		}
	case <-ctx.Done():
		t.Fatal("timeout while waiting for second RPC to succeed")
	}

	// Verify that the token retrieved by the first fetch was successfully
	// attached to the second RPC.
	select {
	case md := <-metadataCh:
		found := false
		for _, val := range md {
			if strings.Contains(val, "Bearer "+tokenValue) {
				// Verify request count is 1. This ensures that the token fetched for
				// the first RPC was reused for the second RPC.
				if count := atomic.LoadInt32(&requestCount); count != 1 {
					t.Errorf("expected exactly 1 request to metadata server, got %d", count)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected token not found in metadata of second RPC: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for metadata from backend for the second RPC")
	}
}

// TestGCPAuthnFilter_PreservesUserCallOptions verifies that when an RPC is
// made with user-specified call options (e.g., grpc.Header), the filter
// successfully appends its PerRPCCredentials option without overriding,
// corrupting or interfering with the existing ones.
func (s) TestGCPAuthnFilter_PreservesUserCallOptions(t *testing.T) {
	setupGCPAuthnTest(t)
	tokenValue := "token"

	// Starts a local HTTP server and sets GCE_METADATA_HOST to spoof the
	// GCE metadata server and redirect token fetch requests to it.
	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(tokenValue))
	}))
	defer metadataServer.Close()
	t.Setenv(gceMetadataHostEnvVar, strings.TrimPrefix(metadataServer.URL, "http://"))

	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	defer mgmtServer.Stop()

	// Create an xDS resolver with bootstrap configuration pointing to the above
	// management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("failed to create xDS resolver for testing: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	metadataCh := make(chan []string, 1)

	// Start a test backend.
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			// Set a custom header back to the client to verify the original CallOption works.
			if err := grpc.SetHeader(ctx, metadata.Pairs("custom-response-header", "custom-val")); err != nil {
				t.Errorf("failed to set response header: %v", err)
			}

			md, _ := metadata.FromIncomingContext(ctx)
			metadataCh <- md.Get("authorization")
			return &testpb.Empty{}, nil
		},
	}
	serverCreds := testutils.CreateServerTLSCredentials(t, tls.NoClientCert)
	stubserver.StartTestService(t, backend, grpc.Creds(serverCreds))
	defer backend.Stop()

	var (
		testServiceName = "service-name"
		clusterName     = "cluster_A"
		endpointName    = "endpoint_A"
		filterName      = "com.google.grpc.gcp_authn"
	)

	// Configure resources on the management server.
	listener := &v3listenerpb.Listener{
		Name: testServiceName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
					RouteConfig: &v3routepb.RouteConfiguration{
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
				Url: "https://example.com",
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
	clientCreds, err := xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
	if err != nil {
		t.Fatalf("failed to create client credentials: %v", err)
	}
	cc, err := grpc.NewClient("xds:///"+testServiceName, grpc.WithTransportCredentials(clientCreds), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to create a gRPC client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	var header metadata.MD

	// RPC call with a custom CallOption.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.Header(&header)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the response header option was executed successfully, proving the first entry was preserved.
	val := header.Get("custom-response-header")
	if len(val) == 0 || val[0] != "custom-val" {
		t.Errorf("expected response header with value: custom-val, got: %v", val)
	}

	// Verify that the credentials option was successfully appended and executed.
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
			t.Errorf("expected token not found in metadata: %v", md)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for metadata from backend")
	}
}
