package server

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

var (
	route1 = "route1"
	route2 = "route2"
	route3 = "route3"
)

// setupTests creates a rds handler with a fake xds client for control over the
// xds client.
func setupTests(t *testing.T) (*rdsHandler, *fakeclient.Client) {
	xdsC := fakeclient.NewClient()
	rh := newRdsHandler(&listenerWrapper{xdsC: xdsC})
	return rh, xdsC
}

// Simplest test: the rds handler receives a route name of length 1, starts a
// watch, gets a successful update, and then writes an update to the update
// channel for listener to pick up.
func (s) TestSuccessCaseOneRDSWatch(t *testing.T) {
	rh, fakeClient := setupTests(t)
	// When you first update the rds handler with a list of a single Route names
	// that needs dynamic RDS Configuration, this Route name has not been seen
	// before, so the RDS Handler should start a watch on that RouteName.
	routeNames := map[string]bool{route1: true}
	rh.updateRouteNamesToWatch(routeNames)
	// The RDS Handler should start a watch for that routeName.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotRoute, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	if gotRoute != route1 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route1)
	}
	rdsUpdate := xdsclient.RouteConfigUpdate{
		RouteConfigName: route1,
	}
	// Invoke callback with the xds client with a certain route update. Due to
	// this route update updating every route name that rds handler handles,
	// this should write to the update channel to send to the listener.
	fakeClient.InvokeWatchRouteConfigCallback(rdsUpdate, nil)
	rhuWant := map[string]xdsclient.RouteConfigUpdate{route1: rdsUpdate}
	select {
	case rhu := <-rh.updateChannel:
		if diff := cmp.Diff(rhu.rdsUpdates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
	// Close the rds handler. This is meant to be called when the lis wrapper is
	// closed, and the call should cancel all the watches present (for this
	// test, a single watch).
	rh.close()
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route1 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route1)
	}
} // Cleanup (including other files vs. the route config PR I just sent out) + Just get this build working before even trying to add new tests

// test case 1 < A

// test case 2 < A, then AB
// TestSuccessCaseTwoUpdates tests the case where the rds handler receives an update with a single Route, then receives
// a second update with two routes. The handler should start a watch for the added route, and if received a RDS update for that
// route it should send an update with both RDS updates present.
func (s) TestSuccessCaseTwoUpdates(t *testing.T) {
	rh, fakeClient := setupTests(t)

	routeNames := map[string]bool{route1: true}
	rh.updateRouteNamesToWatch(routeNames)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}

	// Update the RDSHandler with route names which adds a route name to watch.
	// This should trigger the RDSHandler to start a watch for the added route
	// name to watch.
	routeNames = map[string]bool{route1: true, route2: true}
	rh.updateRouteNamesToWatch(routeNames)
	gotRoute, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	if gotRoute != route2 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route2)
	}

	// Invoke the callback with an update for route 1. This shouldn't cause the
	// handler to write an update, as it has not received RouteConfigurations
	// for every RouteName.
	rdsUpdate1 := xdsclient.RouteConfigUpdate{
		RouteConfigName: route1,
	}
	fakeClient.InvokeWatchRouteConfigCallback(rdsUpdate1, nil)

	// The RDS Handler should not send an update.
	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()
	select {
	case <-rh.updateChannel:
		t.Fatal("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-shouldNotHappenCtx.Done():
	}

	// Invoke the callback with an update for route 2. This should cause the
	// handler to write an update, as it has received RouteConfigurations for
	// every RouteName.
	rdsUpdate2 := xdsclient.RouteConfigUpdate{
		RouteConfigName: route2,
	}
	fakeClient.InvokeWatchRouteConfigCallback(rdsUpdate2, nil)
	// The RDS Handler should then update the listener wrapper with an update
	// with two route configurations, as both route names the RDS Handler handles
	// have received an update.
	rhuWant := map[string]xdsclient.RouteConfigUpdate{route1: rdsUpdate1, route2: rdsUpdate2}
	select {
	case rhu := <-rh.updateChannel:
		if diff := cmp.Diff(rhu.rdsUpdates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the rds handler update to be written to the update buffer.")
	}

	// Close the rds handler. This is meant to be called when the lis wrapper is
	// closed, and the call should cancel all the watches present (for this
	// test, two watches on route1 and route2).
	rh.close()
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route1 && routeNameDeleted != route2 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v or %v", routeNameDeleted, route1, route2)
	}

	routeNameDeleted, err = fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route1 && routeNameDeleted != route2 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v or %v", routeNameDeleted, route1, route2)
	}
}

// test case 3 < AB, A
// TestSuccessCaseDeletedRoute tests the case where the rds handler receives an update with two routes,
// then receives an update with only
func (s) TestSuccessCaseDeletedRoute(t *testing.T) {
	rh, fakeClient := setupTests(t)

	routeNames := map[string]bool{route1: true, route2: true}
	rh.updateRouteNamesToWatch(routeNames)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Will start two watches.
	_, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	_, err = fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error %v", err)
	}

	// Update the RDSHandler with route names which deletes a route name to
	// watch. This should trigger the RDSHandler to cancel the watch for the
	// deleted route name to watch.
	routeNames = map[string]bool{route1: true}
	rh.updateRouteNamesToWatch(routeNames)
	// This should delete the watch for route2.
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error %v", err)
	}
	if routeNameDeleted != route2 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route2)
	}

	rh.close()
	_, err = fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
}

// test case 4 < AB, BC
// TestSuccessCaseTwoUpdatesAddAndDeleteRoute tests the case where the rds
// handler receives an update with two routes, and then receives an update with
// two routes, one previously there and one added (i.e. 12 -> 23). This should
// cause the route that is no longer there to be deleted and cancelled, and the
// route that was added should have a watch started for it.
/*func (s) TestSuccessCaseTwoUpdatesAddAndDeleteRoute(t *testing.T) {
	rh, fakeClient := setupTests(t)

	routeNames := map[string]bool{route1: true, route2: true}
	rh.updateRouteNamesToWatch(routeNames)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchRouteConfig(ctx)
}*/

// Perhaps still test the update buffer to test the three maps present, in case
// something goes wrong in the logic for the three maps data.

// error case
