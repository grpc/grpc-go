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

// TestClusterWatch covers the cases:
// - an update is received after a watch()
// - an update for another resource name
// - an update is received after cancel()
func (s) TestClusterWatch(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Push an update, with an extra resource for a different resource name.
	// Specify a non-nil raw proto in the original resource to ensure that the
	// new update is not considered equal to the old one.
	newUpdate := wantUpdate
	newUpdate.Raw = &anypb.Any{}
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName:  {Update: newUpdate},
		"randomName": {},
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, newUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterTwoWatchSameResourceName covers the case where an update is received
// after two watch() for the same resource name.
func (s) TestClusterTwoWatchSameResourceName(t *testing.T) {
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

	var clusterUpdateChs []*testutils.Channel
	var cancelLastWatch func()
	const count = 2
	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		cancelLastWatch = client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		if err := verifyClusterUpdate(ctx, clusterUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again. None of the watchers should
	// be notified because one has been cancelled, and the other is receiving
	// the same update.
	cancelLastWatch()
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {Update: wantUpdate}}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		func() {
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if u, err := clusterUpdateChs[i].Receive(sCtx); err != context.DeadlineExceeded {
				t.Errorf("unexpected ClusterUpdate: %v, %v, want channel recv timeout", u, err)
			}
		}()
	}

	// Push a new update and make sure the uncancelled watcher is invoked.
	// Specify a non-nil raw proto to ensure that the new update is not
	// considered equal to the old one.
	newUpdate := ClusterUpdate{ClusterName: testEDSName, Raw: &anypb.Any{}}
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {Update: newUpdate}}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateChs[0], newUpdate, nil); err != nil {
		t.Fatal(err)
	}
}

// TestClusterThreeWatchDifferentResourceName covers the case where an update is
// received after three watch() for different resource names.
func (s) TestClusterThreeWatchDifferentResourceName(t *testing.T) {
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
	var clusterUpdateChs []*testutils.Channel
	const count = 2
	for i := 0; i < count; i++ {
		clusterUpdateCh := testutils.NewChannel()
		clusterUpdateChs = append(clusterUpdateChs, clusterUpdateCh)
		client.WatchCluster(testCDSName+"1", func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
		})

		if i == 0 {
			// A new watch is registered on the underlying API client only for
			// the first iteration because we are using the same resource name.
			if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
				t.Fatalf("want new watch to start, got error %v", err)
			}
		}
	}

	// Third watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	client.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ClusterName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ClusterName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName + "1": {Update: wantUpdate1},
		testCDSName + "2": {Update: wantUpdate2},
	}, UpdateMetadata{})

	for i := 0; i < count; i++ {
		if err := verifyClusterUpdate(ctx, clusterUpdateChs[i], wantUpdate1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

// TestClusterWatchAfterCache covers the case where watch is called after the update
// is in cache.
func (s) TestClusterWatchAfterCache(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName: {Update: wantUpdate},
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	clusterUpdateCh2 := testutils.NewChannel()
	client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := apiClient.addWatches[ClusterResource].Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("want no new watch to start (recv timeout), got resource name: %v error %v", n, err)
	}

	// New watch should receives the update.
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Old watch should see nothing.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}
}

// TestClusterWatchExpiryTimer tests the case where the client does not receive
// an CDS response for the request that it sends out. We want the watch callback
// to be invoked with an error once the watchExpiryTimer fires.
func (s) TestClusterWatchExpiryTimer(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	client.WatchCluster(testCDSName, func(u ClusterUpdate, err error) {
		clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: u, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := clusterUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(ClusterUpdateErrTuple)
	if gotUpdate.Err == nil || !cmp.Equal(gotUpdate.Update, ClusterUpdate{}) {
		t.Fatalf("unexpected clusterUpdate: (%v, %v), want: (ClusterUpdate{}, nil)", gotUpdate.Update, gotUpdate.Err)
	}
}

// TestClusterWatchExpiryTimerStop tests the case where the client does receive
// an CDS response for the request that it sends out. We want no error even
// after expiry timeout.
func (s) TestClusterWatchExpiryTimerStop(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	client.WatchCluster(testCDSName, func(u ClusterUpdate, err error) {
		clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: u, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName: {Update: wantUpdate},
	}, UpdateMetadata{})
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

	clusterUpdateCh1 := testutils.NewChannel()
	client.WatchCluster(testCDSName+"1", func(update ClusterUpdate, err error) {
		clusterUpdateCh1.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Another watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	client.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ClusterName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ClusterName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName + "1": {Update: wantUpdate1},
		testCDSName + "2": {Update: wantUpdate2},
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh1, wantUpdate1, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}

	// Send another update to remove resource 1.
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName + "2": {Update: wantUpdate2}}, UpdateMetadata{})

	// Watcher 1 should get an error.
	if u, err := clusterUpdateCh1.Receive(ctx); err != nil || ErrType(u.(ClusterUpdateErrTuple).Err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Watcher 2 should not see an update since the resource has not changed.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh2.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected ClusterUpdate: %v, want receiving from channel timeout", u)
	}

	// Send another update with resource 2 modified. Specify a non-nil raw proto
	// to ensure that the new update is not considered equal to the old one.
	wantUpdate2 = ClusterUpdate{ClusterName: testEDSName + "2", Raw: &anypb.Any{}}
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName + "2": {Update: wantUpdate2}}, UpdateMetadata{})

	// Watcher 1 should not see an update.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh1.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected Cluster: %v, want receiving from channel timeout", u)
	}

	// Watcher 2 should get the update.
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}
}

// TestClusterWatchNACKError covers the case that an update is NACK'ed, and the
// watcher should also receive the error.
func (s) TestClusterWatchNACKError(t *testing.T) {
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

	clusterUpdateCh := testutils.NewChannel()
	cancelWatch := client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
	})
	defer cancelWatch()
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantError := fmt.Errorf("testing error")
	client.NewClusters(map[string]ClusterUpdateErrTuple{testCDSName: {
		Err: wantError,
	}}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, ClusterUpdate{}, wantError); err != nil {
		t.Fatal(err)
	}
}

// TestClusterWatchPartialValid covers the case that a response contains both
// valid and invalid resources. This response will be NACK'ed by the xdsclient.
// But the watchers with valid resources should receive the update, those with
// invalida resources should receive an error.
func (s) TestClusterWatchPartialValid(t *testing.T) {
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
		clusterUpdateCh := testutils.NewChannel()
		cancelWatch := client.WatchCluster(name, func(update ClusterUpdate, err error) {
			clusterUpdateCh.Send(ClusterUpdateErrTuple{Update: update, Err: err})
		})
		defer func() {
			cancelWatch()
			if _, err := apiClient.removeWatches[ClusterResource].Receive(ctx); err != nil {
				t.Fatalf("want watch to be canceled, got err: %v", err)
			}
		}()
		if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		updateChs[name] = clusterUpdateCh
	}

	wantError := fmt.Errorf("testing error")
	wantError2 := fmt.Errorf("individual error")
	client.NewClusters(map[string]ClusterUpdateErrTuple{
		testCDSName:     {Update: ClusterUpdate{ClusterName: testEDSName}},
		badResourceName: {Err: wantError2},
	}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})

	// The valid resource should be sent to the watcher.
	if err := verifyClusterUpdate(ctx, updateChs[testCDSName], ClusterUpdate{ClusterName: testEDSName}, nil); err != nil {
		t.Fatal(err)
	}

	// The failed watcher should receive an error.
	if err := verifyClusterUpdate(ctx, updateChs[badResourceName], ClusterUpdate{}, wantError2); err != nil {
		t.Fatal(err)
	}
}
