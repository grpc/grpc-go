/*
 *
 * Copyright 2021 gRPC authors.
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

package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

const (
	listenerName = "listener"
	clusterName  = "cluster"

	route1 = "route1"
	route2 = "route2"
	route3 = "route3"
)

// xdsSetupFoTests performs the following setup actions:
//   - spins up an xDS management server
//   - creates an xDS client with a bootstrap configuration pointing to the above
//     management server
//
// Returns the following:
// - a reference to the management server
// - nodeID to use when pushing resources to the management server
// - a channel to read lds resource names received by the management server
// - a channel to read rds resource names received by the management server
// - an xDS client to pass to the rdsHandler under test
func xdsSetupFoTests(t *testing.T) (*e2e.ManagementServer, string, chan []string, chan []string, xdsclient.XDSClient) {
	t.Helper()

	ldsNamesCh := make(chan []string, 1)
	rdsNamesCh := make(chan []string, 1)

	// Setup the management server to push the requested route configuration
	// resource names on to a channel for the test to inspect.
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			switch req.GetTypeUrl() {
			case version.V3ListenerURL:
				select {
				case <-ldsNamesCh:
				default:
				}
				select {
				case ldsNamesCh <- req.GetResourceNames():
				default:
				}
			case version.V3RouteConfigURL:
				select {
				case <-rdsNamesCh:
				default:
				}
				select {
				case rdsNamesCh <- req.GetResourceNames():
				default:
				}
			default:
				return fmt.Errorf("unexpected resources %v of type %q requested", req.GetResourceNames(), req.GetTypeUrl())
			}
			return nil
		},
		AllowResourceSubset: true,
	})
	t.Cleanup(cleanup)

	xdsC, cancel, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	return mgmtServer, nodeID, ldsNamesCh, rdsNamesCh, xdsC
}

// Waits for the wantNames to be pushed on to namesCh. Fails the test by calling
// t.Fatal if the context expires before that.
func waitForResourceNames(ctx context.Context, t *testing.T, namesCh chan []string, wantNames []string) {
	t.Helper()

	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		select {
		case <-ctx.Done():
		case gotNames := <-namesCh:
			if cmp.Equal(gotNames, wantNames, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(s1, s2 string) bool { return s1 < s2 })) {
				return
			}
			t.Logf("Received resource names %v, want %v", gotNames, wantNames)
		}
	}
	t.Fatalf("Timeout waiting for resource to be requested from the management server")
}

// Waits for an update to be pushed on updateCh and compares it to wantUpdate.
// Fails the test by calling t.Fatal if the context expires or if the update
// received on the channel does not match wantUpdate.
func verifyUpdateFromChannel(ctx context.Context, t *testing.T, updateCh chan rdsHandlerUpdate, wantUpdate rdsHandlerUpdate) {
	t.Helper()

	opts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
		cmp.AllowUnexported(rdsHandlerUpdate{}),
	}
	select {
	case gotUpdate := <-updateCh:
		if diff := cmp.Diff(gotUpdate, wantUpdate, opts...); diff != "" {
			t.Fatalf("Got unexpected route config update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for a route config update")
	}
}

func routeConfigResourceForName(name string) *v3routepb.RouteConfiguration {
	return e2e.RouteConfigResourceWithOptions(e2e.RouteConfigOptions{
		RouteConfigName:      name,
		ListenerName:         listenerName,
		ClusterSpecifierType: e2e.RouteConfigClusterSpecifierTypeCluster,
		ClusterName:          clusterName,
	})
}

var defaultRouteConfigUpdate = xdsresource.RouteConfigUpdate{
	VirtualHosts: []*xdsresource.VirtualHost{{
		Domains: []string{listenerName},
		Routes: []*xdsresource.Route{{
			Prefix:           newStringP("/"),
			ActionType:       xdsresource.RouteActionRoute,
			WeightedClusters: map[string]xdsresource.WeightedCluster{clusterName: {Weight: 1}},
		}},
	}},
}

// Tests the simplest scenario: the rds handler receives a single route name.
//
// The test verifies the following:
//   - the handler starts a watch for the given route name
//   - once an update it received from the management server, it is pushed to the
//     update channel
//   - once the handler is closed, the watch for the route name is canceled.
func (s) TestRDSHandler_SuccessCaseOneRDSWatch(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Configure the management server with a route config resource.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeConfigResourceForName(route1)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an rds handler and give it a single route to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})

	// Verify that the given route is requested.
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1})

	// Verify that the update is pushed to the handler's update channel.
	wantUpdate := rdsHandlerUpdate{updates: map[string]xdsresource.RouteConfigUpdate{route1: defaultRouteConfigUpdate}}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Close the rds handler and verify that the watch is canceled.
	rh.close()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{})
}

// Tests the case where the rds handler receives two route names to watch. The
// test verifies that when the handler receives only once of them, it does not
// push an update on the channel. And when the handler receives the second
// route, the test verifies that the update is pushed.
func (s) TestRDSHandler_SuccessCaseTwoUpdates(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Create an rds handler and give it a single route to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})

	// Verify that the given route is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1})

	// Update the rds handler to watch for two routes.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1, route2})

	// Configure the management server with a single route config resource.
	routeResource1 := routeConfigResourceForName(route1)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeResource1},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the rds handler does not send an update.
	sCtx, sCtxCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-updateCh:
		t.Fatal("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-sCtx.Done():
	}

	// Configure the management server with both route config resources.
	routeResource2 := routeConfigResourceForName(route2)
	resources.Routes = []*v3routepb.RouteConfiguration{routeResource1, routeResource2}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the update is pushed to the handler's update channel.
	wantUpdate := rdsHandlerUpdate{
		updates: map[string]xdsresource.RouteConfigUpdate{
			route1: defaultRouteConfigUpdate,
			route2: defaultRouteConfigUpdate,
		},
	}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Close the rds handler and verify that the watch is canceled.
	rh.close()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{})
}

// Tests the case where the rds handler receives an update with two routes, then
// receives an update with only one route. The rds handler is expected to cancel
// the watch for the route no longer present, and push a corresponding update
// with only one route.
func (s) TestRDSHandler_SuccessCaseDeletedRoute(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Create an rds handler and give it two routes to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})

	// Verify that the given routes are requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1, route2})

	// Configure the management server with two route config resources.
	routeResource1 := routeConfigResourceForName(route1)
	routeResource2 := routeConfigResourceForName(route2)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeResource1, routeResource2},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the update is pushed to the handler's update channel.
	wantUpdate := rdsHandlerUpdate{
		updates: map[string]xdsresource.RouteConfigUpdate{
			route1: defaultRouteConfigUpdate,
			route2: defaultRouteConfigUpdate,
		},
	}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Update the handler to watch only one of the two previous routes.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})

	// Verify that the other route is no longer requested.
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1})

	// Verify that an update is pushed with only one route.
	wantUpdate = rdsHandlerUpdate{updates: map[string]xdsresource.RouteConfigUpdate{route1: defaultRouteConfigUpdate}}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Close the rds handler and verify that the watch is canceled.
	rh.close()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{})
}

// Tests the case where the rds handler receives an update with two routes, and
// then receives an update with two routes, one previously there and one added
// (i.e. 12 -> 23). This should cause the route that is no longer there to be
// deleted and cancelled, and the route that was added should have a watch
// started for it. The test also verifies that an update is not pushed by the
// rds handler until the newly added route config resource is received from the
// management server.
func (s) TestRDSHandler_SuccessCaseTwoUpdatesAddAndDeleteRoute(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Create an rds handler and give it two routes to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})

	// Verify that the given routes are requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1, route2})

	// Configure the management server with two route config resources.
	routeResource1 := routeConfigResourceForName(route1)
	routeResource2 := routeConfigResourceForName(route2)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeResource1, routeResource2},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the update is pushed to the handler's update channel.
	wantUpdate := rdsHandlerUpdate{
		updates: map[string]xdsresource.RouteConfigUpdate{
			route1: defaultRouteConfigUpdate,
			route2: defaultRouteConfigUpdate,
		},
	}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Update the handler with a route that was already there and a new route.
	rh.updateRouteNamesToWatch(map[string]bool{route2: true, route3: true})
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route2, route3})

	// The handler should not send an update.
	sCtx, sCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-updateCh:
		t.Fatalf("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-sCtx.Done():
	}

	// Configure the management server with the third resource.
	routeResource3 := routeConfigResourceForName(route3)
	resources.Routes = append(resources.Routes, routeResource3)
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the update is pushed to the handler's update channel.
	wantUpdate = rdsHandlerUpdate{
		updates: map[string]xdsresource.RouteConfigUpdate{
			route2: defaultRouteConfigUpdate,
			route3: defaultRouteConfigUpdate,
		},
	}
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Close the rds handler and verify that the watch is canceled.
	rh.close()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{})
}

// Tests the scenario where the rds handler gets told to watch three rds
// configurations, gets two successful updates, then gets told to watch only
// those two. The rds handler should then write an update to update buffer.
func (s) TestRDSHandler_SuccessCaseSecondUpdateMakesRouteFull(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Create an rds handler and give it three routes to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true, route3: true})

	// Verify that the given routes are requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1, route2, route3})

	// Configure the management server with two route config resources.
	routeResource1 := routeConfigResourceForName(route1)
	routeResource2 := routeConfigResourceForName(route2)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeResource1, routeResource2},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// The handler should not send an update.
	sCtx, sCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-rh.updateChannel:
		t.Fatalf("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-sCtx.Done():
	}

	// Tell the rds handler to now only watch Route 1 and Route 2. This should
	// trigger the rds handler to write an update to the update buffer as it now
	// has full rds configuration.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})
	wantUpdate := rdsHandlerUpdate{
		updates: map[string]xdsresource.RouteConfigUpdate{
			route1: defaultRouteConfigUpdate,
			route2: defaultRouteConfigUpdate,
		},
	}
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1, route2})
	verifyUpdateFromChannel(ctx, t, updateCh, wantUpdate)

	// Close the rds handler and verify that the watch is canceled.
	rh.close()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{})
}

// TestErrorReceived tests the case where the rds handler receives a route name
// to watch, then receives an update with an error. This error should be then
// written to the update channel.
func (s) TestErrorReceived(t *testing.T) {
	mgmtServer, nodeID, _, rdsNamesCh, xdsC := xdsSetupFoTests(t)

	// Create an rds handler and give it a single route to watch.
	updateCh := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, nil, updateCh)
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})

	// Verify that the given route is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, rdsNamesCh, []string{route1})

	// Configure the management server with a single route config resource, that
	// is expected to be NACKed.
	routeResource := routeConfigResourceForName(route1)
	routeResource.VirtualHosts[0].RetryPolicy = &v3routepb.RetryPolicy{NumRetries: &wrapperspb.UInt32Value{Value: 0}}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Routes:         []*v3routepb.RouteConfiguration{routeResource},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	const wantErr = "received route is invalid: retry_policy.num_retries = 0; must be >= 1"
	select {
	case update := <-updateCh:
		if !strings.Contains(update.err.Error(), wantErr) {
			t.Fatalf("Update received with error %v, want error containing %v", update.err, wantErr)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel")
	}
}

func newStringP(s string) *string {
	return &s
}
