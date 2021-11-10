/*
 *
 * Copyright 2020 gRPC authors.
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

package xdsclient

import (
	"context"
	"fmt"
	"testing"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestLDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func (s) TestLDSWatch(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	ldsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := xdsresource.ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Push an update, with an extra resource for a different resource name.
	// Specify a non-nil raw proto in the original resource to ensure that the
	// new update is not considered equal to the old one.
	newUpdate := xdsresource.ListenerUpdate{RouteConfigName: testRDSName, Raw: &anypb.Any{}}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{
		testLDSName:  {Update: newUpdate},
		"randomName": {},
	}, xdsresource.UpdateMetadata{})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, newUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestLDSTwoWatchSameResourceName(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	const count = 2
	var (
		ldsUpdateChs    []*testutils.Channel
		cancelLastWatch func()
	)

	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		cancelLastWatch = client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
			ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	wantUpdate := xdsresource.ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	for i := 0; i < count; i++ {
		if err := verifyListenerUpdate(ctx, ldsUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again. None of the watchers should
	// be notified because one has been cancelled, and the other is receiving
	// the same update.
	cancelLastWatch()
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	for i := 0; i < count; i++ {
		func() {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := ldsUpdateChs[i].Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
			}
		}()
	}

	// Push a new update and make sure the uncancelled watcher is invoked.
	// Specify a non-nil raw proto to ensure that the new update is not
	// considered equal to the old one.
	newUpdate := xdsresource.ListenerUpdate{RouteConfigName: testRDSName, Raw: &anypb.Any{}}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: newUpdate}}, xdsresource.UpdateMetadata{})
	if err := verifyListenerUpdate(ctx, ldsUpdateChs[0], newUpdate, nil); err != nil {
		t.Fatal(err)
	}
}

// TestLDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestLDSThreeWatchDifferentResourceName(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	var ldsUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		client.WatchListener(testLDSName+"1", func(update xdsresource.ListenerUpdate, err error) {
			ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	// Third watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName+"2", func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "1"}
	wantUpdate2 := xdsresource.ListenerUpdate{RouteConfigName: testRDSName + "2"}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{
		testLDSName + "1": {Update: wantUpdate1},
		testLDSName + "2": {Update: wantUpdate2},
	}, xdsresource.UpdateMetadata{})

	for i := 0; i < count; i++ {
		if err := verifyListenerUpdate(ctx, ldsUpdateChs[i], wantUpdate1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestLDSWatchAfterCache(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	ldsUpdateCh := testutils.NewChannel()
	client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := xdsresource.ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: wantUpdate}}, xdsresource.UpdateMetadata{})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receive the update.
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Old watch should see nothing.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestLDSResourceRemoved(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	ldsUpdateCh1 := testutils.NewChannel()
	client.WatchListener(testLDSName+"1", func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh1.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	// Another watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName+"2", func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh2.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := xdsresource.ListenerUpdate{RouteConfigName: testEDSName + "1"}
	wantUpdate2 := xdsresource.ListenerUpdate{RouteConfigName: testEDSName + "2"}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{
		testLDSName + "1": {Update: wantUpdate1},
		testLDSName + "2": {Update: wantUpdate2},
	}, xdsresource.UpdateMetadata{})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh1, wantUpdate1, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}

	// Send another update to remove resource 1.
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName + "2": {Update: wantUpdate2}}, xdsresource.UpdateMetadata{})

	// Watcher 1 should get an error.
	if u, err := ldsUpdateCh1.Receive(ctx); err != nil || xdsresource.ErrType(u.(xdsresource.ListenerUpdateErrTuple).Err) != xdsresource.ErrorTypeResourceNotFound {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Watcher 2 should not see an update since the resource has not changed.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh2.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
	}

	// Send another update with resource 2 modified. Specify a non-nil raw proto
	// to ensure that the new update is not considered equal to the old one.
	wantUpdate2 = xdsresource.ListenerUpdate{RouteConfigName: testEDSName + "2", Raw: &anypb.Any{}}
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName + "2": {Update: wantUpdate2}}, xdsresource.UpdateMetadata{})

	// Watcher 1 should not see an update.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh1.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
	}

	// Watcher 2 should get the update.
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

// TestListenerWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestListenerWatchNACKError(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	ldsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	defer cancelWatch()
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantError := fmt.Errorf("testing error")
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Err: wantError}}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, xdsresource.ListenerUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestListenerWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestListenerWatchPartialValid(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	const badResourceName = "bad-resource"
	updateChs := make(map[string]*testutils.Channel)

	for _, name := range []string{testLDSName, badResourceName} {
		ldsUpdateCh := testutils.NewChannel()
		cancelWatch := client.WatchListener(name, func(update xdsresource.ListenerUpdate, err error) {
			ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
		})
		defer func() {
			cancelWatch()
			if _, err := apiClient.removeWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
				t.Fatalf("want watch to be canceled, got err: %v", err)
			}
		}()
		if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		updateChs[name] = ldsUpdateCh
	}

	wantError := fmt.Errorf("testing error")
	wantError2 := fmt.Errorf("individual error")
	client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{
		testLDSName:     {Update: xdsresource.ListenerUpdate{RouteConfigName: testEDSName}},
		badResourceName: {Err: wantError2},
	}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})

	// The valid resource should be sent to the watcher.
	if err := verifyListenerUpdate(ctx, updateChs[testLDSName], xdsresource.ListenerUpdate{RouteConfigName: testEDSName}, nil); err != nil {
		t.Fatal(err)
	}

	// The failed watcher should receive an error.
	if err := verifyListenerUpdate(ctx, updateChs[badResourceName], xdsresource.ListenerUpdate{}, wantError2); err != nil {
		t.Fatal(err)
	}
}

// TestListenerWatch_RedundantUpdateSupression tests scenarios where an update
// with an unmodified resource is suppressed, and modified resource is not.
func (s) TestListenerWatch_RedundantUpdateSupression(t *testing.T) {
	apiClientCh, cleanup := overrideNewController()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, false))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	c, err := apiClientCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for API client to be created: %v", err)
	}
	apiClient := c.(*testController)

	ldsUpdateCh := testutils.NewChannel()
	client.WatchListener(testLDSName, func(update xdsresource.ListenerUpdate, err error) {
		ldsUpdateCh.Send(xdsresource.ListenerUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[xdsresource.ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	basicListener := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
			}),
		},
	})
	listenerWithFilter1 := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: "customFilter1",
						ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: &anypb.Any{
							TypeUrl: "custom.filter",
							Value:   []byte{1, 2, 3},
						}},
					},
				},
			}),
		},
	})
	listenerWithFilter2 := testutils.MarshalAny(&v3listenerpb.Listener{
		Name: testLDSName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
				RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
					Rds: &v3httppb.Rds{RouteConfigName: "route-config-name"},
				},
				HttpFilters: []*v3httppb.HttpFilter{
					{
						Name: "customFilter2",
						ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: &anypb.Any{
							TypeUrl: "custom.filter",
							Value:   []byte{1, 2, 3},
						}},
					},
				},
			}),
		},
	})

	tests := []struct {
		update       xdsresource.ListenerUpdate
		wantCallback bool
	}{
		{
			// First update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: basicListener},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: basicListener},
			wantCallback: false,
		},
		{
			// New update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter1},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter1},
			wantCallback: false,
		},
		{
			// New update. Callback should be invoked.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter2},
			wantCallback: true,
		},
		{
			// Same update as previous. Callback should be skipped.
			update:       xdsresource.ListenerUpdate{Raw: listenerWithFilter2},
			wantCallback: false,
		},
	}
	for _, test := range tests {
		client.NewListeners(map[string]xdsresource.ListenerUpdateErrTuple{testLDSName: {Update: test.update}}, xdsresource.UpdateMetadata{})
		if test.wantCallback {
			if err := verifyListenerUpdate(ctx, ldsUpdateCh, test.update, nil); err != nil {
				t.Fatal(err)
			}
		} else {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := ldsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
			}
		}
	}
}
