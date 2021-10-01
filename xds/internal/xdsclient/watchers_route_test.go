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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc/internal/testutils"
)

// TestRDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an update is received after cancel()
func (s) TestRDSWatch(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	rdsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchRouteConfig(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: wantUpdate}}, UpdateMetadata{})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Push an update, with an extra resource for a different resource name.
	// Specify a non-nil raw proto in the original resource to ensure that the
	// new update is not considered equal to the old one.
	newUpdate := wantUpdate
	newUpdate.Raw = &anypb.Any{}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{
		testRDSName:  {Update: newUpdate},
		"randomName": {},
	}, UpdateMetadata{})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, newUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: wantUpdate}}, UpdateMetadata{})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := rdsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestRDSTwoWatchSameResourceName(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	const count = 2
	var (
		rdsUpdateChs    []*testutils.Channel
		cancelLastWatch func()
	)
	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		cancelLastWatch = client.WatchRouteConfig(testRDSName, func(update RouteConfigUpdate, err error) {
			rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	wantUpdate := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again. None of the watchers should
	// be notified because one has been cancelled, and the other is receiving
	// the same update.
	cancelLastWatch()
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		func() {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := rdsUpdateChs[i].Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
			}
		}()
	}

	// Push a new update and make sure the uncancelled watcher is invoked.
	// Specify a non-nil raw proto to ensure that the new update is not
	// considered equal to the old one.
	newUpdate := wantUpdate
	newUpdate.Raw = &anypb.Any{}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: newUpdate}}, UpdateMetadata{})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[0], newUpdate, nil); err != nil {
		t.Fatal(err)
	}
}

// TestRDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestRDSThreeWatchDifferentResourceName(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	// Two watches for the same name.
	var rdsUpdateChs []*testutils.Channel
	const count = 2
	for i := 0; i < count; i++ {
		rdsUpdateCh := testutils.NewChannel()
		rdsUpdateChs = append(rdsUpdateChs, rdsUpdateCh)
		client.WatchRouteConfig(testRDSName+"1", func(update RouteConfigUpdate, err error) {
			rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	// Third watch for a different name.
	rdsUpdateCh2 := testutils.NewChannel()
	client.WatchRouteConfig(testRDSName+"2", func(update RouteConfigUpdate, err error) {
		rdsUpdateCh2.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName + "1": {Weight: 1}}}},
			},
		},
	}
	wantUpdate2 := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName + "2": {Weight: 1}}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{
		testRDSName + "1": {Update: wantUpdate1},
		testRDSName + "2": {Update: wantUpdate2},
	}, UpdateMetadata{})

	for i := 0; i < count; i++ {
		if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[i], wantUpdate1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

// TestRDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestRDSWatchAfterCache(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	rdsUpdateCh := testutils.NewChannel()
	client.WatchRouteConfig(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName: {Weight: 1}}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testRDSName: {Update: wantUpdate}}, UpdateMetadata{})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	rdsUpdateCh2 := testutils.NewChannel()
	client.WatchRouteConfig(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh2.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[RouteConfigResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := rdsUpdateCh2.Receive(ctx); err != nil || !cmp.Equal(u, RouteConfigUpdateErrTuple{wantUpdate, nil}, cmp.AllowUnexported(RouteConfigUpdateErrTuple{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := rdsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestRouteWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestRouteWatchNACKError(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	rdsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchRouteConfig(testCDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
	})
	defer cancelWatch()
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantError := fmt.Errorf("testing error")
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{testCDSName: {Err: wantError}}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, RouteConfigUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestRouteWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestRouteWatchPartialValid(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
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
	apiClient := c.(*testAPIClient)

	const badResourceName = "bad-resource"
	updateChs := make(map[string]*testutils.Channel)

	for _, name := range []string{testRDSName, badResourceName} {
		rdsUpdateCh := testutils.NewChannel()
		cancelWatch := client.WatchRouteConfig(name, func(update RouteConfigUpdate, err error) {
			rdsUpdateCh.Send(RouteConfigUpdateErrTuple{Update: update, Err: err})
		})
		defer func() {
			cancelWatch()
			if _, err := apiClient.removeWatches[RouteConfigResource].Receive(ctx); err != nil {
				t.Fatalf("want watch to be canceled, got err: %v", err)
			}
		}()
		if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		updateChs[name] = rdsUpdateCh
	}

	wantError := fmt.Errorf("testing error")
	wantError2 := fmt.Errorf("individual error")
	client.NewRouteConfigs(map[string]RouteConfigUpdateErrTuple{
		testRDSName: {Update: RouteConfigUpdate{VirtualHosts: []*VirtualHost{{
			Domains: []string{testLDSName},
			Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName: {Weight: 1}}}},
		}}}},
		badResourceName: {Err: wantError2},
	}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})

	// The valid resource should be sent to the watcher.
	if err := verifyRouteConfigUpdate(ctx, updateChs[testRDSName], RouteConfigUpdate{VirtualHosts: []*VirtualHost{{
		Domains: []string{testLDSName},
		Routes:  []*Route{{Prefix: newStringP(""), WeightedClusters: map[string]WeightedCluster{testCDSName: {Weight: 1}}}},
	}}}, nil); err != nil {
		t.Fatal(err)
	}

	// The failed watcher should receive an error.
	if err := verifyRouteConfigUpdate(ctx, updateChs[badResourceName], RouteConfigUpdate{}, wantError2); err != nil {
		t.Fatal(err)
	}
}
