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

package client

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/internal/testutils"
)

type rdsUpdateErr struct {
	u   RouteConfigUpdate
	err error
}

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
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{testRDSName: wantUpdate})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another update for a different resource name.
	client.NewRouteConfigs(map[string]RouteConfigUpdate{"randomName": {}})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := rdsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewRouteConfigs(map[string]RouteConfigUpdate{testRDSName: wantUpdate})
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
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
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
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
				Routes:  []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{testRDSName: wantUpdate})
	for i := 0; i < count; i++ {
		if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	client.NewRouteConfigs(map[string]RouteConfigUpdate{testRDSName: wantUpdate})
	for i := 0; i < count-1; i++ {
		if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := rdsUpdateChs[count-1].Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
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
			rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
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
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "1": 1}}},
			},
		},
	}
	wantUpdate2 := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName + "2": 1}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{
		testRDSName + "1": wantUpdate1,
		testRDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if err := verifyRouteConfigUpdate(ctx, rdsUpdateChs[i], wantUpdate1); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh2, wantUpdate2); err != nil {
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
		rdsUpdateCh.Send(rdsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[RouteConfigResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{
			{
				Domains: []string{testLDSName},
				Routes:  []*Route{{Prefix: newStringP(""), Action: map[string]uint32{testCDSName: 1}}},
			},
		},
	}
	client.NewRouteConfigs(map[string]RouteConfigUpdate{testRDSName: wantUpdate})
	if err := verifyRouteConfigUpdate(ctx, rdsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	rdsUpdateCh2 := testutils.NewChannel()
	client.WatchRouteConfig(testRDSName, func(update RouteConfigUpdate, err error) {
		rdsUpdateCh2.Send(rdsUpdateErr{u: update, err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[RouteConfigResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if u, err := rdsUpdateCh2.Receive(ctx); err != nil || !cmp.Equal(u, rdsUpdateErr{wantUpdate, nil}, cmp.AllowUnexported(rdsUpdateErr{})) {
		t.Errorf("unexpected RouteConfigUpdate: %v, error receiving from channel: %v", u, err)
	}

	// Old watch should see nothing.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := rdsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected RouteConfigUpdate: %v, %v, want channel recv timeout", u, err)
	}
}
