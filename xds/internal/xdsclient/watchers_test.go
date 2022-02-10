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
 */

package xdsclient

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/internal/testutils"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"
)

// testClientSetup sets up the client and controller for the test. It returns a
// newly created client, and a channel where new controllers will be sent to.
func testClientSetup(t *testing.T, overrideWatchExpiryTimeout bool) (*clientImpl, *testutils.Channel) {
	t.Helper()
	ctrlCh := overrideNewController(t)

	watchExpiryTimeout := defaultWatchExpiryTimeout
	if overrideWatchExpiryTimeout {
		watchExpiryTimeout = defaultTestWatchExpiryTimeout
	}

	client, err := newWithConfig(clientOpts(), watchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(client.Close)
	return client, ctrlCh
}

// newWatch starts a new watch on the client.
func newWatch(t *testing.T, client *clientImpl, typ xdsresource.ResourceType, resourceName string) (updateCh *testutils.Channel, cancelWatch func()) {
	newWatchF, _, _ := typeToTestFuncs(typ)
	updateCh, cancelWatch = newWatchF(client, resourceName)
	t.Cleanup(cancelWatch)

	if u, ok := updateCh.ReceiveOrFail(); ok {
		t.Fatalf("received unexpected update immediately after watch: %+v", u)
	}
	return
}

// getControllerAndPubsub returns the controller and pubsub for the given
// type+resourceName from the client.
func getControllerAndPubsub(ctx context.Context, t *testing.T, client *clientImpl, ctrlCh *testutils.Channel, typ xdsresource.ResourceType, resourceName string) (*testController, pubsub.UpdateHandler) {
	c, err := ctrlCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	ctrl := c.(*testController)

	if _, err := ctrl.addWatches[typ].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	updateHandler := findPubsubForTest(t, client, xdsresource.ParseName(resourceName).Authority)

	return ctrl, updateHandler
}

// findPubsubForTest returns the pubsub for the given authority, to send updates
// to. If authority is "", the default is returned. If the authority is not
// found, the test will fail.
func findPubsubForTest(t *testing.T, c *clientImpl, authority string) pubsub.UpdateHandler {
	t.Helper()
	var config *bootstrap.ServerConfig
	if authority == "" {
		config = c.config.XDSServer
	} else {
		authConfig, ok := c.config.Authorities[authority]
		if !ok {
			t.Fatalf("failed to find authority %q", authority)
		}
		config = authConfig.XDSServer
	}
	a := c.authorities[config.String()]
	if a == nil {
		t.Fatalf("authority for %q is not created", authority)
	}
	return a.pubsub
}

var (
	newLDSWatchF = func(client *clientImpl, resourceName string) (*testutils.Channel, func()) {
		updateCh := testutils.NewChannel()
		cancelLastWatch := client.WatchListener(resourceName, func(update xdsresource.ListenerUpdate, err error) {
			updateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
		})
		return updateCh, cancelLastWatch
	}
	newLDSUpdateF = func(updateHandler pubsub.UpdateHandler, updates map[string]interface{}) {
		wantUpdates := map[string]xdsresource.ListenerUpdateErrTuple{}
		for n, u := range updates {
			wantUpdate := u.(xdsresource.ListenerUpdate)
			wantUpdates[n] = xdsresource.ListenerUpdateErrTuple{Update: wantUpdate}
		}
		updateHandler.NewListeners(wantUpdates, xdsresource.UpdateMetadata{})
	}
	verifyLDSUpdateF = func(ctx context.Context, t *testing.T, updateCh *testutils.Channel, update interface{}, err error) {
		t.Helper()
		wantUpdate := update.(xdsresource.ListenerUpdate)
		if err := verifyListenerUpdate(ctx, updateCh, wantUpdate, err); err != nil {
			t.Fatal(err)
		}
	}

	newRDSWatchF = func(client *clientImpl, resourceName string) (*testutils.Channel, func()) {
		updateCh := testutils.NewChannel()
		cancelLastWatch := client.WatchRouteConfig(resourceName, func(update xdsresource.RouteConfigUpdate, err error) {
			updateCh.Send(xdsresource.RouteConfigUpdateErrTuple{Update: update, Err: err})
		})
		return updateCh, cancelLastWatch
	}
	newRDSUpdateF = func(updateHandler pubsub.UpdateHandler, updates map[string]interface{}) {
		wantUpdates := map[string]xdsresource.RouteConfigUpdateErrTuple{}
		for n, u := range updates {
			wantUpdate := u.(xdsresource.RouteConfigUpdate)
			wantUpdates[n] = xdsresource.RouteConfigUpdateErrTuple{Update: wantUpdate}
		}
		updateHandler.NewRouteConfigs(wantUpdates, xdsresource.UpdateMetadata{})
	}
	verifyRDSUpdateF = func(ctx context.Context, t *testing.T, updateCh *testutils.Channel, update interface{}, err error) {
		t.Helper()
		wantUpdate := update.(xdsresource.RouteConfigUpdate)
		if err := verifyRouteConfigUpdate(ctx, updateCh, wantUpdate, err); err != nil {
			t.Fatal(err)
		}
	}

	newCDSWatchF = func(client *clientImpl, resourceName string) (*testutils.Channel, func()) {
		updateCh := testutils.NewChannel()
		cancelLastWatch := client.WatchCluster(resourceName, func(update xdsresource.ClusterUpdate, err error) {
			updateCh.Send(xdsresource.ClusterUpdateErrTuple{Update: update, Err: err})
		})
		return updateCh, cancelLastWatch
	}
	newCDSUpdateF = func(updateHandler pubsub.UpdateHandler, updates map[string]interface{}) {
		wantUpdates := map[string]xdsresource.ClusterUpdateErrTuple{}
		for n, u := range updates {
			wantUpdate := u.(xdsresource.ClusterUpdate)
			wantUpdates[n] = xdsresource.ClusterUpdateErrTuple{Update: wantUpdate}
		}
		updateHandler.NewClusters(wantUpdates, xdsresource.UpdateMetadata{})
	}
	verifyCDSUpdateF = func(ctx context.Context, t *testing.T, updateCh *testutils.Channel, update interface{}, err error) {
		t.Helper()
		wantUpdate := update.(xdsresource.ClusterUpdate)
		if err := verifyClusterUpdate(ctx, updateCh, wantUpdate, err); err != nil {
			t.Fatal(err)
		}
	}

	newEDSWatchF = func(client *clientImpl, resourceName string) (*testutils.Channel, func()) {
		updateCh := testutils.NewChannel()
		cancelLastWatch := client.WatchEndpoints(resourceName, func(update xdsresource.EndpointsUpdate, err error) {
			updateCh.Send(xdsresource.EndpointsUpdateErrTuple{Update: update, Err: err})
		})
		return updateCh, cancelLastWatch
	}
	newEDSUpdateF = func(updateHandler pubsub.UpdateHandler, updates map[string]interface{}) {
		wantUpdates := map[string]xdsresource.EndpointsUpdateErrTuple{}
		for n, u := range updates {
			wantUpdate := u.(xdsresource.EndpointsUpdate)
			wantUpdates[n] = xdsresource.EndpointsUpdateErrTuple{Update: wantUpdate}
		}
		updateHandler.NewEndpoints(wantUpdates, xdsresource.UpdateMetadata{})
	}
	verifyEDSUpdateF = func(ctx context.Context, t *testing.T, updateCh *testutils.Channel, update interface{}, err error) {
		t.Helper()
		wantUpdate := update.(xdsresource.EndpointsUpdate)
		if err := verifyEndpointsUpdate(ctx, updateCh, wantUpdate, err); err != nil {
			t.Fatal(err)
		}
	}
)

func typeToTestFuncs(typ xdsresource.ResourceType) (
	newWatchF func(client *clientImpl, resourceName string) (*testutils.Channel, func()),
	newUpdateF func(updateHandler pubsub.UpdateHandler, updates map[string]interface{}),
	verifyUpdateF func(ctx context.Context, t *testing.T, updateCh *testutils.Channel, update interface{}, err error),
) {
	switch typ {
	case xdsresource.ListenerResource:
		newWatchF = newLDSWatchF
		newUpdateF = newLDSUpdateF
		verifyUpdateF = verifyLDSUpdateF
	case xdsresource.RouteConfigResource:
		newWatchF = newRDSWatchF
		newUpdateF = newRDSUpdateF
		verifyUpdateF = verifyRDSUpdateF
	case xdsresource.ClusterResource:
		newWatchF = newCDSWatchF
		newUpdateF = newCDSUpdateF
		verifyUpdateF = verifyCDSUpdateF
	case xdsresource.EndpointsResource:
		newWatchF = newEDSWatchF
		newUpdateF = newEDSUpdateF
		verifyUpdateF = verifyEDSUpdateF
	}
	return
}

// TestClusterWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func testWatch(t *testing.T, typ xdsresource.ResourceType, update interface{}, resourceName string) {
	overrideFedEnvVar(t)
	for _, rName := range []string{resourceName, xdstestutils.BuildResourceName(typ, testAuthority, resourceName, nil)} {
		t.Run(rName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, cancelWatch := newWatch(t, client, typ, rName)
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, rName)
			_, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

			// Send an update, and check the result.
			newUpdateF(updateHandler, map[string]interface{}{rName: update})
			verifyUpdateF(ctx, t, updateCh, update, nil)

			// Push an update, with an extra resource for a different resource name.
			// Specify a non-nil raw proto in the original resource to ensure that the
			// new update is not considered equal to the old one.
			var newUpdate interface{}
			switch typ {
			case xdsresource.ListenerResource:
				newU := update.(xdsresource.ListenerUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.RouteConfigResource:
				newU := update.(xdsresource.RouteConfigUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.ClusterResource:
				newU := update.(xdsresource.ClusterUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.EndpointsResource:
				newU := update.(xdsresource.EndpointsUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			}
			newUpdateF(updateHandler, map[string]interface{}{rName: newUpdate})
			verifyUpdateF(ctx, t, updateCh, newUpdate, nil)

			// Cancel watch, and send update again.
			cancelWatch()
			newUpdateF(updateHandler, map[string]interface{}{rName: update})
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected update: %v, %v, want channel recv timeout", u, err)
			}
		})
	}
}

// testClusterTwoWatchSameResourceName covers the case where an update is
// received after two watch() for the same resource name.
func testTwoWatchSameResourceName(t *testing.T, typ xdsresource.ResourceType, update interface{}, resourceName string) {
	overrideFedEnvVar(t)
	for _, rName := range []string{resourceName, xdstestutils.BuildResourceName(typ, testAuthority, resourceName, nil)} {
		t.Run(rName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, _ := newWatch(t, client, typ, resourceName)
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, resourceName)
			newWatchF, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

			updateChs := []*testutils.Channel{updateCh}
			var cancelLastWatch func()
			const count = 1
			for i := 0; i < count; i++ {
				var updateCh *testutils.Channel
				updateCh, cancelLastWatch = newWatchF(client, resourceName)
				updateChs = append(updateChs, updateCh)
			}

			newUpdateF(updateHandler, map[string]interface{}{resourceName: update})
			for i := 0; i < count+1; i++ {
				verifyUpdateF(ctx, t, updateChs[i], update, nil)
			}

			// Cancel the last watch, and send update again. None of the watchers should
			// be notified because one has been cancelled, and the other is receiving
			// the same update.
			cancelLastWatch()
			newUpdateF(updateHandler, map[string]interface{}{resourceName: update})
			for i := 0; i < count+1; i++ {
				func() {
					sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
					defer sCancel()
					if u, err := updateChs[i].Receive(sCtx); err != context.DeadlineExceeded {
						t.Errorf("unexpected update: %v, %v, want channel recv timeout", u, err)
					}
				}()
			}

			// Push a new update and make sure the uncancelled watcher is invoked.
			// Specify a non-nil raw proto to ensure that the new update is not
			// considered equal to the old one.
			var newUpdate interface{}
			switch typ {
			case xdsresource.ListenerResource:
				newU := update.(xdsresource.ListenerUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.RouteConfigResource:
				newU := update.(xdsresource.RouteConfigUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.ClusterResource:
				newU := update.(xdsresource.ClusterUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.EndpointsResource:
				newU := update.(xdsresource.EndpointsUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			}
			newUpdateF(updateHandler, map[string]interface{}{resourceName: newUpdate})
			verifyUpdateF(ctx, t, updateCh, newUpdate, nil)
		})
	}
}

// testThreeWatchDifferentResourceName starts two watches for name1, and one
// watch for name2. This test verifies that two watches for name1 receive the
// same update, and name2 watch receives a different update.
func testThreeWatchDifferentResourceName(t *testing.T, typ xdsresource.ResourceType, update1 interface{}, resourceName1 string, update2 interface{}, resourceName2 string) {
	overrideFedEnvVar(t)
	for _, rName := range [][]string{
		{resourceName1, resourceName2},
		{xdstestutils.BuildResourceName(typ, testAuthority, resourceName1, nil), xdstestutils.BuildResourceName(typ, testAuthority, resourceName2, nil)},
	} {
		t.Run(rName[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, _ := newWatch(t, client, typ, rName[0])
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, rName[0])
			newWatchF, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

			// Two watches for the same name.
			updateChs := []*testutils.Channel{updateCh}
			const count = 1
			for i := 0; i < count; i++ {
				var updateCh *testutils.Channel
				updateCh, _ = newWatchF(client, rName[0])
				updateChs = append(updateChs, updateCh)
			}
			// Third watch for a different name.
			updateCh2, _ := newWatchF(client, rName[1])

			newUpdateF(updateHandler, map[string]interface{}{
				rName[0]: update1,
				rName[1]: update2,
			})

			// The first several watches for the same resource should all
			// receive the first update.
			for i := 0; i < count+1; i++ {
				verifyUpdateF(ctx, t, updateChs[i], update1, nil)
			}
			// The last watch for the different resource should receive the
			// second update.
			verifyUpdateF(ctx, t, updateCh2, update2, nil)
		})
	}
}

// testWatchAfterCache covers the case where watch is called after the update is
// in cache.
func testWatchAfterCache(t *testing.T, typ xdsresource.ResourceType, update interface{}, resourceName string) {
	overrideFedEnvVar(t)
	for _, rName := range []string{resourceName, xdstestutils.BuildResourceName(typ, testAuthority, resourceName, nil)} {
		t.Run(rName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, _ := newWatch(t, client, typ, rName)
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, rName)
			newWatchF, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

			newUpdateF(updateHandler, map[string]interface{}{rName: update})
			verifyUpdateF(ctx, t, updateCh, update, nil)

			// Another watch for the resource in cache.
			updateCh2, _ := newWatchF(client, rName)

			// New watch should receive the update.
			verifyUpdateF(ctx, t, updateCh2, update, nil)

			// Old watch should see nothing.
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected update: %v, %v, want channel recv timeout", u, err)
			}
		})
	}
}

// testResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func testResourceRemoved(t *testing.T, typ xdsresource.ResourceType, update1 interface{}, resourceName1 string, update2 interface{}, resourceName2 string) {
	overrideFedEnvVar(t)
	for _, rName := range [][]string{
		{resourceName1, resourceName2},
		{xdstestutils.BuildResourceName(typ, testAuthority, resourceName1, nil), xdstestutils.BuildResourceName(typ, testAuthority, resourceName2, nil)},
	} {
		t.Run(rName[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, _ := newWatch(t, client, typ, rName[0])
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, rName[0])
			newWatchF, newUpdateF, verifyUpdateF := typeToTestFuncs(typ)

			// Another watch for a different name.
			updateCh2, _ := newWatchF(client, rName[1])

			newUpdateF(updateHandler, map[string]interface{}{
				rName[0]: update1,
				rName[1]: update2,
			})
			verifyUpdateF(ctx, t, updateCh, update1, nil)
			verifyUpdateF(ctx, t, updateCh2, update2, nil)

			// Send another update to remove resource 1.
			newUpdateF(updateHandler, map[string]interface{}{
				rName[1]: update2,
			})

			// Watcher 1 should get an error.
			if u, err := updateCh.Receive(ctx); err != nil {
				t.Errorf("failed to receive update: %v", err)
			} else {
				var gotErr error
				switch typ {
				case xdsresource.ListenerResource:
					newU := u.(xdsresource.ListenerUpdateErrTuple)
					gotErr = newU.Err
				case xdsresource.RouteConfigResource:
					newU := u.(xdsresource.RouteConfigUpdateErrTuple)
					gotErr = newU.Err
				case xdsresource.ClusterResource:
					newU := u.(xdsresource.ClusterUpdateErrTuple)
					gotErr = newU.Err
				case xdsresource.EndpointsResource:
					newU := u.(xdsresource.EndpointsUpdateErrTuple)
					gotErr = newU.Err
				}
				if xdsresource.ErrType(gotErr) != xdsresource.ErrorTypeResourceNotFound {
					t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
				}
			}

			// Watcher 2 should not see an update since the resource has not changed.
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := updateCh2.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected ClusterUpdate: %v, want receiving from channel timeout", u)
			}

			// Send another update with resource 2 modified. Specify a non-nil raw proto
			// to ensure that the new update is not considered equal to the old one.
			var newUpdate interface{}
			switch typ {
			case xdsresource.ListenerResource:
				newU := update2.(xdsresource.ListenerUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.RouteConfigResource:
				newU := update2.(xdsresource.RouteConfigUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.ClusterResource:
				newU := update2.(xdsresource.ClusterUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			case xdsresource.EndpointsResource:
				newU := update2.(xdsresource.EndpointsUpdate)
				newU.Raw = &anypb.Any{}
				newUpdate = newU
			}
			newUpdateF(updateHandler, map[string]interface{}{
				rName[1]: newUpdate,
			})

			// Watcher 1 should not see an update.
			sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := updateCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected Cluster: %v, want receiving from channel timeout", u)
			}

			// Watcher 2 should get the update.
			verifyUpdateF(ctx, t, updateCh2, newUpdate, nil)
		})
	}
}

// testWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalid resources should receive an error.
func testWatchPartialValid(t *testing.T, typ xdsresource.ResourceType, update interface{}, resourceName string) {
	overrideFedEnvVar(t)
	const badResourceName = "bad-resource"

	for _, rName := range [][]string{
		{resourceName, badResourceName},
		{xdstestutils.BuildResourceName(typ, testAuthority, resourceName, nil), xdstestutils.BuildResourceName(typ, testAuthority, badResourceName, nil)},
	} {
		t.Run(rName[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client, ctrlCh := testClientSetup(t, false)
			updateCh, _ := newWatch(t, client, typ, rName[0])
			_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, typ, rName[0])
			newWatchF, _, verifyUpdateF := typeToTestFuncs(typ)

			updateChs := map[string]*testutils.Channel{
				rName[0]: updateCh,
			}

			for _, name := range []string{rName[1]} {
				updateChT, _ := newWatchF(client, rName[1])
				updateChs[name] = updateChT
			}

			wantError := fmt.Errorf("testing error")
			wantError2 := fmt.Errorf("individual error")

			switch typ {
			case xdsresource.ListenerResource:
				updateHandler.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{
					rName[0]: {Update: update.(xdsresource.ListenerUpdate)},
					rName[1]: {Err: wantError2},
				}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
			case xdsresource.RouteConfigResource:
				updateHandler.NewRouteConfigs(map[string]xdsresource.RouteConfigUpdateErrTuple{
					rName[0]: {Update: update.(xdsresource.RouteConfigUpdate)},
					rName[1]: {Err: wantError2},
				}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
			case xdsresource.ClusterResource:
				updateHandler.NewClusters(map[string]xdsresource.ClusterUpdateErrTuple{
					rName[0]: {Update: update.(xdsresource.ClusterUpdate)},
					rName[1]: {Err: wantError2},
				}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
			case xdsresource.EndpointsResource:
				updateHandler.NewEndpoints(map[string]xdsresource.EndpointsUpdateErrTuple{
					rName[0]: {Update: update.(xdsresource.EndpointsUpdate)},
					rName[1]: {Err: wantError2},
				}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
			}

			// The valid resource should be sent to the watcher.
			verifyUpdateF(ctx, t, updateChs[rName[0]], update, nil)

			// The failed watcher should receive an error.
			verifyUpdateF(ctx, t, updateChs[rName[1]], update, wantError2)
		})
	}
}
