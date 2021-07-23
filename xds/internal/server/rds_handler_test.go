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
