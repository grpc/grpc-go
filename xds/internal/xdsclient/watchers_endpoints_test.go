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
		endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Push an update, with an extra resource for a different resource name.
	// Specify a non-nil raw proto in the original resource to ensure that the
	// new update is not considered equal to the old one.
	newUpdate := wantUpdate
	newUpdate.Raw = &anypb.Any{}
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{
		testCDSName:  {Update: newUpdate},
		"randomName": {},
	}, UpdateMetadata{})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, newUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
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
			endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
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
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again. None of the watchers should
	// be notified because one has been cancelled, and the other is receiving
	// the same update.
	cancelLastWatch()
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		func() {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := endpointsUpdateChs[i].Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected endpointsUpdate: %v, %v, want channel recv timeout", u, err)
			}
		}()
	}

	// Push a new update and make sure the uncancelled watcher is invoked.
	// Specify a non-nil raw proto to ensure that the new update is not
	// considered equal to the old one.
	newUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}, Raw: &anypb.Any{}}
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: newUpdate}}, UpdateMetadata{})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[0], newUpdate, nil); err != nil {
		t.Fatal(err)
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
			endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
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
		endpointsUpdateCh2.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	wantUpdate2 := EndpointsUpdate{Localities: []Locality{testLocalities[1]}}
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{
		testCDSName + "1": {Update: wantUpdate1},
		testCDSName + "2": {Update: wantUpdate2},
	}, UpdateMetadata{})

	for i := 0; i < count; i++ {
		if err := verifyEndpointsUpdate(ctx, endpointsUpdateChs[i], wantUpdate1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh2, wantUpdate2, nil); err != nil {
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
		endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := EndpointsUpdate{Localities: []Locality{testLocalities[0]}}
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	endpointsUpdateCh2 := testutils.NewChannel()
	client.WatchEndpoints(testCDSName, func(update EndpointsUpdate, err error) {
		endpointsUpdateCh2.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[EndpointsResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh2, wantUpdate, nil); err != nil {
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
		endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := endpointsUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(EndpointsUpdateErrTuple)
	if gotUpdate.Err == nil || !cmp.Equal(gotUpdate.Update, EndpointsUpdate{}) {
		t.Fatalf("unexpected endpointsUpdate: (%v, %v), want: (EndpointsUpdate{}, nil)", gotUpdate.Update, gotUpdate.Err)
	}
}

// TestEndpointsWatchNACKError covers the case that an update is NACK'ed, and
// the watcher should also receive the error.
func (s) TestEndpointsWatchNACKError(t *testing.T) {
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
		endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
	})
	defer cancelWatch()
	if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantError := fmt.Errorf("testing error")
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{testCDSName: {Err: wantError}}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, EndpointsUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestEndpointsWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestEndpointsWatchPartialValid(t *testing.T) {
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

	for _, name := range []string{testCDSName, badResourceName} {
		endpointsUpdateCh := testutils.NewChannel()
		cancelWatch := client.WatchEndpoints(name, func(update EndpointsUpdate, err error) {
			endpointsUpdateCh.Send(EndpointsUpdateErrTuple{Update: update, Err: err})
		})
		defer func() {
			cancelWatch()
			if _, err := apiClient.removeWatches[EndpointsResource].Receive(ctx); err != nil {
				t.Fatalf("want watch to be canceled, got err: %v", err)
			}
		}()
		if _, err := apiClient.addWatches[EndpointsResource].Receive(ctx); err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		updateChs[name] = endpointsUpdateCh
	}

	wantError := fmt.Errorf("testing error")
	wantError2 := fmt.Errorf("individual error")
	client.NewEndpoints(map[string]EndpointsUpdateErrTuple{
		testCDSName:     {Update: EndpointsUpdate{Localities: []Locality{testLocalities[0]}}},
		badResourceName: {Err: wantError2},
	}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})

	// The valid resource should be sent to the watcher.
	if err := verifyEndpointsUpdate(ctx, updateChs[testCDSName], EndpointsUpdate{Localities: []Locality{testLocalities[0]}}, nil); err != nil {
		t.Fatal(err)
	}

	// The failed watcher should receive an error.
	if err := verifyEndpointsUpdate(ctx, updateChs[badResourceName], EndpointsUpdate{}, wantError2); err != nil {
		t.Fatal(err)
	}
}
