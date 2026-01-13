/*
 *
 * Copyright 2019 gRPC authors.
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
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	iresolver "google.golang.org/grpc/internal/resolver"
	iringhash "google.golang.org/grpc/internal/ringhash"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/balancer/clusterimpl"
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/internal/xds/bootstrap"
	serverFeature "google.golang.org/grpc/internal/xds/clients/xdsclient"
	rinternal "google.golang.org/grpc/internal/xds/resolver/internal"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	_ "google.golang.org/grpc/internal/xds/balancer/cdsbalancer" // Register the cds LB policy
	_ "google.golang.org/grpc/internal/xds/httpfilter/router"    // Register the router filter
)

// Tests the case where xDS client creation is expected to fail because the
// bootstrap configuration for the xDS client pool is not specified. The test
// verifies that xDS resolver build fails as well.
func (s) TestResolverBuilder_ClientCreationFails_NoBootstrap(t *testing.T) {
	// Build an xDS resolver specifying nil for bootstrap configuration for the
	// xDS client pool.
	pool := xdsclient.NewPool(nil)
	var xdsResolver resolver.Builder
	if newResolver := internal.NewXDSResolverWithPoolForTesting; newResolver != nil {
		var err error
		xdsResolver, err = newResolver.(func(*xdsclient.Pool) (resolver.Builder, error))(pool)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
	}

	target := resolver.Target{URL: *testutils.MustParseURL("xds:///target")}
	if _, err := xdsResolver.Build(target, nil, resolver.BuildOptions{}); err == nil {
		t.Fatalf("xds Resolver Build(%v) succeeded when expected to fail, because there is no bootstrap configuration for the xDS client pool", pool)
	}
}

// Tests the case where the specified dial target contains an authority that is
// not specified in the bootstrap file. Verifies that the resolver.Build method
// fails with the expected error string.
func (s) TestResolverBuilder_AuthorityNotDefinedInBootstrap(t *testing.T) {
	contents := e2e.DefaultBootstrapContents(t, "node-id", "dummy-management-server")

	// Create an xDS resolver with the above bootstrap configuration.
	xdsResolver, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(contents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	target := resolver.Target{URL: *testutils.MustParseURL("xds://non-existing-authority/target")}
	const wantErr = `authority "non-existing-authority" specified in dial target "xds://non-existing-authority/target" is not found in the bootstrap file`
	r, err := xdsResolver.Build(target, &testutils.ResolverClientConn{Logger: t}, resolver.BuildOptions{})
	if r != nil {
		r.Close()
	}
	if err == nil {
		t.Fatalf("xds Resolver Build(%v) succeeded for target with authority not specified in bootstrap", target)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("xds Resolver Build(%v) returned err: %v, wantErr: %v", target, err, wantErr)
	}
}

// Test builds an xDS resolver and verifies that the resource name specified in
// the discovery request matches expectations.
func (s) TestResolverResourceName(t *testing.T) {
	tests := []struct {
		name                         string
		listenerResourceNameTemplate string
		extraAuthority               string
		dialTarget                   string
		wantResourceNames            []string
	}{
		{
			name:                         "default %s old style",
			listenerResourceNameTemplate: "%s",
			dialTarget:                   "xds:///target",
			wantResourceNames:            []string{"target"},
		},
		{
			name:                         "old style no percent encoding",
			listenerResourceNameTemplate: "/path/to/%s",
			dialTarget:                   "xds:///target",
			wantResourceNames:            []string{"/path/to/target"},
		},
		{
			name:                         "new style with %s",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			dialTarget:                   "xds:///0.0.0.0:8080",
			wantResourceNames:            []string{"xdstp://authority.com/0.0.0.0:8080"},
		},
		{
			name:                         "new style percent encoding",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			dialTarget:                   "xds:///[::1]:8080",
			wantResourceNames:            []string{"xdstp://authority.com/%5B::1%5D:8080"},
		},
		{
			name:                         "new style different authority",
			listenerResourceNameTemplate: "xdstp://authority.com/%s",
			extraAuthority:               "test-authority",
			dialTarget:                   "xds://test-authority/target",
			wantResourceNames:            []string{"xdstp://test-authority/envoy.config.listener.v3.Listener/target"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Spin up an xDS management server for the test.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			nodeID := uuid.New().String()
			mgmtServer, lisCh, _, _ := setupManagementServerForTest(t, nodeID)

			// Create a bootstrap configuration with test options.
			opts := bootstrap.ConfigOptionsForTesting{
				Servers: []byte(fmt.Sprintf(`[{
					"server_uri": %q,
					"channel_creds": [{"type": "insecure"}]
				}]`, mgmtServer.Address)),
				ClientDefaultListenerResourceNameTemplate: tt.listenerResourceNameTemplate,
				Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
			}
			if tt.extraAuthority != "" {
				// In this test, we really don't care about having multiple
				// management servers. All we need to verify is whether the
				// resource name matches expectation.
				opts.Authorities = map[string]json.RawMessage{
					tt.extraAuthority: []byte(fmt.Sprintf(`{
						"server_uri": %q,
						"channel_creds": [{"type": "insecure"}]
					}`, mgmtServer.Address)),
				}
			}
			contents, err := bootstrap.NewContentsForTesting(opts)
			if err != nil {
				t.Fatalf("Failed to create bootstrap configuration: %v", err)
			}

			buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL(tt.dialTarget)}, contents)
			waitForResourceNames(ctx, t, lisCh, tt.wantResourceNames)
		})
	}
}

// Tests the case where a service update from the underlying xDS client is
// received after the resolver is closed, and verifies that the update is not
// propagated to the ClientConn.
func (s) TestResolverWatchCallbackAfterClose(t *testing.T) {
	// Setup the management server that synchronizes with the test goroutine
	// using two channels. The management server signals the test goroutine when
	// it receives a discovery request for a route configuration resource. And
	// the test goroutine signals the management server when the resolver is
	// closed.
	routeConfigResourceNamesCh := make(chan []string, 1)
	waitForResolverCloseCh := make(chan struct{})
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3RouteConfigURL {
				select {
				case <-routeConfigResourceNamesCh:
				default:
				}
				select {
				case routeConfigResourceNamesCh <- req.GetResourceNames():
				default:
				}
				<-waitForResolverCloseCh
			}
			return nil
		},
	})

	// Create a bootstrap configuration specifying the above management server.
	nodeID := uuid.New().String()
	contents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	// Configure resources on the management server.
	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)}
	routes := []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, routes)

	// Wait for a discovery request for a route configuration resource.
	stateCh, _, r := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, contents)
	waitForResourceNames(ctx, t, routeConfigResourceNamesCh, []string{defaultTestRouteConfigName})

	// Close the resolver and unblock the management server.
	r.Close()
	close(waitForResolverCloseCh)

	// Verify that the update from the management server is not propagated to
	// the ClientConn. The xDS resolver, once closed, is expected to drop
	// updates from the xDS client.
	verifyNoUpdateFromResolver(ctx, t, stateCh)
}

// Tests that the xDS resolver's Close method closes the xDS client.
func (s) TestResolverCloseClosesXDSClient(t *testing.T) {
	// Override xDS client creation to use bootstrap configuration pointing to a
	// dummy management server. Also close a channel when the returned xDS
	// client is closed.
	origNewClient := rinternal.NewXDSClient
	closeCh := make(chan struct{})
	rinternal.NewXDSClient = func(string, estats.MetricsRecorder) (xdsclient.XDSClient, func(), error) {
		bc := e2e.DefaultBootstrapContents(t, uuid.New().String(), "dummy-management-server-address")
		config, err := bootstrap.NewConfigFromContents(bc)
		if err != nil {
			t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bc), err)
		}
		pool := xdsclient.NewPool(config)
		if err != nil {
			t.Fatalf("Failed to create an xDS client pool: %v", err)
		}
		c, cancel, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
			Name:               t.Name(),
			WatchExpiryTimeout: defaultTestTimeout,
		})
		return c, sync.OnceFunc(func() {
			close(closeCh)
			cancel()
		}), err
	}
	defer func() { rinternal.NewXDSClient = origNewClient }()

	_, _, r := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///my-service-client-side-xds")}, nil)
	r.Close()

	select {
	case <-closeCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for xDS client to be closed")
	}
}

// Tests the case where there is no virtual host in the route configuration
// matches the dataplane authority. Verifies that the resolver returns the
// correct error.
func (s) TestNoMatchingVirtualHost(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure route resource with no virtual host so that it does not match the authority.
	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	route.VirtualHosts = nil
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, []*v3listenerpb.Listener{listener}, []*v3routepb.RouteConfiguration{route})

	// Build the resolver inline (duplicating buildResolverForTarget internals)
	// to avoid issues with blocked channel writes when NACKs occur.
	target := resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}

	// Create an xDS resolver with the provided bootstrap configuration.
	builder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	errCh := testutils.NewChannel()
	tcc := &testutils.ResolverClientConn{Logger: t, ReportErrorF: func(err error) { errCh.Replace(err) }}
	r, err := builder.Build(target, tcc, resolver.BuildOptions{
		Authority: url.PathEscape(target.Endpoint()),
	})
	if err != nil {
		t.Fatalf("Failed to build xDS resolver for target %q: %v", target, err)
	}
	defer r.Close()

	// Wait for and verify the error update from the resolver.
	// Since the resource is not cached, it should be received as resource error.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for error to be propagated to the ClientConn")
	case gotErr := <-errCh.C:
		if gotErr == nil {
			t.Fatalf("got nil error from resolver, want error containing 'could not find VirtualHost'")
		}
		errStr := fmt.Sprint(gotErr)
		if !strings.Contains(errStr, fmt.Sprintf("could not find VirtualHost for %q", defaultTestServiceName)) {
			t.Fatalf("got error from resolver %q, want error containing 'could not find VirtualHost for %q'", errStr, defaultTestServiceName)
		}
		if !strings.Contains(errStr, nodeID) {
			t.Fatalf("got error from resolver %q, want nodeID %q", errStr, nodeID)
		}
	}
}

// Tests the case where a resource, not present in cache, returned by the
// management server is NACKed by the xDS client, which then returns an update
// containing a resource error to the resolver. It tests the case where the
// resolver gets an error update without any previous good update. The test
// also verifies that these are propagated to the ClientConn.
func (s) TestResolverBadServiceUpdate_NACKedWithoutCache(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure a listener resource that is expected to be NACKed because it
	// does not contain the `RouteSpecifier` field in the HTTPConnectionManager.
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	lis := &v3listenerpb.Listener{
		Name:        defaultTestServiceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, []*v3listenerpb.Listener{lis}, nil)

	// Build the resolver inline (duplicating buildResolverForTarget internals)
	// to avoid issues with blocked channel writes when NACKs occur.
	target := resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}

	// Create an xDS resolver with the provided bootstrap configuration.
	builder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	errCh := testutils.NewChannel()
	tcc := &testutils.ResolverClientConn{Logger: t, ReportErrorF: func(err error) { errCh.Replace(err) }}
	r, err := builder.Build(target, tcc, resolver.BuildOptions{
		Authority: url.PathEscape(target.Endpoint()),
	})
	if err != nil {
		t.Fatalf("Failed to build xDS resolver for target %q: %v", target, err)
	}
	defer r.Close()

	// Wait for and verify the error update from the resolver.
	// Since the resource is not cached, it should be received as resource error.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for error to be propagated to the ClientConn")
	case gotErr := <-errCh.C:
		if gotErr == nil {
			t.Fatalf("got nil error from resolver, want error containing 'no RouteSpecifier'")
		}
		errStr := fmt.Sprint(gotErr)
		if !strings.Contains(errStr, "no RouteSpecifier") {
			t.Fatalf("got error from resolver %q, want error containing 'no RouteSpecifier'", errStr)
		}
		if !strings.Contains(errStr, nodeID) {
			t.Fatalf("got error from resolver %q, want nodeID %q", errStr, nodeID)
		}
	}
}

// Tests the case where a resource, present in cache, returned by the management
// server is NACKed by the xDS client, which then returns an update containing
// an ambient error to the resolver. It tests the case where the resolver gets a
// good update first, and an error after the good update. The test verifies that
// the RPCs succeeds as expected after receiving good update as well as ambient
// error.
func (s) TestResolverBadServiceUpdate_NACKedWithCache(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Configure all resources on the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
	})
	mgmtServer.Update(ctx, resources)

	// Expect a good update from the resolver.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	// "Make an RPC" by invoking the config selector.
	_, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	// Configure a listener resource that is expected to be NACKed because it
	// does not contain the `RouteSpecifier` field in the HTTPConnectionManager.
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	lis := &v3listenerpb.Listener{
		Name:        defaultTestServiceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}

	// Since the resource is cached, it should be received as an ambient error
	// and so the RPCs should continue passing.
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, []*v3listenerpb.Listener{lis}, nil)

	// "Make an RPC" by invoking the config selector which should succeed by
	// continuing to use the previously cached resource.
	_, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
}

// TestResolverGoodServiceUpdate tests the case where the resource returned by
// the management server is ACKed by the xDS client, which then returns a good
// service update to the resolver. The test verifies that the service config
// returned by the resolver matches expectations, and that the config selector
// returned by the resolver picks clusters based on the route configuration
// received from the management server.
func (s) TestResolverGoodServiceUpdate(t *testing.T) {
	for _, tt := range []struct {
		name              string
		routeConfig       *v3routepb.RouteConfiguration
		clusterConfig     []*v3clusterpb.Cluster
		endpointConfig    []*v3endpointpb.ClusterLoadAssignment
		wantServiceConfig string
		wantClusters      map[string]bool
	}{
		{
			name: "single cluster",
			routeConfig: e2e.RouteConfigResourceWithOptions(e2e.RouteConfigOptions{
				RouteConfigName:      defaultTestRouteConfigName,
				ListenerName:         defaultTestServiceName,
				ClusterSpecifierType: e2e.RouteConfigClusterSpecifierTypeCluster,
				ClusterName:          defaultTestClusterName,
			}),
			clusterConfig:     []*v3clusterpb.Cluster{e2e.DefaultCluster(defaultTestClusterName, defaultTestEndpointName, e2e.SecurityLevelNone)},
			endpointConfig:    []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(defaultTestEndpointName, defaultTestHostname, defaultTestPort)},
			wantServiceConfig: wantServiceConfig(defaultTestClusterName),
			wantClusters:      map[string]bool{fmt.Sprintf("cluster:%s", defaultTestClusterName): true},
		},
		{
			name: "two clusters",
			routeConfig: e2e.RouteConfigResourceWithOptions(e2e.RouteConfigOptions{
				RouteConfigName:      defaultTestRouteConfigName,
				ListenerName:         defaultTestServiceName,
				ClusterSpecifierType: e2e.RouteConfigClusterSpecifierTypeWeightedCluster,
				WeightedClusters:     map[string]int{"cluster_1": 75, "cluster_2": 25},
			}),
			clusterConfig: []*v3clusterpb.Cluster{
				e2e.DefaultCluster("cluster_1", "endpoint_1", e2e.SecurityLevelNone),
				e2e.DefaultCluster("cluster_2", "endpoint_2", e2e.SecurityLevelNone)},
			endpointConfig: []*v3endpointpb.ClusterLoadAssignment{
				e2e.DefaultEndpoint("endpoint_1", defaultTestHostname, defaultTestPort),
				e2e.DefaultEndpoint("endpoint_2", defaultTestHostname, defaultTestPort)},
			// This update contains the cluster from the previous update as well
			// as this update, as the previous config selector still references
			// the old cluster when the new one is pushed.
			wantServiceConfig: `{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        "cluster:cluster_1": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_1"
			}
		  }]
        },
        "cluster:cluster_2": {
          "childPolicy": [{
			"cds_experimental": {
			  "cluster": "cluster_2"
			}
		  }]
        }
      }
    }
  }]}`,
			wantClusters: map[string]bool{"cluster:cluster_1": true, "cluster:cluster_2": true},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Spin up an xDS management server for the test.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			nodeID := uuid.New().String()
			mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

			// Configure the management server with a good listener resource and a
			// route configuration resource, as specified by the test case.
			listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)}
			routes := []*v3routepb.RouteConfiguration{tt.routeConfig}
			configureAllResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, routes, tt.clusterConfig, tt.endpointConfig)

			stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

			// Read the update pushed by the resolver to the ClientConn.
			cs := verifyUpdateFromResolver(ctx, t, stateCh, tt.wantServiceConfig)

			pickedClusters := make(map[string]bool)
			// Odds of picking 75% cluster 100 times in a row: 1 in 3E-13.  And
			// with the random number generator stubbed out, we can rely on this
			// to be 100% reproducible.
			for i := 0; i < 100; i++ {
				res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
				if err != nil {
					t.Fatalf("cs.SelectConfig(): %v", err)
				}
				cluster := clustermanager.GetPickedClusterForTesting(res.Context)
				pickedClusters[cluster] = true
				res.OnCommitted()
			}
			if !cmp.Equal(pickedClusters, tt.wantClusters) {
				t.Errorf("Picked clusters: %v; want: %v", pickedClusters, tt.wantClusters)
			}
		})
	}
}

// Tests a case where a resolver receives a RouteConfig update with a HashPolicy
// specifying to generate a hash. The configSelector generated should
// successfully generate a Hash.
func (s) TestResolverRequestHash(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure the management server with a good listener resource and a
	// route configuration resource that specifies a hash policy.
	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)}
	routes := []*v3routepb.RouteConfiguration{{
		Name: defaultTestRouteConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{defaultTestServiceName},
			Routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
				Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
					ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
						Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
							{
								Name:   defaultTestClusterName,
								Weight: &wrapperspb.UInt32Value{Value: 100},
							},
						},
					}},
					HashPolicy: []*v3routepb.RouteAction_HashPolicy{{
						PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
							Header: &v3routepb.RouteAction_HashPolicy_Header{
								HeaderName: ":path",
							},
						},
						Terminal: true,
					}},
				}},
			}},
		}},
	}}
	cluster := []*v3clusterpb.Cluster{e2e.DefaultCluster(defaultTestClusterName, defaultTestEndpointName, e2e.SecurityLevelNone)}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(defaultTestEndpointName, defaultTestHostname, defaultTestPort)}
	configureAllResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, routes, cluster, endpoints)

	// Build the resolver and read the config selector out of it.
	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)
	cs := verifyUpdateFromResolver(ctx, t, stateCh, "")

	// Selecting a config when there was a hash policy specified in the route
	// that will be selected should put a request hash in the config's context.
	res, err := cs.SelectConfig(iresolver.RPCInfo{
		Context: metadata.NewOutgoingContext(ctx, metadata.Pairs(":path", "/products")),
		Method:  "/service/method",
	})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	wantHash := xxhash.Sum64String("/products")
	gotHash, ok := iringhash.XDSRequestHash(res.Context)
	if !ok {
		t.Fatalf("Got no request hash, want: %v", wantHash)
	}
	if gotHash != wantHash {
		t.Fatalf("Got request hash: %v, want: %v", gotHash, wantHash)
	}
}

// Tests the case where resources are removed from the management server,
// causing it to send an empty update to the xDS client, which returns a
// resource-not-found error to the xDS resolver. The test verifies that an
// ongoing RPC is handled to completion when this happens.
func (s) TestResolverRemovedWithRPCs(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure resources on the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: defaultTestServiceName,
		NodeID:     nodeID,
		Host:       defaultTestHostname,
		Port:       defaultTestPort[0],
		SecLevel:   e2e.SecurityLevelNone,
	})
	mgmtServer.Update(ctx, resources)

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Read the update pushed by the resolver to the ClientConn.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	// Delete the resources on the management server. This should result in a
	// resource-not-found error from the xDS client.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{NodeID: nodeID}); err != nil {
		t.Fatal(err)
	}

	// The RPC started earlier is still in progress. So, the xDS resolver will
	// not produce an empty service config at this point. Instead it will retain
	// the cluster to which the RPC is ongoing in the service config, but will
	// return an erroring config selector which will fail new RPCs.
	cs = verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))
	_, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err := verifyResolverError(err, codes.Unavailable, "has been removed", nodeID); err != nil {
		t.Fatal(err)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()

	// Now that the RPC is committed, the xDS resolver is expected to send an
	// update with an empty service config.
	var state resolver.State
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for an update from the resolver: %v", ctx.Err())
	case state = <-stateCh:
		if err := state.ServiceConfig.Err; err != nil {
			t.Fatalf("Received error in service config: %v", state.ServiceConfig.Err)
		}
		wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)("{}")
		if !internal.EqualServiceConfigForTesting(state.ServiceConfig.Config, wantSCParsed.Config) {
			t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, state.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
		}
	}

	// Add the resources back.
	mgmtServer.Update(ctx, resources)

	// The resolver should send a service config with the cluster name.
	cs = verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	// Verify that RPCs pass again.
	res, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	res.OnCommitted()
}

// Tests the case where resources returned by the management server are removed.
// The test verifies that the resolver pushes the expected config selector and
// service config in this case.
func (s) TestResolverRemovedResource(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure resources on the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: defaultTestServiceName,
		NodeID:     nodeID,
		Host:       defaultTestHostname,
		Port:       defaultTestPort[0],
		SecLevel:   e2e.SecurityLevelNone,
	})
	mgmtServer.Update(ctx, resources)
	stateCh, errCh, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Read the update pushed by the resolver to the ClientConn.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	// "Make an RPC" by invoking the config selector.
	res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}

	// "Finish the RPC"; this could cause a panic if the resolver doesn't
	// handle it correctly.
	res.OnCommitted()

	// Delete the resources on the management server, resulting in a
	// resource-not-found error from the xDS client.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{NodeID: nodeID}); err != nil {
		t.Fatal(err)
	}

	// The channel should receive the existing service config with the original
	// cluster but with an erroring config selector.
	cs = verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	// "Make another RPC" by invoking the config selector.
	_, err = cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err := verifyResolverError(err, codes.Unavailable, "has been removed", nodeID); err != nil {
		t.Fatal(err)
	}

	// In the meantime, an empty ServiceConfig update should have been sent.
	var state resolver.State
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for an update from the resolver: %v", ctx.Err())
	case state = <-stateCh:
		if err := state.ServiceConfig.Err; err != nil {
			t.Fatalf("Received error in service config: %v", state.ServiceConfig.Err)
		}
		wantSCParsed := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)("{}")
		if !internal.EqualServiceConfigForTesting(state.ServiceConfig.Config, wantSCParsed.Config) {
			t.Fatalf("Got service config:\n%s \nWant service config:\n%s", cmp.Diff(nil, state.ServiceConfig.Config), cmp.Diff(nil, wantSCParsed.Config))
		}
	}

	// The xDS resolver is expected to report an error to the channel.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for an error from the resolver: %v", ctx.Err())
	case err := <-errCh:
		if err := verifyResolverError(err, codes.Unavailable, "has been removed", nodeID); err != nil {
			t.Fatal(err)
		}
	}
}

// Tests the case where the resolver receives max stream duration as part of the
// listener and route configuration resources.  The test verifies that the RPC
// timeout returned by the config selector matches expectations. A non-nil max
// stream duration (this includes an explicit zero value) in a matching route
// overrides the value specified in the listener resource.
func (s) TestResolverMaxStreamDuration(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Configure the management server with a listener resource that specifies a
	// max stream duration as part of its HTTP connection manager. Also
	// configure a route configuration resource, which has multiple routes with
	// different values of max stream duration.
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: defaultTestRouteConfigName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
		CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
			MaxStreamDuration: durationpb.New(1 * time.Second),
		},
	})
	listeners := []*v3listenerpb.Listener{{
		Name:        defaultTestServiceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}}
	routes := []*v3routepb.RouteConfiguration{{
		Name: defaultTestRouteConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{defaultTestServiceName},
			Routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/foo"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
							Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
								{
									Name:   "A",
									Weight: &wrapperspb.UInt32Value{Value: 100},
								},
							}},
						},
						MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{
							MaxStreamDuration: durationpb.New(5 * time.Second),
						},
					}},
				},
				{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/bar"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
							Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
								{
									Name:   "B",
									Weight: &wrapperspb.UInt32Value{Value: 100},
								},
							}},
						},
						MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{
							MaxStreamDuration: durationpb.New(0 * time.Second),
						},
					}},
				},
				{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
							Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
								{
									Name:   "C",
									Weight: &wrapperspb.UInt32Value{Value: 100},
								},
							}},
						},
					}},
				},
			},
		}},
	}}
	cluster := []*v3clusterpb.Cluster{
		e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelNone),
		e2e.DefaultCluster("B", "endpoint_B", e2e.SecurityLevelNone),
		e2e.DefaultCluster("C", "endpoint_C", e2e.SecurityLevelNone),
	}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint("endpoint_A", defaultTestHostname, defaultTestPort),
		e2e.DefaultEndpoint("endpoint_B", defaultTestHostname, defaultTestPort),
		e2e.DefaultEndpoint("endpoint_C", defaultTestHostname, defaultTestPort),
	}
	configureAllResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, routes, cluster, endpoints)

	// Read the update pushed by the resolver to the ClientConn.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, "")

	testCases := []struct {
		name   string
		method string
		want   *time.Duration
	}{{
		name:   "RDS setting",
		method: "/foo/method",
		want:   newDurationP(5 * time.Second),
	}, {
		name:   "explicit zero in RDS; ignore LDS",
		method: "/bar/method",
		want:   nil,
	}, {
		name:   "no config in RDS; fallback to LDS",
		method: "/baz/method",
		want:   newDurationP(time.Second),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := iresolver.RPCInfo{
				Method:  tc.method,
				Context: ctx,
			}
			res, err := cs.SelectConfig(req)
			if err != nil {
				t.Errorf("cs.SelectConfig(%v): %v", req, err)
				return
			}
			res.OnCommitted()
			got := res.MethodConfig.Timeout
			if !cmp.Equal(got, tc.want) {
				t.Errorf("For method %q: res.MethodConfig.Timeout = %v; want %v", tc.method, got, tc.want)
			}
		})
	}
}

// Tests that clusters remain in service config if RPCs are in flight.
func (s) TestResolverDelayedOnCommitted(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure resources on the management server.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: defaultTestServiceName,
		NodeID:     nodeID,
		Host:       defaultTestHostname,
		Port:       defaultTestPort[0],
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Read the update pushed by the resolver to the ClientConn.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, wantServiceConfig(resources.Clusters[0].Name))

	// Make an RPC, but do not commit it yet.
	resOld, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	wantClusterName := fmt.Sprintf("cluster:%s", resources.Clusters[0].Name)
	if cluster := clustermanager.GetPickedClusterForTesting(resOld.Context); cluster != wantClusterName {
		t.Fatalf("Picked cluster is %q, want %q", cluster, wantClusterName)
	}

	// Delay resOld.OnCommitted(). As long as there are pending RPCs to removed
	// clusters, they still appear in the service config.
	oldClusterName := resources.Clusters[0].Name
	// Update the route configuration resource on the management server to
	// return a new cluster.
	newClusterName := "new-" + defaultTestClusterName
	newEndpointName := "new-" + defaultTestEndpointName
	resources.Routes = []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(resources.Routes[0].Name, defaultTestServiceName, newClusterName)}
	resources.Clusters = []*v3clusterpb.Cluster{e2e.DefaultCluster(newClusterName, newEndpointName, e2e.SecurityLevelNone)}
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(newEndpointName, defaultTestHostname, defaultTestPort)}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Read the update pushed by the resolver to the ClientConn and ensure the
	// old cluster is present in the service config. Also ensure that the newly
	// returned config selector does not hold a reference to the old cluster.
	wantSC := fmt.Sprintf(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:%s": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "%s"
					}
				  }
				]
			  },
			  "cluster:%s": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "%s"
					}
				  }
				]
			  }
			}
		  }
		}
	  ]
}`, oldClusterName, oldClusterName, newClusterName, newClusterName)
	cs = verifyUpdateFromResolver(ctx, t, stateCh, wantSC)

	resNew, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
	if err != nil {
		t.Fatalf("cs.SelectConfig(): %v", err)
	}
	wantClusterName = fmt.Sprintf("cluster:%s", newClusterName)
	if cluster := clustermanager.GetPickedClusterForTesting(resNew.Context); cluster != wantClusterName {
		t.Fatalf("Picked cluster is %q, want %q", cluster, wantClusterName)
	}

	// Invoke OnCommitted on the old RPC; should lead to a service config update
	// that deletes the old cluster, as the old cluster no longer has any
	// pending RPCs.
	resOld.OnCommitted()

	wantSC = fmt.Sprintf(`
{
	"loadBalancingConfig": [
		{
		  "xds_cluster_manager_experimental": {
			"children": {
			  "cluster:%s": {
				"childPolicy": [
				  {
					"cds_experimental": {
					  "cluster": "%s"
					}
				  }
				]
			  }
			}
		  }
		}
	  ]
}`, newClusterName, newClusterName)
	verifyUpdateFromResolver(ctx, t, stateCh, wantSC)
}

// Tests the case where two LDS updates with the same RDS name to watch are
// received without an RDS in between. Those LDS updates shouldn't trigger a
// service config update.
func (s) TestResolverMultipleLDSUpdates(t *testing.T) {
	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	// Configure the management server with a listener resource, but no route
	// configuration resource.
	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)}
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, nil)

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Ensure there is no update from the resolver.
	verifyNoUpdateFromResolver(ctx, t, stateCh)

	// Configure the management server with a listener resource that points to
	// the same route configuration resource but has different values for max
	// stream duration field. There is still no route configuration resource on
	// the management server.
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: defaultTestRouteConfigName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
		CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
			MaxStreamDuration: durationpb.New(1 * time.Second),
		},
	})
	listeners = []*v3listenerpb.Listener{{
		Name:        defaultTestServiceName,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}}
	configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, nil)

	// Ensure that there is no update from the resolver.
	verifyNoUpdateFromResolver(ctx, t, stateCh)
}

// TestResolverWRR tests the case where the route configuration returned by the
// management server contains a set of weighted clusters. The test performs a
// bunch of RPCs using the cluster specifier returned by the resolver, and
// verifies the cluster distribution.
func (s) TestResolverWRR(t *testing.T) {
	origNewWRR := rinternal.NewWRR
	rinternal.NewWRR = testutils.NewTestWRR
	defer func() { rinternal.NewWRR = origNewWRR }()

	// Spin up an xDS management server for the test.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	nodeID := uuid.New().String()
	mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

	stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

	// Configure resources on the management server.
	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)}
	routes := []*v3routepb.RouteConfiguration{e2e.RouteConfigResourceWithOptions(e2e.RouteConfigOptions{
		RouteConfigName:      defaultTestRouteConfigName,
		ListenerName:         defaultTestServiceName,
		ClusterSpecifierType: e2e.RouteConfigClusterSpecifierTypeWeightedCluster,
		WeightedClusters:     map[string]int{"A": 75, "B": 25},
	})}
	clusters := []*v3clusterpb.Cluster{
		e2e.DefaultCluster("A", "endpoint_A", e2e.SecurityLevelNone),
		e2e.DefaultCluster("B", "endpoint_B", e2e.SecurityLevelNone),
	}
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint("endpoint_A", defaultTestHostname, defaultTestPort),
		e2e.DefaultEndpoint("endpoint_B", defaultTestHostname, defaultTestPort),
	}
	configureAllResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, listeners, routes, clusters, endpoints)

	// Read the update pushed by the resolver to the ClientConn.
	cs := verifyUpdateFromResolver(ctx, t, stateCh, "")

	// Make RPCs to verify WRR behavior in the cluster specifier.
	picks := map[string]int{}
	for i := 0; i < 100; i++ {
		res, err := cs.SelectConfig(iresolver.RPCInfo{Context: ctx, Method: "/service/method"})
		if err != nil {
			t.Fatalf("cs.SelectConfig(): %v", err)
		}
		picks[clustermanager.GetPickedClusterForTesting(res.Context)]++
		res.OnCommitted()
	}
	want := map[string]int{"cluster:A": 75, "cluster:B": 25}
	if !cmp.Equal(picks, want) {
		t.Errorf("Picked clusters: %v; want: %v", picks, want)
	}
}

func (s) TestConfigSelector_FailureCases(t *testing.T) {
	const methodName = "1"

	tests := []struct {
		name     string
		listener *v3listenerpb.Listener
		wantErr  string
	}{
		{
			name: "route type RouteActionUnsupported invalid for client",
			listener: &v3listenerpb.Listener{
				Name: defaultTestServiceName,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
							RouteConfig: &v3routepb.RouteConfiguration{
								Name: defaultTestRouteConfigName,
								VirtualHosts: []*v3routepb.VirtualHost{{
									Domains: []string{defaultTestServiceName},
									Routes: []*v3routepb.Route{{
										Match: &v3routepb.RouteMatch{
											PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: methodName},
										},
										Action: &v3routepb.Route_FilterAction{},
									}},
								}},
							}},
						HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
					}),
				},
			},
			wantErr: "matched route does not have a supported route action type",
		},
		{
			name: "route type RouteActionNonForwardingAction invalid for client",
			listener: &v3listenerpb.Listener{
				Name: defaultTestServiceName,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
							RouteConfig: &v3routepb.RouteConfiguration{
								Name: defaultTestRouteConfigName,
								VirtualHosts: []*v3routepb.VirtualHost{{
									Domains: []string{defaultTestServiceName},
									Routes: []*v3routepb.Route{{
										Match: &v3routepb.RouteMatch{
											PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: methodName},
										},
										Action: &v3routepb.Route_NonForwardingAction{},
									}},
								}},
							}},
						HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
					}),
				},
			},
			wantErr: "matched route does not have a supported route action type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Spin up an xDS management server.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			nodeID := uuid.New().String()
			mgmtServer, _, _, bc := setupManagementServerForTest(t, nodeID)

			// Build an xDS resolver.
			stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, bc)

			// Update the management server with a listener resource that
			// contains inline route configuration.
			configureResourcesOnManagementServer(ctx, t, mgmtServer, nodeID, []*v3listenerpb.Listener{test.listener}, nil)

			// Ensure that the resolver pushes a state update to the channel.
			cs := verifyUpdateFromResolver(ctx, t, stateCh, "")

			// Ensure that it returns the expected error.
			_, err := cs.SelectConfig(iresolver.RPCInfo{Method: methodName, Context: ctx})
			if err := verifyResolverError(err, codes.Unavailable, test.wantErr, nodeID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func newDurationP(d time.Duration) *time.Duration {
	return &d
}

// TestResolver_AutoHostRewrite verifies the propagation of the AutoHostRewrite
// field from the xDS resolver.
//
// Per gRFC A81, this feature should only be active if two conditions met:
// 1. The environment variable (XDSAuthorityRewrite) is enabled.
// 2. The xDS server is marked as "trusted_xds_server" in the bootstrap config.
func (s) TestResolver_AutoHostRewrite(t *testing.T) {
	for _, tt := range []struct {
		name                string
		autoHostRewrite     bool
		envconfig           bool
		serverfeature       serverFeature.ServerFeature
		wantAutoHostRewrite bool
	}{
		{
			name:                "EnvVarDisabled_NonTrustedServer_AutoHostRewriteOff",
			autoHostRewrite:     false,
			envconfig:           false,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarDisabled_NonTrustedServer_AutoHostRewriteOn",
			autoHostRewrite:     true,
			envconfig:           false,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarDisabled_TrustedServer_AutoHostRewriteOff",
			autoHostRewrite:     false,
			envconfig:           false,
			serverfeature:       serverFeature.ServerFeatureTrustedXDSServer,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarDisabled_TrustedServer_AutoHostRewriteOn",
			autoHostRewrite:     true,
			envconfig:           false,
			serverfeature:       serverFeature.ServerFeatureTrustedXDSServer,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarEnabled_NonTrustedServer_AutoHostRewriteOff",
			autoHostRewrite:     false,
			envconfig:           true,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarEnabled_NonTrustedServer_AutoHostRewriteOn",
			autoHostRewrite:     true,
			envconfig:           true,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarEnabled_TrustedServer_AutoHostRewriteOff",
			autoHostRewrite:     false,
			envconfig:           true,
			serverfeature:       serverFeature.ServerFeatureTrustedXDSServer,
			wantAutoHostRewrite: false,
		},
		{
			name:                "EnvVarEnabled_TrustedServer_AutoHostRewriteOn",
			autoHostRewrite:     true,
			envconfig:           true,
			serverfeature:       serverFeature.ServerFeatureTrustedXDSServer,
			wantAutoHostRewrite: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.XDSAuthorityRewrite, tt.envconfig)

			// Spin up an xDS management server for the test.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			nodeID := uuid.New().String()
			mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
			defer mgmtServer.Stop()

			// Configure the management server with a good listener resource and a
			// route configuration resource, as specified by the test case.
			resources := e2e.UpdateOptions{
				NodeID:    nodeID,
				Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)},
				Routes: []*v3routepb.RouteConfiguration{{
					Name: defaultTestRouteConfigName,
					VirtualHosts: []*v3routepb.VirtualHost{{
						Domains: []string{defaultTestServiceName},
						Routes: []*v3routepb.Route{{
							Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
							Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{WeightedClusters: &v3routepb.WeightedCluster{
									Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
										{
											Name:   defaultTestClusterName,
											Weight: &wrapperspb.UInt32Value{Value: 100},
										},
									},
								}},
								HostRewriteSpecifier: &v3routepb.RouteAction_AutoHostRewrite{
									AutoHostRewrite: &wrapperspb.BoolValue{Value: tt.autoHostRewrite},
								},
							}},
						}},
					}},
				}},
				SkipValidation: true,
			}

			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			trustedXdsServer := "[]"
			if tt.serverfeature == serverFeature.ServerFeatureTrustedXDSServer {
				trustedXdsServer = `["trusted_xds_server"]`
			}

			opts := bootstrap.ConfigOptionsForTesting{
				Servers: []byte(fmt.Sprintf(`[{
					"server_uri": %q,
					"channel_creds": [{"type": "insecure"}],
					"server_features": %s
				}]`, mgmtServer.Address, trustedXdsServer)),
				Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
			}

			contents, err := bootstrap.NewContentsForTesting(opts)
			if err != nil {
				t.Fatalf("Failed to create bootstrap configuration: %v", err)
			}

			// Build the resolver and read the config selector out of it.
			stateCh, _, _ := buildResolverForTarget(t, resolver.Target{URL: *testutils.MustParseURL("xds:///" + defaultTestServiceName)}, contents)
			cs := verifyUpdateFromResolver(ctx, t, stateCh, "")

			res, err := cs.SelectConfig(iresolver.RPCInfo{
				Context: ctx,
				Method:  "/service/method",
			})
			if err != nil {
				t.Fatalf("cs.SelectConfig(): %v", err)
			}

			gotAutoHostRewrite := clusterimpl.AutoHostRewriteEnabledForTesting(res.Context)
			if gotAutoHostRewrite != tt.wantAutoHostRewrite {
				t.Fatalf("Got autoHostRewrite: %v, want: %v", gotAutoHostRewrite, tt.wantAutoHostRewrite)
			}
		})
	}
}
