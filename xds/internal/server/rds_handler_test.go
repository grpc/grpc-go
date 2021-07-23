package server

import (
	"context"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"testing"
)

var (
	route1 = "route1"
)

// setupTests creates a rds handler with a fake xds client for control over the
// xdsclient. It also starts the rdsHandler with the certain routeNamesToWatch
// (in practice, this will come from the Filter Chain Manager).
func setupTests(t *testing.T) (*rdsHandler, *fakeclient.Client) {
	xdsC := fakeclient.NewClient()
	rh := newRdsHandler(&listenerWrapper{xdsC: xdsC}) // Will fake the listener component
	return rh, xdsC
}

// Simplest test: the rds handler receives a route name of length 1, starts a watch, gets a successful update
// and then writes an update to update channel.
func (s) TestSuccessCaseOneRDSWatch(t *testing.T) {
	rh, fakeClient := setupTests(t)
	// When you first update the rds handler with a list of a single Route names that needs dynamic RDS Configuration,
	// this Route name has not been seen before, so the RDS Handler should start a watch on that RouteName.
	//
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
		RouteConfigName: "route1",
	}
	// Invoke callback with the xds client with a certain route update. Due to this route update
	// updating every route name that rds handler handles, this should write to the update channel
	// to send to the listener.
	fakeClient.InvokeWatchRouteConfigCallback(rdsUpdate, nil)
	select {
	case rhu := <-rh.updateChannel:

	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
	// Close the rds handler. This is meant to be called when the lis wrapper is closed, and the call
	// should cancel all the watches present (for this test, a single watch).
	rh.close()
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNmae
}

// I need to add close() to rdsHandler to clean up and close all the watches
