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
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"

	"google.golang.org/grpc/xds/internal"
)

var (
	testLocalities = []xdsresource.Locality{
		{
			Endpoints: []xdsresource.Endpoint{{Address: "addr1:314"}},
			ID:        internal.LocalityID{SubZone: "locality-1"},
			Priority:  1,
			Weight:    1,
		},
		{
			Endpoints: []xdsresource.Endpoint{{Address: "addr2:159"}},
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
	testWatch(t, xdsresource.EndpointsResource, xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}}, testCDSName)
}

// TestEndpointsTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestEndpointsTwoWatchSameResourceName(t *testing.T) {
	testTwoWatchSameResourceName(t, xdsresource.EndpointsResource, xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}}, testCDSName)
}

// TestEndpointsThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestEndpointsThreeWatchDifferentResourceName(t *testing.T) {
	testThreeWatchDifferentResourceName(t, xdsresource.EndpointsResource,
		xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}}, testCDSName+"1",
		xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[1]}}, testCDSName+"2",
	)
}

// TestEndpointsWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestEndpointsWatchAfterCache(t *testing.T) {
	testWatchAfterCache(t, xdsresource.EndpointsResource, xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}}, testCDSName)
}

// TestEndpointsWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestEndpointsWatchExpiryTimer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, _ := testClientSetup(t, true)
	endpointsUpdateCh, _ := newWatch(t, client, xdsresource.EndpointsResource, testCDSName)

	u, err := endpointsUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for endpoints update: %v", err)
	}
	gotUpdate := u.(xdsresource.EndpointsUpdateErrTuple)
	if gotUpdate.Err == nil || !cmp.Equal(gotUpdate.Update, xdsresource.EndpointsUpdate{}) {
		t.Fatalf("unexpected endpointsUpdate: (%v, %v), want: (EndpointsUpdate{}, nil)", gotUpdate.Update, gotUpdate.Err)
	}
}

// TestEndpointsWatchNACKError covers the case that an update is NACK'ed, and
// the watcher should also receive the error.
func (s) TestEndpointsWatchNACKError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	endpointsUpdateCh, _ := newWatch(t, client, xdsresource.EndpointsResource, testCDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.EndpointsResource, testCDSName)

	wantError := fmt.Errorf("testing error")
	updateHandler.NewEndpoints(map[string]xdsresource.EndpointsUpdateErrTuple{testCDSName: {Err: wantError}}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
	if err := verifyEndpointsUpdate(ctx, endpointsUpdateCh, xdsresource.EndpointsUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestEndpointsWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestEndpointsWatchPartialValid(t *testing.T) {
	testWatchPartialValid(t, xdsresource.EndpointsResource, xdsresource.EndpointsUpdate{Localities: []xdsresource.Locality{testLocalities[0]}}, testCDSName)
}
