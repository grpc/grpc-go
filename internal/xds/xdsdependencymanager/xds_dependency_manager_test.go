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

package xdsdepmgr

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/status"

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

var wantXdsConfig = xdsresource.XDSConfig{
	Listener: xdsresource.ListenerUpdate{
		RouteConfigName: defaultTestRouteConfigName,
		HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}}},
	RouteConfig: xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{Prefix: newStringP("/"),
					WeightedClusters: map[string]xdsresource.WeightedCluster{defaultTestClusterName: {Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute}},
			},
		},
	},
	VirtualHost: &xdsresource.VirtualHost{
		Domains: []string{defaultTestServiceName},
		Routes: []*xdsresource.Route{{Prefix: newStringP("/"),
			WeightedClusters: map[string]xdsresource.WeightedCluster{defaultTestClusterName: {Weight: 100}},
			ActionType:       xdsresource.RouteActionRoute}},
	},
}

func newStringP(s string) *string {
	return &s
}

// testWatcher is a mock implementation of the ConfigWatcher interface
// that allows defining custom logic for its methods in each test.
type testWatcher struct {
	onUpdate func(xdsresource.XDSConfig)
	onError  func(error)
}

// OnUpdate calls the underlying onUpdate function if it's not nil.
func (w *testWatcher) OnUpdate(cfg xdsresource.XDSConfig) {
	if w.onUpdate != nil {
		w.onUpdate(cfg)
	}
}

// OnError calls the underlying onError function if it's not nil.
func (w *testWatcher) OnError(err error) {
	if w.onError != nil {
		w.onError(err)
	}
}

func verifyError(gotErr error, wantCode codes.Code, wantErr, wantNodeID string) error {
	if gotErr == nil {
		return fmt.Errorf("got nil error from resolver, want error with code %v", wantCode)
	}
	if !strings.Contains(gotErr.Error(), wantErr) {
		return fmt.Errorf("got error from resolver %q, want %q", gotErr, wantErr)
	}
	if gotCode := status.Code(gotErr); gotCode != wantCode {
		return fmt.Errorf("got error from resolver with code %v, want %v", gotCode, wantCode)
	}
	if !strings.Contains(gotErr.Error(), wantNodeID) {
		return fmt.Errorf("got error from resolver %q, want nodeID %q", gotErr, wantNodeID)
	}
	return nil
}

// newDependencyManagerForTest creates a new DependencyManager for testing purposes.
func newDependencyManagerForTest(t *testing.T, listenerName string, target string, bootstrapContents []byte, watcher ConfigWatcher) *DependencyManager {
	t.Helper()

	// Setup the bootstrap file contents.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}

	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create an xDS client pool: %v", err)
	}
	c, cancel, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name:               t.Name(),
		WatchExpiryTimeout: defaultTestTimeout,
	})
	if err != nil {
		t.Fatalf("Failed to create an xDS client: %v", err)
	}
	t.Cleanup(cancel)

	return New(listenerName, target, c, watcher)
}

// Spins up an xDS management server and sets up an xDS bootstrap configuration
// file that points to it.
//
// Returns the following:
//   - A reference to the xDS management server
//   - Contents of the bootstrap configuration pointing to xDS management
//     server
func setupManagementServerForTest(t *testing.T, nodeID string) (*e2e.ManagementServer, []byte) {
	t.Helper()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	return mgmtServer, bootstrapContents

}

// Tests the happy case where the dependency manager receives all the required
// resources and calls OnUpdate with the correct XDSConfig.
func (s) TestHappyCase(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			t.Errorf("Received unexpected error from dependency manager: %v", err)
		},
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
	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
	}

	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(update, wantXdsConfig, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)

		}
	}
}

// Tests the case where the listener contains an inline route configuration and
// verifies that OnUpdate is called with the correct XDSConfig.
func (s) TestInlineRouteConfig(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			t.Errorf("Received unexpected error from dependency manager: %v", err)
		},
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
	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	wantConfig := wantXdsConfig
	wantConfig.Listener.RouteConfigName = ""
	wantConfig.Listener.InlineRouteConfig = &xdsresource.RouteConfigUpdate{
		VirtualHosts: []*xdsresource.VirtualHost{
			{
				Domains: []string{defaultTestServiceName},
				Routes: []*xdsresource.Route{{Prefix: newStringP("/"),
					WeightedClusters: map[string]xdsresource.WeightedCluster{defaultTestClusterName: {Weight: 100}},
					ActionType:       xdsresource.RouteActionRoute}},
			},
		},
	}
	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
	}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(update, wantConfig, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}
}

// Tests the case where dependency manager only receives listener resource but
// does not receive route config resource. Verfies that OnUpdate is not called
// since we do not have all resources.
func (s) TestIncompleteResources(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			t.Errorf("Received unexpected error from dependency manager: %v", err)
		},
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
	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case update := <-updateCh:
		t.Fatalf("Received unexpected update from dependency manager: %v", pretty.ToJSON(update))
	}
}

// Tests the case where dependency manager receives a listener resource error by
// sending the correct update first and then removing the listener resource. It
// verifies that OnError is called with the correct error.
func (s) TestListenerResourceError(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	errorCh := make(chan error, 1)
	updateCh := make(chan xdsresource.XDSConfig, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			errorCh <- err
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Send a correct update first
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

	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
	}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(wantXdsConfig, update, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}

	// Remove listener resource so that we get listener resource error.
	resources.Listeners = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorCh:
		if err := verifyError(err, codes.Unavailable, fmt.Sprintf("xds: resource %q of type %q has been removed", defaultTestServiceName, "ListenerResource"), nodeID); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for error from dependency manager")
	}
}

// Tests the case where dependency manager receives a route config resource
// error by sending a route resource that is NACKed by the XDSClient. It
// verifies that OnError is called with correct error.
func (s) TestRouteResourceError(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	errorCh := make(chan error, 1)
	watcher := &testWatcher{
		onUpdate: func(xdsresource.XDSConfig) {
			t.Errorf("Received unexpected update from dependency manager")
		},
		onError: func(err error) {
			errorCh <- err
		},
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

	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	select {
	case err := <-errorCh:
		if err := verifyError(err, codes.Unavailable, "Route resource error", nodeID); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for error from dependency manager")
	}
}

// Tests the case where route config updates receives does not have any virtual
// host. Verifies that OnError is called with correct error.
func (s) TestNoVirtualHost(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	errorCh := make(chan error, 1)
	watcher := &testWatcher{
		onUpdate: func(xdsresource.XDSConfig) {
			t.Errorf("Received unexpected update from dependency manager")
		},
		onError: func(err error) {
			errorCh <- err
		},
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
	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	select {
	case err := <-errorCh:
		if err := verifyError(err, codes.Unavailable, "Could not find VirtualHost", ""); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for error from dependency manager")
	}
}

// Tests the case where we get a listener resource ambient error and verify that
// we correctly log the warning for it. To make sure we get a listener ambient
// error, we send a correct update first , then send an invalid one and then
// send the valid resource again. We send the valid resource again so that we
// can be sure the abmient error reaches the dependency manager since there is
// no other way to wait for it .
func (s) TestListenerResourceAmbientError(t *testing.T) {
	grpctest.ExpectWarning("Listener resource ambient error")
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	errorCh := make(chan error, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			errorCh <- err
		},
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

	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
	}
	// Wait for the initial valid update.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(wantXdsConfig, update, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}

	// Now, send an invalid listener resource. Since a valid one is already
	// cached, this should result in an ambient error.
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
	resources.Routes = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// We expect no call to OnError or OnUpdate on our watcher. We just wait for
	// a short duration to ensure that.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case err := <-errorCh:
		t.Fatalf("Unexpected call to OnError %v", err)
	case update := <-updateCh:
		t.Fatalf("Unexpected call to OnUpdate %v", pretty.ToJSON(update))
	case <-sCtx.Done():
	}

	// Send valide resources again
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
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(wantXdsConfig, update, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}
}

// Tests the case where we get a route resource ambient error and verify that we
// correctly log the warning for it. To make sure we get a route resource
// ambient error, we send a correct update first , then send an invalid one and
// then send the valid resource again. We send the valid resource again so that
// we can be sure the abmient error reaches the dependency manager since there
// is no other way to wait for it .
func (s) TestRouteResourceAmbientError(t *testing.T) {
	grpctest.ExpectWarning("Route resource ambient error")
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	errorCh := make(chan error, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			errorCh <- err
		},
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

	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
	}
	// Wait for the initial valid update.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(wantXdsConfig, update, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}

	// Make the route resource invalid
	resources.Routes[0].VirtualHosts[0].Routes[0].Match = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// We expect no call to OnError or OnUpdate on our watcher. We just wait for
	// a short duration to ensure that.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case err := <-errorCh:
		t.Fatalf("Unexpected call to OnError %v", err)
	case update := <-updateCh:
		t.Fatalf("Unexpected call to OnUpdate %v", pretty.ToJSON(update))
	case <-sCtx.Done():
	}

	// Send valide resources again
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
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for update from dependency manager")
	case update := <-updateCh:
		if diff := cmp.Diff(wantXdsConfig, update, cmpOpts...); diff != "" {
			t.Fatalf("Did not receive expected update from dependency manager,.  Diff (-got +want):\n%v", diff)
		}
	}
}

// Tests the case where the cluster name changes in the route resource update
// and verify that each time OnUpdate is called with correct cluster name.
func (s) TestRouteResourceUpdate(t *testing.T) {
	nodeID := uuid.New().String()
	mgmtServer, bc := setupManagementServerForTest(t, nodeID)
	defer mgmtServer.Stop()

	updateCh := make(chan xdsresource.XDSConfig, 1)
	watcher := &testWatcher{
		onUpdate: func(cfg xdsresource.XDSConfig) {
			updateCh <- cfg
		},
		onError: func(err error) {
			t.Errorf("Received unexpected error from dependency manager: %v", err)
		},
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

	dm := newDependencyManagerForTest(t, defaultTestServiceName, defaultTestServiceName, bc, watcher)
	defer dm.Close()

	// Wait for the first update.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial update from dependency manager")
	case update := <-updateCh:
		if gotCluster := update.VirtualHost.Routes[0].WeightedClusters[defaultTestClusterName]; gotCluster.Weight != 100 {
			t.Fatalf("Update has wrong cluster, got: %v, want: %v", update.VirtualHost.Routes[0].WeightedClusters, defaultTestClusterName)
		}
	}

	// Update route to point to a new cluster.
	newClusterName := "new-cluster-name"
	route2 := e2e.DefaultRouteConfig(defaultTestRouteConfigName, defaultTestServiceName, newClusterName)
	resources.Routes = []*v3routepb.RouteConfiguration{route2}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the second update and verify it has the new cluster.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for route update from dependency manager")
	case update := <-updateCh:
		if gotCluster := update.VirtualHost.Routes[0].WeightedClusters[newClusterName]; gotCluster.Weight != 100 {
			t.Fatalf("Second update has wrong cluster, got: %v, want: %v", update.VirtualHost.Routes[0].WeightedClusters, newClusterName)
		}
	}
}
