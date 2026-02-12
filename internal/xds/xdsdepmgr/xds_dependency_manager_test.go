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

package xdsdepmgr_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsdepmgr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	_ "google.golang.org/grpc/internal/xds/httpfilter/router" // Register the router filter
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	xdsdepmgr.EnableClusterAndEndpointsWatch = true
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond

	defaultTestServiceName     = "service-name"
	defaultTestRouteConfigName = "route-config-name"
	defaultTestClusterName     = "cluster-name"
	defaultTestEDSServiceName  = "eds-service-name"
)

func newStringP(s string) *string {
	return &s
}

// testWatcher is an implementation of the ConfigWatcher interface that sends
// the updates and errors received from the dependency manager to respective
// channels, for the tests to verify.
type testWatcher struct {
	updateCh chan *xdsresource.XDSConfig
	errorCh  chan error
	done     chan struct{}
}

func newTestWatcher() *testWatcher {
	return &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
		done:     make(chan struct{}),
	}
}

// Update sends the received XDSConfig update to the update channel. Does not
// send updates if the done channel is closed. The done channel is closed in the
// cases of errors because management server keeps sending error updates that
// cases multiple updates to be sent from dependency manager causing the update
// channel to be blocked.
func (w *testWatcher) Update(cfg *xdsresource.XDSConfig) {
	select {
	case <-w.done:
		return
	case w.updateCh <- cfg:
	}
}

// Error sends the received error to the error channel.
func (w *testWatcher) Error(err error) {
	select {
	case <-w.done:
		return
	case w.errorCh <- err:
	}
}

// Closes the testWatcher.done channel which will stop the updates being pushed
// to testWatcher.updateCh. This is the first thing that needs to happen as soon
// as the test ends before anything else closes to avoid deadlocks in tests with
// CDS and EDS errors because, in case of error , management server keeps
// sending multiple error updates.
func (w *testWatcher) close() {
	close(w.done)
}

func verifyError(ctx context.Context, errCh chan error, wantErr, wantNodeID string) error {
	select {
	case gotErr := <-errCh:
		if gotErr == nil {
			return fmt.Errorf("got nil error from resolver, want error %q", wantErr)
		}
		if !strings.Contains(gotErr.Error(), wantErr) {
			return fmt.Errorf("got error from resolver %q, want %q", gotErr, wantErr)
		}
		if !strings.Contains(gotErr.Error(), wantNodeID) {
			return fmt.Errorf("got error from resolver %q, want nodeID %q", gotErr, wantNodeID)
		}
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for error from dependency manager")
	}
	return nil
}

// This function determines the stable, canonical order for any two
// resolver.Endpoint structs.
func lessEndpoint(a, b resolver.Endpoint) bool {
	return getHash(a) < getHash(b)
}

func getHash(e resolver.Endpoint) uint64 {
	h := xxhash.New()

	// We iterate through all addresses to ensure the hash represents
	// the full endpoint identity.
	for _, addr := range e.Addresses {
		h.Write([]byte(addr.Addr))
		h.Write([]byte(addr.ServerName))
	}

	return h.Sum64()
}

func verifyXDSConfig(ctx context.Context, xdsCh chan *xdsresource.XDSConfig, errCh chan error, want *xdsresource.XDSConfig) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for update from dependency manager")
	case update := <-xdsCh:
		cmpOpts := []cmp.Option{
			cmpopts.EquateEmpty(),
			cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
			cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
			cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
			cmpopts.IgnoreFields(xdsresource.ClusterUpdate{}, "Raw", "LBPolicy", "TelemetryLabels"),
			cmpopts.IgnoreFields(xdsresource.EndpointsUpdate{}, "Raw"),
			// Used for EndpointConfig.ResolutionNote and ClusterResult.Err fields.
			cmp.Transformer("ErrorsToString", func(in error) string {
				if in == nil {
					return "" // Treat nil as an empty string
				}
				s := in.Error()

				// Replace all sequences of whitespace (including newlines and
				// tabs) with a single standard space.
				s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

				// Trim any leading/trailing space that might be left over and
				// return error as string.
				return strings.TrimSpace(s)
			}),
			cmpopts.SortSlices(lessEndpoint),
		}
		if diff := cmp.Diff(update, want, cmpOpts...); diff != "" {
			return fmt.Errorf("received unexpected update from dependency manager. Diff (-got +want):\n%v", diff)
		}
	case err := <-errCh:
		return fmt.Errorf("received unexpected error from dependency manager: %v", err)
	}
	return nil
}

func makeXDSConfig(routeConfigName, clusterName, edsServiceName, addr string) *xdsresource.XDSConfig {
	return &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: routeConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: clusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: clusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			},
		},
		Clusters: map[string]*xdsresource.ClusterResult{
			clusterName: {
				Config: xdsresource.ClusterConfig{Cluster: &xdsresource.ClusterUpdate{
					ClusterType:    xdsresource.ClusterTypeEDS,
					ClusterName:    clusterName,
					EDSServiceName: edsServiceName,
				},
					EndpointConfig: &xdsresource.EndpointConfig{
						EDSUpdate: &xdsresource.EndpointsUpdate{
							Localities: []xdsresource.Locality{
								{ID: clients.Locality{
									Region:  "region-1",
									Zone:    "zone-1",
									SubZone: "subzone-1",
								},
									Endpoints: []xdsresource.Endpoint{
										{
											ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}},
											HealthStatus:     xdsresource.EndpointHealthStatusUnknown,
											Weight:           1,
										},
									},
									Weight: 1,
								},
							},
						},
					},
				},
			},
		},
	}
}

// setupManagementServerAndClient creates a management server, an xds client and
// returns the node ID, management server and xds client.
func setupManagementServerAndClient(t *testing.T, allowResourceSubset bool) (string, *e2e.ManagementServer, xdsclient.XDSClient) {
	t.Helper()
	nodeID := uuid.New().String()
	mgmtServer, bootstrapContents := setupManagementServerForTest(t, nodeID, allowResourceSubset)
	xdsClient := createXDSClient(t, bootstrapContents)
	return nodeID, mgmtServer, xdsClient
}

func createXDSClient(t *testing.T, bootstrapContents []byte) xdsclient.XDSClient {
	t.Helper()

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}

	pool := xdsclient.NewPool(config)
	c, cancel, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{Name: t.Name()})
	if err != nil {
		t.Fatalf("Failed to create an xDS client: %v", err)
	}
	t.Cleanup(cancel)
	return c
}

// Spins up an xDS management server and sets up the xDS bootstrap
// configuration.
//
// Returns the following:
//   - A reference to the xDS management server
//   - Contents of the bootstrap configuration pointing to xDS management
//     server
func setupManagementServerForTest(t *testing.T, nodeID string, allowResourceSubset bool) (*e2e.ManagementServer, []byte) {
	t.Helper()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: allowResourceSubset,
	})
	t.Cleanup(mgmtServer.Stop)

	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	return mgmtServer, bootstrapContents
}

// makeAggregateClusterResource returns an aggregate cluster resource with the
// given name and list of child names.
func makeAggregateClusterResource(name string, childNames []string) *v3clusterpb.Cluster {
	return e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: name,
		Type:        e2e.ClusterTypeAggregate,
		ChildNames:  childNames,
	})
}

// makeLogicalDNSClusterResource returns a LOGICAL_DNS cluster resource with the
// given name and given DNS host and port.
func makeLogicalDNSClusterResource(name, dnsHost string, dnsPort uint32) *v3clusterpb.Cluster {
	return e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: name,
		Type:        e2e.ClusterTypeLogicalDNS,
		DNSHostName: dnsHost,
		DNSPort:     dnsPort,
	})
}

// replaceDNSResolver unregisters the DNS resolver and registers a manual
// resolver for the same scheme. This allows the test to fake the DNS resolution
// by supplying the addresses of the test backends.
func replaceDNSResolver(t *testing.T) *manual.Resolver {
	t.Helper()
	mr := manual.NewBuilderWithScheme("dns")

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return mr
}

// Tests the happy case where the dependency manager receives all the required
// resources and verifies that Update is called with the correct XDSConfig.
func (s) TestHappyCase(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the listener contains an inline route configuration and
// verifies that Update is called with the correct XDSConfig.
func (s) TestInlineRouteConfig(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName),
		},
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})}, // router fields are unused by grpc
	})
	listener := &v3listenerpb.Listener{
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
	cluster := e2e.DefaultCluster(defaultTestClusterName, defaultTestEDSServiceName, e2e.SecurityLevelNone)
	endpoint := e2e.DefaultEndpoint(defaultTestEDSServiceName, "localhost", []uint32{8080})
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Clusters:       []*v3clusterpb.Cluster{cluster},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{endpoint},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	wantXdsConfig := makeXDSConfig(defaultTestRouteConfigName, defaultTestClusterName, defaultTestEDSServiceName, "localhost:8080")
	wantXdsConfig.Listener.InlineRouteConfig = wantXdsConfig.RouteConfig
	wantXdsConfig.Listener.RouteConfigName = ""

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where dependency manager only receives listener resource but
// does not receive route config resource. Verifies that Update is not called
// since we do not have all resources.
func (s) TestNoRouteConfigResource(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}
	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case update := <-watcher.updateCh:
		t.Fatalf("Received unexpected update from dependency manager: %+v", update)
	case err := <-watcher.errorCh:
		t.Fatalf("Received unexpected error from dependency manager: %v", err)
	}
}

// Tests the case where dependency manager receives a listener resource error by
// sending the correct update first and then removing the listener resource. It
// verifies that Error is called with the correct error.
func (s) TestListenerResourceNotFoundError(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Send a correct update first
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Remove listener resource so that we get listener resource error.
	resources.Listeners = nil
	resources.SkipValidation = true
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	if err := verifyError(ctx, watcher.errorCh, fmt.Sprintf("xds: resource %q of type %q has been removed", defaultTestServiceName, "ListenerResource"), nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where the Dependency Manager receives an invalid
// RouteConfiguration from the management server. The test provides a
// malformed resource to trigger a NACK, and verifies that the Dependency
// Manager propagates the resulting error via Error method.
func (s) TestRouteConfigResourceError(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	// Remove the Match to make sure the route resource is NACKed by XDSClient
	// sending a route resource error to dependency manager.
	route.VirtualHosts[0].Routes[0].Match = nil
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Routes:         []*v3routepb.RouteConfiguration{route},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Defer closing the watcher to prevent a potential hang. The management
	// server may send repeated errors, triggering updates that hold the
	// dependency manager's mutex. This defer is defined last so it executes
	// first (before dm.Close()). If we don't stop the watcher, dm.Close() will
	// deadlock waiting for the mutex currently held by the blocking Update
	// call.
	defer watcher.close()

	if err := verifyError(ctx, watcher.errorCh, "route resource error", nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where a received route configuration update has no virtual
// hosts. Verifies that Error is called with the expected error.
func (s) TestNoVirtualHost(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID, false)
	xdsClient := createXDSClient(t, bc)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	// Make the virtual host match nil so that the route config is NACKed.
	resources.Routes[0].VirtualHosts = nil

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	if err := verifyError(ctx, watcher.errorCh, "could not find VirtualHost", nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where we already have a cached resource and then we get a
// route resource with no virtual host, which also results in error being sent
// across.
func (s) TestNoVirtualHost_ExistingResource(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Verify valid update.
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Endpoints[0].ClusterName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// 3. Send route update with no virtual host.
	resources.Routes[0].VirtualHosts = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// 4. Verify error.
	if err := verifyError(ctx, watcher.errorCh, "could not find VirtualHost", nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where we get an ambient error and verify that we correctly log
// a warning for it. To make sure we get an ambient error, we send a correct
// update first, then send an invalid one and then send the valid resource
// again. We send the valid resource again so that we can be sure the ambient
// error reaches the dependency manager since there is no other way to wait for
// it.
func (s) TestAmbientError(t *testing.T) {
	// Expect a warning log for the ambient error.
	grpctest.ExpectWarning("Listener resource ambient error")

	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Configure a valid resources.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Wait for the initial valid update.
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Configure a listener resource that is expected to be NACKed because it
	// does not contain the `RouteSpecifier` field in the HTTPConnectionManager.
	// Since a valid one is already cached, this should result in an ambient
	// error.
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
	resources.Listeners = []*v3listenerpb.Listener{lis}
	resources.SkipValidation = true
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// We expect no call to Error or Update on our watcher. We just wait for a
	// short duration to ensure that.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case err := <-watcher.errorCh:
		t.Fatalf("Unexpected call to Error %v", err)
	case update := <-watcher.updateCh:
		t.Fatalf("Unexpected call to Update %+v", update)
	case <-sCtx.Done():
	}

	// Send valid resources again.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the cluster name changes in the route resource update
// and verify that each time Update is called with correct cluster name.
func (s) TestRouteResourceUpdate(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Configure initial resources
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Wait for the first update.
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Update route to point to a new cluster.
	newClusterName := "new-cluster-name"
	route2 := e2e.DefaultRouteConfig(resources.Routes[0].Name, defaultTestServiceName, newClusterName)
	cluster2 := e2e.DefaultCluster(newClusterName, newClusterName, e2e.SecurityLevelNone)
	endpoints2 := e2e.DefaultEndpoint(newClusterName, "localhost", []uint32{8081})
	resources.Routes = []*v3routepb.RouteConfiguration{route2}
	resources.Clusters = []*v3clusterpb.Cluster{cluster2}
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{endpoints2}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the second update and verify it has the new cluster.
	wantXdsConfig.RouteConfig.VirtualHosts[0].Routes[0].WeightedClusters[0].Name = newClusterName
	wantXdsConfig.VirtualHost.Routes[0].WeightedClusters[0].Name = newClusterName
	wantXdsConfig.Clusters = map[string]*xdsresource.ClusterResult{
		newClusterName: {
			Config: xdsresource.ClusterConfig{
				Cluster: &xdsresource.ClusterUpdate{
					ClusterName:    newClusterName,
					ClusterType:    xdsresource.ClusterTypeEDS,
					EDSServiceName: newClusterName,
				},
				EndpointConfig: &xdsresource.EndpointConfig{
					EDSUpdate: &xdsresource.EndpointsUpdate{
						Localities: []xdsresource.Locality{
							{ID: clients.Locality{
								Region:  "region-1",
								Zone:    "zone-1",
								SubZone: "subzone-1",
							},
								Endpoints: []xdsresource.Endpoint{
									{
										ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "localhost:8081"}}},
										HealthStatus:     xdsresource.EndpointHealthStatusUnknown,
										Weight:           1,
									},
								},
								Weight: 1,
							},
						},
					},
				},
			},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the route resource is first sent from the management
// server and the changed to be inline with the listener and then again changed
// to be received from the management server. It verifies that each time Update
// is called with the correct XDSConfig.
func (s) TestRouteResourceChangeToInline(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Initial resources with defaultTestClusterName
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Wait for the first update.
	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Update route to point to a new cluster and make it inline with the
	// listener.
	newClusterName := "new-cluster-name"
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, newClusterName),
		},
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})}, // router fields are unused by grpc
	})
	resources.Listeners[0].ApiListener.ApiListener = hcm
	resources.Clusters = []*v3clusterpb.Cluster{e2e.DefaultCluster(newClusterName, defaultTestEDSServiceName, e2e.SecurityLevelNone)}
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(defaultTestEDSServiceName, "localhost", []uint32{8081})}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the second update and verify it has the new cluster.
	wantInlineXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			HTTPFilters: []xdsresource.HTTPFilter{{Name: "router"}},
			InlineRouteConfig: &xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{defaultTestServiceName},
						Routes: []*xdsresource.Route{{
							Prefix:           newStringP("/"),
							WeightedClusters: []xdsresource.WeightedCluster{{Name: newClusterName, Weight: 100}},
							ActionType:       xdsresource.RouteActionRoute,
						}},
					},
				},
			},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: newClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: newClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			},
		},
		Clusters: map[string]*xdsresource.ClusterResult{
			newClusterName: {
				Config: xdsresource.ClusterConfig{Cluster: &xdsresource.ClusterUpdate{
					ClusterType:    xdsresource.ClusterTypeEDS,
					ClusterName:    newClusterName,
					EDSServiceName: defaultTestEDSServiceName,
				},
					EndpointConfig: &xdsresource.EndpointConfig{
						EDSUpdate: &xdsresource.EndpointsUpdate{
							Localities: []xdsresource.Locality{
								{ID: clients.Locality{
									Region:  "region-1",
									Zone:    "zone-1",
									SubZone: "subzone-1",
								},
									Endpoints: []xdsresource.Endpoint{
										{
											ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "localhost:8081"}}},
											HealthStatus:     xdsresource.EndpointHealthStatusUnknown,
											Weight:           1,
										},
									},
									Weight: 1,
								},
							},
						},
					},
				},
			},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantInlineXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Change the route resource back to non-inline.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the dependency manager receives a cluster resource error
// and verifies that Update is called with XDSConfig containing cluster error.
func (s) TestClusterResourceError(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Defer closing the watcher to prevent a potential hang. The management
	// server may send repeated errors, triggering updates that hold the
	// dependency manager's mutex. This defer is defined last so it executes
	// first (before dm.Close()). If we don't stop the watcher, dm.Close() will
	// deadlock waiting for the mutex currently held by the blocking Update
	// call.
	defer watcher.close()

	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	wantXdsConfig.Clusters[resources.Clusters[0].Name] = &xdsresource.ClusterResult{Err: fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("unsupported config_source_specifier *corev3.ConfigSource_Ads in lrs_server field"))}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the dependency manager receives a cluster resource
// ambient error. A valid cluster resource is sent first, then an invalid
// one and then the valid resource again. The valid resource is sent again
// to make sure that the ambient error reaches the dependency manager since
// there is no other way to wait for it.
func (s) TestClusterAmbientError(t *testing.T) {
	// Expect a warning log for the ambient error.
	grpctest.ExpectWarning("Cluster resource ambient error")

	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, false)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Clusters[0].EdsClusterConfig.ServiceName, "localhost:8080")
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Configure a cluster resource that is expected to be NACKed because it
	// does not contain the `LrsServer` field. Since a valid one is already
	// cached, this should result in an ambient error.
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{}}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case <-time.After(defaultTestShortTimeout):
	case update := <-watcher.updateCh:
		t.Fatalf("received unexpected update from dependency manager: %v", update)
	}

	// Send valid resources again to guarantee we get the cluster ambient error
	// before the test ends.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where a cluster is an aggregate cluster whose children are of
// type EDS and DNS. Verifies that Update is not called when one of the child
// resources is not configured and then verifies that Update is called with
// correct config when all resources are configured.
func (s) TestAggregateCluster(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, true)

	dnsR := replaceDNSResolver(t)
	dnsR.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: "127.0.0.1:8081"}}},
			{Addresses: []resolver.Address{{Addr: "[::1]:8081"}}},
		},
	})

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	aggregateCluster := makeAggregateClusterResource(defaultTestClusterName, []string{"eds-cluster", "dns-cluster"})
	edsCluster := e2e.DefaultCluster("eds-cluster", defaultTestEDSServiceName, e2e.SecurityLevelNone)
	endpoint := e2e.DefaultEndpoint(defaultTestEDSServiceName, "localhost", []uint32{8080})
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Clusters:  []*v3clusterpb.Cluster{aggregateCluster, edsCluster},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoint},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Verify that no configuration is pushed to the child policy yet, because
	// not all clusters making up the aggregate cluster have been resolved yet.
	select {
	case <-time.After(defaultTestShortTimeout):
	case update := <-watcher.updateCh:
		t.Fatalf("received unexpected update from dependency manager: %+v", update)
	case err := <-watcher.errorCh:
		t.Fatalf("received unexpected error from dependency manager: %v", err)
	}

	// Now configure the LogicalDNS cluster in the management server. This
	// should result in configuration being pushed down to the child policy.
	resources.Clusters = append(resources.Clusters, makeLogicalDNSClusterResource("dns-cluster", "localhost", 8081))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: resources.Routes[0].Name,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: resources.Clusters[0].Name, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: resources.Clusters[0].Name, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			}},
		Clusters: map[string]*xdsresource.ClusterResult{
			resources.Clusters[0].Name: {
				Config: xdsresource.ClusterConfig{Cluster: &xdsresource.ClusterUpdate{
					ClusterType:             xdsresource.ClusterTypeAggregate,
					ClusterName:             resources.Clusters[0].Name,
					PrioritizedClusterNames: []string{"eds-cluster", "dns-cluster"},
				},
					AggregateConfig: &xdsresource.AggregateConfig{LeafClusters: []string{"eds-cluster", "dns-cluster"}},
				},
			},
			"eds-cluster": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						ClusterType:    xdsresource.ClusterTypeEDS,
						ClusterName:    "eds-cluster",
						EDSServiceName: defaultTestEDSServiceName},
					EndpointConfig: &xdsresource.EndpointConfig{
						EDSUpdate: &xdsresource.EndpointsUpdate{
							Localities: []xdsresource.Locality{
								{ID: clients.Locality{
									Region:  "region-1",
									Zone:    "zone-1",
									SubZone: "subzone-1",
								},
									Endpoints: []xdsresource.Endpoint{
										{
											ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: "localhost:8080"}}},
											HealthStatus:     xdsresource.EndpointHealthStatusUnknown,
											Weight:           1,
										},
									},
									Weight: 1,
								},
							},
						},
					},
				}},
			"dns-cluster": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						ClusterType: xdsresource.ClusterTypeLogicalDNS,
						ClusterName: "dns-cluster",
						DNSHostName: "localhost:8081",
					},
					EndpointConfig: &xdsresource.EndpointConfig{
						DNSEndpoints: &xdsresource.DNSUpdate{
							Endpoints: []resolver.Endpoint{
								{Addresses: []resolver.Address{{Addr: "127.0.0.1:8081"}}},
								{Addresses: []resolver.Address{{Addr: "[::1]:8081"}}},
							},
						},
					},
				},
			},
		},
	}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

}

// Tests the case where an aggregate cluster has one child whose resource is
// configured with an error. Verifies that the error is correctly received in
// the XDSConfig.
func (s) TestAggregateClusterChildError(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, true)

	watcher := newTestWatcher()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	dnsR := replaceDNSResolver(t)
	dnsR.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: "127.0.0.1:8081"}}},
			{Addresses: []resolver.Address{{Addr: "[::1]:8081"}}},
		},
	})

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(defaultTestClusterName, []string{"err-cluster", "good-cluster"}),
			e2e.DefaultCluster("err-cluster", defaultTestEDSServiceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource("good-cluster", "localhost", 8081),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(defaultTestEDSServiceName, "localhost", []uint32{8080})},
	}
	resources.Endpoints[0].Endpoints[0].LbEndpoints[0].LoadBalancingWeight = &wrapperspb.UInt32Value{Value: 0}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Defer closing the watcher to prevent a potential hang. The management
	// server may send repeated errors, triggering updates that hold the
	// dependency manager's mutex. This defer is defined last so it executes
	// first (before dm.Close()). If we don't stop the watcher, dm.Close() will
	// deadlock waiting for the mutex currently held by the blocking Update
	// call.
	defer watcher.close()

	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{
					Prefix:           newStringP("/"),
					WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute,
				}},
			}},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
		Clusters: map[string]*xdsresource.ClusterResult{
			defaultTestClusterName: {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						ClusterType:             xdsresource.ClusterTypeAggregate,
						ClusterName:             defaultTestClusterName,
						PrioritizedClusterNames: []string{"err-cluster", "good-cluster"},
					},
					AggregateConfig: &xdsresource.AggregateConfig{LeafClusters: []string{"err-cluster", "good-cluster"}},
				},
			},
			"err-cluster": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						ClusterType:    xdsresource.ClusterTypeEDS,
						ClusterName:    "err-cluster",
						EDSServiceName: defaultTestEDSServiceName,
					},
					EndpointConfig: &xdsresource.EndpointConfig{
						ResolutionNote: fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("EDS response contains an endpoint with zero weight: endpoint:{address:{socket_address:{address:%q  port_value:%v}}}  load_balancing_weight:{}", "localhost", 8080)),
					},
				},
			},
			"good-cluster": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						ClusterType: xdsresource.ClusterTypeLogicalDNS,
						ClusterName: "good-cluster",
						DNSHostName: "localhost:8081",
					},
					EndpointConfig: &xdsresource.EndpointConfig{
						DNSEndpoints: &xdsresource.DNSUpdate{
							Endpoints: []resolver.Endpoint{
								{Addresses: []resolver.Address{{Addr: "127.0.0.1:8081"}}},
								{Addresses: []resolver.Address{{Addr: "[::1]:8081"}}},
							},
						},
					},
				},
			},
		},
	}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where an aggregate cluster has no leaf clusters by creating a
// cyclic dependency where A->B and B->A. Verifies that an error with "no leaf
// clusters found" is received.
func (s) TestAggregateClusterNoLeafCluster(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, true)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const (
		clusterNameA = defaultTestClusterName // cluster name in cds LB policy config
		clusterNameB = defaultTestClusterName + "-B"
	)
	// Create a cyclic dependency where A->B and B->A.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB}),
			makeAggregateClusterResource(clusterNameB, []string{clusterNameA}),
		},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{
					Prefix:           newStringP("/"),
					WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute,
				}},
			}},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
		Clusters: map[string]*xdsresource.ClusterResult{
			defaultTestClusterName: {
				Err: fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("aggregate cluster graph has no leaf clusters")),
			},
			clusterNameB: {
				Err: fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("aggregate cluster graph has no leaf clusters")),
			},
		},
	}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where nested aggregate clusters exceed the max depth of 16.
// Verify that the error is correctly received in the XDSConfig in all the
// clusters.
func (s) TestAggregateClusterMaxDepth(t *testing.T) {
	const clusterDepth = 17
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, true)

	watcher := newTestWatcher()
	defer watcher.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a graph of aggregate clusters with 18 clusters.
	clusters := make([]*v3clusterpb.Cluster, clusterDepth)
	for i := 0; i < clusterDepth; i++ {
		clusters[i] = makeAggregateClusterResource(fmt.Sprintf("agg-%d", i), []string{fmt.Sprintf("agg-%d", i+1)})
	}

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, "agg-0")},
		Clusters:       clusters,
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	commonError := fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("aggregate cluster graph exceeds max depth (%d)", 16))

	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{
					// The route should point to the first cluster in the chain:
					// agg-0
					Prefix:           newStringP("/"),
					WeightedClusters: []xdsresource.WeightedCluster{{Name: "agg-0", Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute,
				}},
			}},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: "agg-0", Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
		Clusters: map[string]*xdsresource.ClusterResult{}, // Initialize the map
	}

	// Populate the Clusters map with all clusters,except the last one, each
	// having the common error
	for i := 0; i < clusterDepth; i++ {
		clusterName := fmt.Sprintf("agg-%d", i)

		// The ClusterResult only needs the Err field set to the common error
		wantXdsConfig.Clusters[clusterName] = &xdsresource.ClusterResult{
			Err: commonError,
		}
	}

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where the Endpoint watcher receives an ambient error. Tests
// verifies that the error is stored in resolution note and the update remains
// too.
func (s) TestEndpointAmbientError(t *testing.T) {
	nodeID, mgmtServer, xdsClient := setupManagementServerAndClient(t, true)

	watcher := newTestWatcher()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		NodeID:     nodeID,
		DialTarget: defaultTestServiceName,
		Host:       "localhost",
		Port:       8080,
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	// Defer closing the watcher to prevent a potential hang. The management
	// server may send repeated errors, triggering updates that hold the
	// dependency manager's mutex. This defer is defined last so it executes
	// first (before dm.Close()). If we don't stop the watcher, dm.Close() will
	// deadlock waiting for the mutex currently held by the blocking Update
	// call.
	defer watcher.close()

	wantXdsConfig := makeXDSConfig(resources.Routes[0].Name, resources.Clusters[0].Name, resources.Endpoints[0].ClusterName, "localhost:8080")

	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Send an ambient error for the endpoint resource by setting the weight to
	// 0.
	resources.Endpoints[0].Endpoints[0].LbEndpoints[0].LoadBalancingWeight = &wrapperspb.UInt32Value{Value: 0}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	wantXdsConfig.Clusters[resources.Clusters[0].Name].Config.EndpointConfig.ResolutionNote = fmt.Errorf("[xDS node id: %v]: %v", nodeID, fmt.Errorf("EDS response contains an endpoint with zero weight: endpoint:{address:{socket_address:{address:%q port_value:%v}}} load_balancing_weight:{}", "localhost", 8080))
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}
