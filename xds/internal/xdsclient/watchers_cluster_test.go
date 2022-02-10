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
)

// TestClusterWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func (s) TestClusterWatch(t *testing.T) {
	testWatch(t, xdsresource.ClusterResource, xdsresource.ClusterUpdate{ClusterName: testEDSName}, testCDSName)
}

// TestClusterTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestClusterTwoWatchSameResourceName(t *testing.T) {
	testTwoWatchSameResourceName(t, xdsresource.ClusterResource, xdsresource.ClusterUpdate{ClusterName: testEDSName}, testCDSName)
}

// TestClusterThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestClusterThreeWatchDifferentResourceName(t *testing.T) {
	testThreeWatchDifferentResourceName(t, xdsresource.ClusterResource,
		xdsresource.ClusterUpdate{ClusterName: testEDSName + "1"}, testCDSName+"1",
		xdsresource.ClusterUpdate{ClusterName: testEDSName + "2"}, testCDSName+"2",
	)
}

// TestClusterWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestClusterWatchAfterCache(t *testing.T) {
	testWatchAfterCache(t, xdsresource.ClusterResource, xdsresource.ClusterUpdate{ClusterName: testEDSName}, testCDSName)
}

// TestClusterWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestClusterWatchExpiryTimer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, _ := testClientSetup(t, true)
	clusterUpdateCh, _ := newWatch(t, client, xdsresource.ClusterResource, testCDSName)

	u, err := clusterUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(xdsresource.ClusterUpdateErrTuple)
	if gotUpdate.Err == nil || !cmp.Equal(gotUpdate.Update, xdsresource.ClusterUpdate{}) {
		t.Fatalf("unexpected clusterUpdate: (%v, %v), want: (ClusterUpdate{}, nil)", gotUpdate.Update, gotUpdate.Err)
	}
}

// TestClusterWatchExpiryTimerStop tests the case where the client does receive
// an CDS response for the request that it sends out. We want no error even
// after expiry timeout.
func (s) TestClusterWatchExpiryTimerStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, true)
	clusterUpdateCh, _ := newWatch(t, client, xdsresource.ClusterResource, testCDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.ClusterResource, testCDSName)

	wantUpdate := xdsresource.ClusterUpdate{ClusterName: testEDSName}
	updateHandler.NewClusters(map[string]xdsresource.ClusterUpdateErrTuple{
		testCDSName: {Update: wantUpdate},
	}, xdsresource.UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Wait for an error, the error should never happen.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestWatchExpiryTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterResourceRemoved covers the cases:
// - an update is received after a watch()
// - another update is received, with one resource removed
//   - this should trigger callback with resource removed error
// - one more update without the removed resource
//   - the callback (above) shouldn't receive any update
func (s) TestClusterResourceRemoved(t *testing.T) {
	testResourceRemoved(t, xdsresource.ClusterResource,
		xdsresource.ClusterUpdate{ClusterName: testEDSName + "1"}, testCDSName+"1",
		xdsresource.ClusterUpdate{ClusterName: testEDSName + "2"}, testCDSName+"2",
	)
}

// TestClusterWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestClusterWatchNACKError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client, ctrlCh := testClientSetup(t, false)
	clusterUpdateCh, _ := newWatch(t, client, xdsresource.ClusterResource, testCDSName)
	_, updateHandler := getControllerAndPubsub(ctx, t, client, ctrlCh, xdsresource.ClusterResource, testCDSName)

	wantError := fmt.Errorf("testing error")
	updateHandler.NewClusters(map[string]xdsresource.ClusterUpdateErrTuple{testCDSName: {
		Err: wantError,
	}}, xdsresource.UpdateMetadata{ErrState: &xdsresource.UpdateErrorMetadata{Err: wantError}})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, xdsresource.ClusterUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestClusterWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestClusterWatchPartialValid(t *testing.T) {
	testWatchPartialValid(t, xdsresource.ClusterResource, xdsresource.ClusterUpdate{ClusterName: testEDSName}, testCDSName)
}
