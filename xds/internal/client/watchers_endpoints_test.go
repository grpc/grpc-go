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
	"google.golang.org/grpc/xds/internal"
)

var (
	testLocalities = []Locality{
		{
			Endpoints: []Endpoint{{Address: "addr1:314"}},
			ID:        internal.LocalityID{SubZone: "locality-1"},
			Priority:  1,
			Weight:    1,
		},
		{
			Endpoints: []Endpoint{{Address: "addr2:159"}},
			ID:        internal.LocalityID{SubZone: "locality-2"},
			Priority:  0,
			Weight:    1,
		},
	}
)

type endpointsUpdateErr struct {
	u   EndpointsUpdate
	err error
}

// TestEndpointsWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name (which doesn't trigger callback)
// - an update is received after cancel()
func (s) TestEndpointsWatch(t *testing.T) {
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

	endpointsUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	client.NewEndpoints(map[string]EndpointsUpdate{testCDSName: wantUpdate})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another update for a different resource name.
	client.NewEndpoints(map[string]EndpointsUpdate{"randomName": {}})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := endpointsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewEndpoints(map[string]EndpointsUpdate{testCDSName: wantUpdate})
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := endpointsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestEndpointsTwoWatchSameResourceName(t *testing.T) {
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
		endpointsUpdateChs []*testutils.Channel
		cancelLastWatch    func()
	)
	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		cancelLastWatch = client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	client.NewEndpoints(map[string]EndpointsUpdate{testCDSName: wantUpdate})
	for i := 0; i < count; i++ {
		if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	client.NewEndpoints(map[string]EndpointsUpdate{testCDSName: wantUpdate})
	for i := 0; i < count-1; i++ {
		if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[i], wantUpdate); err != nil {
			t.Fatal(err)
		}
	}

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := endpointsUpdateChs[count-1].Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestEndpointsThreeWatchDifferentResourceName(t *testing.T) {
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
	var endpointsUpdateChs []*testutils.Channel
	const count = 2
	for i := 0; i < count; i++ {
		endpointsUpdateCh := testutils.NewChannel()
		endpointsUpdateChs = append(endpointsUpdateChs, endpointsUpdateCh)
		client.WatchEndpoints(testCDSName+"1", func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	// Third watch for a different name.
	endpointsUpdateCh2 := testutils.NewChannel()
	client.WatchEndpoints(testCDSName+"2", func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	wantUpdate2 := EndpointsUpdate{Localities: []Locality{testLocalities[1]}}
	client.NewEndpoints(map[string]EndpointsUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	})

	for i := 0; i < count; i++ {
		if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[i], wantUpdate1); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh2, wantUpdate2); err != nil {
		t.Fatal(err)
	}
}

// TestEndpointsWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestEndpointsWatchAfterCache(t *testing.T) {
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

	endpointsUpdateCh := testutils.NewChannel()
	client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	client.NewEndpoints(map[string]EndpointsUpdate{testCDSName: wantUpdate})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	endpointsUpdateCh2 := testutils.NewChannel()
	client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(endpointsUpdateErr{u: update, err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[EndpointsResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh2, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Old watch should see nothing.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := endpointsUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestEndpointsWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestEndpointsWatchExpiryTimer(t *testing.T) {
	apiClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	client, err := newWithConfig(clientOpts(testXDSServer, true))
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

	endpointsUpdateCh := testutils.NewChannel()
	client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh.Send(endpointsUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := endpointsUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(endpointsUpdateErr)
	if gotUpdate.err == nil || !cmp.Equal(gotUpdate.u, EndpointsUpdate{}) {
		t.Fatalf("unexpected endpointsUpdate: (%v, %v), want: (EndpointsUpdate{}, nil)", gotUpdate.u, gotUpdate.err)
	}
}
