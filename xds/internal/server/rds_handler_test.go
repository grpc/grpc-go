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
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const (
	route1 = "route1"
	route2 = "route2"
	route3 = "route3"
)

// setupTests creates a rds handler with a fake xds client for control over the
// xds client.
func setupTests() (*rdsHandler, *fakeclient.Client, chan rdsHandlerUpdate) {
	xdsC := fakeclient.NewClient()
	ch := make(chan rdsHandlerUpdate, 1)
	rh := newRDSHandler(xdsC, ch)
	return rh, xdsC, ch
}

// waitForFuncWithNames makes sure that a blocking function returns the correct
// set of names, where order doesn't matter. This takes away nondeterminism from
// ranging through a map.
func waitForFuncWithNames(ctx context.Context, f func(context.Context) (string, error), names ...string) error {
	wantNames := make(map[string]bool, len(names))
	for _, name := range names {
		wantNames[name] = true
	}
	gotNames := make(map[string]bool, len(names))
	for range wantNames {
		name, err := f(ctx)
		if err != nil {
			return err
		}
		gotNames[name] = true
	}
	if !cmp.Equal(gotNames, wantNames) {
		return fmt.Errorf("got routeNames %v, want %v", gotNames, wantNames)
	}
	return nil
}

// TestSuccessCaseOneRDSWatch tests the simplest scenario: the rds handler
// receives a single route name, starts a watch for that route name, gets a
// successful update, and then writes an update to the update channel for
// listener to pick up.
func (s) TestSuccessCaseOneRDSWatch(t *testing.T) {
	rh, fakeClient, ch := setupTests()
	// When you first update the rds handler with a list of a single Route names
	// that needs dynamic RDS Configuration, this Route name has not been seen
	// before, so the RDS Handler should start a watch on that RouteName.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})
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
	rdsUpdate := xdsresource.RouteConfigUpdate{}
	// Invoke callback with the xds client with a certain route update. Due to
	// this route update updating every route name that rds handler handles,
	// this should write to the update channel to send to the listener.
	fakeClient.InvokeWatchRouteConfigCallback(route1, rdsUpdate, nil)
	rhuWant := map[string]xdsresource.RouteConfigUpdate{route1: rdsUpdate}
	select {
	case rhu := <-ch:
		if diff := cmp.Diff(rhu.updates, rhuWant); diff != "" {
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
}

// TestSuccessCaseTwoUpdates tests the case where the rds handler receives an
// update with a single Route, then receives a second update with two routes.
// The handler should start a watch for the added route, and if received a RDS
// update for that route it should send an update with both RDS updates present.
func (s) TestSuccessCaseTwoUpdates(t *testing.T) {
	rh, fakeClient, ch := setupTests()

	rh.updateRouteNamesToWatch(map[string]bool{route1: true})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotRoute, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	if gotRoute != route1 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route1)
	}

	// Update the RDSHandler with route names which adds a route name to watch.
	// This should trigger the RDSHandler to start a watch for the added route
	// name to watch.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})
	gotRoute, err = fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	if gotRoute != route2 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route2)
	}

	// Invoke the callback with an update for route 1. This shouldn't cause the
	// handler to write an update, as it has not received RouteConfigurations
	// for every RouteName.
	rdsUpdate1 := xdsresource.RouteConfigUpdate{}
	fakeClient.InvokeWatchRouteConfigCallback(route1, rdsUpdate1, nil)

	// The RDS Handler should not send an update.
	sCtx, sCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-ch:
		t.Fatal("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-sCtx.Done():
	}

	// Invoke the callback with an update for route 2. This should cause the
	// handler to write an update, as it has received RouteConfigurations for
	// every RouteName.
	rdsUpdate2 := xdsresource.RouteConfigUpdate{}
	fakeClient.InvokeWatchRouteConfigCallback(route2, rdsUpdate2, nil)
	// The RDS Handler should then update the listener wrapper with an update
	// with two route configurations, as both route names the RDS Handler handles
	// have received an update.
	rhuWant := map[string]xdsresource.RouteConfigUpdate{route1: rdsUpdate1, route2: rdsUpdate2}
	select {
	case rhu := <-ch:
		if diff := cmp.Diff(rhu.updates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the rds handler update to be written to the update buffer.")
	}

	// Close the rds handler. This is meant to be called when the lis wrapper is
	// closed, and the call should cancel all the watches present (for this
	// test, two watches on route1 and route2).
	rh.close()
	if err = waitForFuncWithNames(ctx, fakeClient.WaitForCancelRouteConfigWatch, route1, route2); err != nil {
		t.Fatalf("Error while waiting for names: %v", err)
	}
}

// TestSuccessCaseDeletedRoute tests the case where the rds handler receives an
// update with two routes, then receives an update with only one route. The RDS
// Handler is expected to cancel the watch for the route no longer present.
func (s) TestSuccessCaseDeletedRoute(t *testing.T) {
	rh, fakeClient, ch := setupTests()

	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Will start two watches.
	if err := waitForFuncWithNames(ctx, fakeClient.WaitForWatchRouteConfig, route1, route2); err != nil {
		t.Fatalf("Error while waiting for names: %v", err)
	}

	// Update the RDSHandler with route names which deletes a route name to
	// watch. This should trigger the RDSHandler to cancel the watch for the
	// deleted route name to watch.
	rh.updateRouteNamesToWatch(map[string]bool{route1: true})
	// This should delete the watch for route2.
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error %v", err)
	}
	if routeNameDeleted != route2 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route2)
	}

	rdsUpdate := xdsresource.RouteConfigUpdate{}
	// Invoke callback with the xds client with a certain route update. Due to
	// this route update updating every route name that rds handler handles,
	// this should write to the update channel to send to the listener.
	fakeClient.InvokeWatchRouteConfigCallback(route1, rdsUpdate, nil)
	rhuWant := map[string]xdsresource.RouteConfigUpdate{route1: rdsUpdate}
	select {
	case rhu := <-ch:
		if diff := cmp.Diff(rhu.updates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}

	rh.close()
	routeNameDeleted, err = fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route1 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route1)
	}
}

// TestSuccessCaseTwoUpdatesAddAndDeleteRoute tests the case where the rds
// handler receives an update with two routes, and then receives an update with
// two routes, one previously there and one added (i.e. 12 -> 23). This should
// cause the route that is no longer there to be deleted and cancelled, and the
// route that was added should have a watch started for it.
func (s) TestSuccessCaseTwoUpdatesAddAndDeleteRoute(t *testing.T) {
	rh, fakeClient, ch := setupTests()

	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := waitForFuncWithNames(ctx, fakeClient.WaitForWatchRouteConfig, route1, route2); err != nil {
		t.Fatalf("Error while waiting for names: %v", err)
	}

	// Update the rds handler with two routes, one which was already there and a new route.
	// This should cause the rds handler to delete/cancel watch for route 1 and start a watch
	// for route 3.
	rh.updateRouteNamesToWatch(map[string]bool{route2: true, route3: true})

	// Start watch comes first, which should be for route3 as was just added.
	gotRoute, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error: %v", err)
	}
	if gotRoute != route3 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route3)
	}

	// Then route 1 should be deleted/cancelled watch for, as it is no longer present
	// in the new RouteName to watch map.
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route1 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route1)
	}

	// Invoke the callback with an update for route 2. This shouldn't cause the
	// handler to write an update, as it has not received RouteConfigurations
	// for every RouteName.
	rdsUpdate2 := xdsresource.RouteConfigUpdate{}
	fakeClient.InvokeWatchRouteConfigCallback(route2, rdsUpdate2, nil)

	// The RDS Handler should not send an update.
	sCtx, sCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-ch:
		t.Fatalf("RDS Handler wrote an update to updateChannel when it shouldn't have, as each route name has not received an update yet")
	case <-sCtx.Done():
	}

	// Invoke the callback with an update for route 3. This should cause the
	// handler to write an update, as it has received RouteConfigurations for
	// every RouteName.
	rdsUpdate3 := xdsresource.RouteConfigUpdate{}
	fakeClient.InvokeWatchRouteConfigCallback(route3, rdsUpdate3, nil)
	// The RDS Handler should then update the listener wrapper with an update
	// with two route configurations, as both route names the RDS Handler handles
	// have received an update.
	rhuWant := map[string]xdsresource.RouteConfigUpdate{route2: rdsUpdate2, route3: rdsUpdate3}
	select {
	case rhu := <-rh.updateChannel:
		if diff := cmp.Diff(rhu.updates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the rds handler update to be written to the update buffer.")
	}
	// Close the rds handler. This is meant to be called when the lis wrapper is
	// closed, and the call should cancel all the watches present (for this
	// test, two watches on route2 and route3).
	rh.close()
	if err = waitForFuncWithNames(ctx, fakeClient.WaitForCancelRouteConfigWatch, route2, route3); err != nil {
		t.Fatalf("Error while waiting for names: %v", err)
	}
}

// TestSuccessCaseSecondUpdateMakesRouteFull tests the scenario where the rds handler gets
// told to watch three rds configurations, gets two successful updates, then gets told to watch
// only those two. The rds handler should then write an update to update buffer.
func (s) TestSuccessCaseSecondUpdateMakesRouteFull(t *testing.T) {
	rh, fakeClient, ch := setupTests()

	rh.updateRouteNamesToWatch(map[string]bool{route1: true, route2: true, route3: true})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := waitForFuncWithNames(ctx, fakeClient.WaitForWatchRouteConfig, route1, route2, route3); err != nil {
		t.Fatalf("Error while waiting for names: %v", err)
	}

	// Invoke the callbacks for two of the three watches. Since RDS is not full,
	// this shouldn't trigger rds handler to write an update to update buffer.
	fakeClient.InvokeWatchRouteConfigCallback(route1, xdsresource.RouteConfigUpdate{}, nil)
	fakeClient.InvokeWatchRouteConfigCallback(route2, xdsresource.RouteConfigUpdate{}, nil)

	// The RDS Handler should not send an update.
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
	// Route 3 should be deleted/cancelled watch for, as it is no longer present
	// in the new RouteName to watch map.
	routeNameDeleted, err := fakeClient.WaitForCancelRouteConfigWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelRDS failed with error: %v", err)
	}
	if routeNameDeleted != route3 {
		t.Fatalf("xdsClient.CancelRDS called for route %v, want %v", routeNameDeleted, route1)
	}
	rhuWant := map[string]xdsresource.RouteConfigUpdate{route1: {}, route2: {}}
	select {
	case rhu := <-ch:
		if diff := cmp.Diff(rhu.updates, rhuWant); diff != "" {
			t.Fatalf("got unexpected route update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the rds handler update to be written to the update buffer.")
	}
}

// TestErrorReceived tests the case where the rds handler receives a route name
// to watch, then receives an update with an error. This error should be then
// written to the update channel.
func (s) TestErrorReceived(t *testing.T) {
	rh, fakeClient, ch := setupTests()

	rh.updateRouteNamesToWatch(map[string]bool{route1: true})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotRoute, err := fakeClient.WaitForWatchRouteConfig(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchRDS failed with error %v", err)
	}
	if gotRoute != route1 {
		t.Fatalf("xdsClient.WatchRDS called for route: %v, want %v", gotRoute, route1)
	}

	rdsErr := errors.New("some error")
	fakeClient.InvokeWatchRouteConfigCallback(route1, xdsresource.RouteConfigUpdate{}, rdsErr)
	select {
	case rhu := <-ch:
		if rhu.err.Error() != "some error" {
			t.Fatalf("Did not receive the expected error, instead received: %v", rhu.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel")
	}
}
