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

	"google.golang.org/grpc/internal/testutils"
)

type ldsUpdateErr struct {
	u   ListenerUpdate
	err error
}

// TestLDSWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func (s) TestLDSWatch(t *testing.T) {
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

	ldsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchListener(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: wantUpdate})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another update, with an extra resource for a different resource name.
	client.NewListeners(map[string]ListenerUpdate{
		testLDSName:  wantUpdate,
		"randomName": {},
	})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewListeners(map[string]ListenerUpdate{testLDSName: wantUpdate})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestLDSTwoWatchSameResourceName(t *testing.T) {
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
		ldsUpdateChs    []*testutils.Channel
		cancelLastWatch func()
	)

	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		cancelLastWatch = client.WatchListener(testLDSName, func(update ListenerUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: wantUpdate})
	for i := 0; i < count; i++ {
		if err := verifyListenerUpdate(ctx, ldsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	client.NewListeners(map[string]ListenerUpdate{testLDSName: wantUpdate})
	for i := 0; i < count-1; i++ {
		if err := verifyListenerUpdate(ctx, ldsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateChs[count-1].Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestLDSThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestLDSThreeWatchDifferentResourceName(t *testing.T) {
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

	var ldsUpdateChs []*testutils.Channel
	const count = 2

	// Two watches for the same name.
	for i := 0; i < count; i++ {
		ldsUpdateCh := testutils.NewChannel()
		ldsUpdateChs = append(ldsUpdateChs, ldsUpdateCh)
		client.WatchListener(testLDSName+"1", func(update ListenerUpdate, err error) {
			ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	// Third watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName+"2", func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ListenerUpdate{RouteConfigName: testRDSName + "1"}
	wantUpdate2 := ListenerUpdate{RouteConfigName: testRDSName + "2"}
	client.NewListeners(map[string]ListenerUpdate{
		testLDSName + "1": wantUpdate1,
		testLDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if err := verifyListenerUpdate(ctx, ldsUpdateChs[i], wantUpdate1); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestLDSWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestLDSWatchAfterCache(t *testing.T) {
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

	ldsUpdateCh := testutils.NewChannel()
	client.WatchListener(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ListenerUpdate{RouteConfigName: testRDSName}
	client.NewListeners(map[string]ListenerUpdate{testLDSName: wantUpdate})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName, func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[ListenerResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receive the update.
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate); err != nil {
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

	ldsUpdateCh1 := testutils.NewChannel()
	client.WatchListener(testLDSName+"1", func(update ListenerUpdate, err error) {
		ldsUpdateCh1.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}
	// Another watch for a different name.
	ldsUpdateCh2 := testutils.NewChannel()
	client.WatchListener(testLDSName+"2", func(update ListenerUpdate, err error) {
		ldsUpdateCh2.Send(ldsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ListenerResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ListenerUpdate{RouteConfigName: testEDSName + "1"}
	wantUpdate2 := ListenerUpdate{RouteConfigName: testEDSName + "2"}
	client.NewListeners(map[string]ListenerUpdate{
		testLDSName + "1": wantUpdate1,
		testLDSName + "2": wantUpdate2,
	})
	if err := verifyListenerUpdate(ctx, ldsUpdateCh1, wantUpdate1); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// Send another update to remove resource 1.
	client.NewListeners(map[string]ListenerUpdate{testLDSName + "2": wantUpdate2})

	// Watcher 1 should get an error.
	if u, err := ldsUpdateCh1.Receive(ctx); err != nil || ErrType(u.(ldsUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected ListenerUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Watcher 2 should get the same update again.
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}

	// Send one more update without resource 1.
	client.NewListeners(map[string]ListenerUpdate{testLDSName + "2": wantUpdate2})

	// Watcher 1 should not see an update.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := ldsUpdateCh1.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ListenerUpdate: %v, want receiving from channel timeout", u)
	}

	// Watcher 2 should get the same update again.
	if err := verifyListenerUpdate(ctx, ldsUpdateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}
