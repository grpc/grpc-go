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
	"strings"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsdepmgr"

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
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Microsecond

	defaultTestServiceName     = "service-name"
	defaultTestRouteConfigName = "route-config-name"
	defaultTestClusterName     = "cluster-name"
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
}

// Update sends the received XDSConfig update to the update channel.
func (w *testWatcher) Update(cfg *xdsresource.XDSConfig) {
	w.updateCh <- cfg
}

// Error sends the received error to the error channel.
func (w *testWatcher) Error(err error) {
	w.errorCh <- err
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
		}
		if diff := cmp.Diff(update, want, cmpOpts...); diff != "" {
			return fmt.Errorf("received unexpected update from dependency manager. Diff (-got +want):\n%v", diff)
		}
	case err := <-errCh:
		return fmt.Errorf("received unexpected error from dependency manager: %v", err)
	}
	return nil
}

func createXDSClient(t *testing.T, bootstrapContents []byte) xdsclient.XDSClient {
	t.Helper()

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}

	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create an xDS client pool: %v", err)
	}
	c, cancel, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{Name: t.Name()})
	if err != nil {
		t.Fatalf("Failed to create an xDS client: %v", err)
	}
	t.Cleanup(cancel)
	return c
}

// Spins up an xDS management server and sets up the xDS bootstrap configuration.
//
// Returns the following:
//   - A reference to the xDS management server
//   - Contents of the bootstrap configuration pointing to xDS management
//     server
func setupManagementServerForTest(t *testing.T, nodeID string) (*e2e.ManagementServer, []byte) {
	t.Helper()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	t.Cleanup(mgmtServer.Stop)

	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	return mgmtServer, bootstrapContents
}

// Tests the happy case where the dependency manager receives all the required
// resources and verifies that Update is called with with the correct XDSConfig.
func (s) TestHappyCase(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
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
	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the listener contains an inline route configuration and
// verifies that Update is called with the correct XDSConfig.
func (s) TestInlineRouteConfig(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
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
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	dm := xdsdepmgr.New(defaultTestServiceName, defaultTestServiceName, xdsClient, watcher)
	defer dm.Close()

	wantConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			InlineRouteConfig: &xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{defaultTestServiceName},
						Routes: []*xdsresource.Route{{
							Prefix:           newStringP("/"),
							WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
							ActionType:       xdsresource.RouteActionRoute},
						},
					},
				},
			},
			HTTPFilters: []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute},
					},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where dependency manager only receives listener resource but
// does not receive route config resource. Verfies that Update is not called
// since we do not have all resources.
func (s) TestIncompleteResources(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
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
func (s) TestListenerResourceError(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Send a correct update first
	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	listener.FilterChains = nil
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
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

	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Remove listener resource so that we get listener resource error.
	resources.Listeners = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	if err := verifyError(ctx, watcher.errorCh, fmt.Sprintf("xds: resource %q of type %q has been removed", defaultTestServiceName, "ListenerResource"), nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where dependency manager receives a route config resource
// error by sending a route resource that is NACKed by the XDSClient. It
// verifies that Error is called with correct error.
func (s) TestRouteResourceError(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	errorCh := make(chan error, 1)
	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  errorCh,
	}
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

	if err := verifyError(ctx, watcher.errorCh, "route resource error", nodeID); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where route config updates receives does not have any virtual
// host. Verifies that Error is called with correct error.
func (s) TestNoVirtualHost(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	route.VirtualHosts = nil
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

	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Configure a valid listener and route.
	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
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

	// Wait for the initial valid update.
	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
	}
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
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// We expect no call to Error or Update on our watcher. We just wait for
	// a short duration to ensure that.
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
	listener = e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route = e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Routes:         []*v3routepb.RouteConfiguration{route},
		SkipValidation: true,
	}
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
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Initial resources with defaultTestClusterName
	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
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

	// Wait for the first update.
	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Update route to point to a new cluster.
	newClusterName := "new-cluster-name"
	route2 := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, newClusterName)
	resources.Routes = []*v3routepb.RouteConfiguration{route2}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the second update and verify it has the new cluster.
	wantXdsConfig.RouteConfig.VirtualHosts[0].Routes[0].WeightedClusters[0].Name = newClusterName
	wantXdsConfig.VirtualHost.Routes[0].WeightedClusters[0].Name = newClusterName
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the route resource is first sent from the management
// server and the changed to be inline with the listener and then again changed
// to be received from the management server. It verifies that each time Update
// called with the correct XDSConfig.
func (s) TestRouteResourceChangeToInline(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()
	xdsClient := createXDSClient(t, bc)

	watcher := &testWatcher{
		updateCh: make(chan *xdsresource.XDSConfig),
		errorCh:  make(chan error),
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Initial resources with defaultTestClusterName
	listener := e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
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

	// Wait for the first update.
	wantXdsConfig := &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			RouteConfigName: defaultTestRouteConfigName,
			HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{defaultTestServiceName},
					Routes: []*xdsresource.Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{defaultTestServiceName},
			Routes: []*xdsresource.Route{{
				Prefix:           newStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: defaultTestClusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute,
			}},
		},
	}
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Update route to point to a new cluster.
	newClusterName := "new-cluster-name"
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, newClusterName),
		},
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})}, // router fields are unused by grpc
	})
	resources.Listeners[0].ApiListener.ApiListener = hcm
	resources.Routes = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the second update and verify it has the new cluster.
	wantXdsConfig.Listener.InlineRouteConfig = &xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{Prefix: newStringP("/"),
					WeightedClusters: []xdsresource.WeightedCluster{{Name: newClusterName, Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute,
				}},
			},
		},
	}
	wantXdsConfig.Listener.RouteConfigName = ""
	wantXdsConfig.RouteConfig.VirtualHosts[0].Routes[0].WeightedClusters[0].Name = newClusterName
	wantXdsConfig.VirtualHost.Routes[0].WeightedClusters[0].Name = newClusterName
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}

	// Change the route resource back to non-inline.
	listener = e2e.DefaultClientListener(defaultTestServiceName, defaultTestRouteConfigName)
	route = e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, defaultTestClusterName)
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Routes:         []*v3routepb.RouteConfiguration{route},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the third update and verify it has the original cluster.
	wantXdsConfig.Listener.InlineRouteConfig = nil
	wantXdsConfig.Listener.RouteConfigName = defaultTestRouteConfigName
	wantXdsConfig.RouteConfig.VirtualHosts[0].Routes[0].WeightedClusters[0].Name = defaultTestClusterName
	wantXdsConfig.VirtualHost.Routes[0].WeightedClusters[0].Name = defaultTestClusterName
	if err := verifyXDSConfig(ctx, watcher.updateCh, watcher.errorCh, wantXdsConfig); err != nil {
		t.Fatal(err)
	}
}
