// +build go1.12

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

	"google.golang.org/grpc/internal/testutils"
)

type clusterUpdateErr struct {
	u   ClusterUpdate
	err error
}

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
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another update, with an extra resource for a different resource name.
	client.NewClusters(map[string]ClusterUpdate{
		testCDSName:  wantUpdate,
		"randomName": {},
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Cancel watch, and send update again.
	cancelWatch()
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate}, UpdateMetadata{})
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
			clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
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
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate}, UpdateMetadata{})
	for i := 0; i < count; i++ {
		if err := verifyClusterUpdate(ctx, clusterUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel the last watch, and send update again.
	cancelLastWatch()
	client.NewClusters(map[string]ClusterUpdate{testCDSName: wantUpdate}, UpdateMetadata{})
	for i := 0; i < count-1; i++ {
		if err := verifyClusterUpdate(ctx, clusterUpdateChs[i], wantUpdate, nil); err != nil {
			t.Fatal(err)
		}
	}

	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateChs[count-1].Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
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
			clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
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
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ClusterName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ClusterName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
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
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, wantUpdate, nil); err != nil {
		t.Fatal(err)
	}

	// Another watch for the resource in cache.
	clusterUpdateCh2 := testutils.NewChannel()
	client.WatchCluster(testCDSName, func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
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
		clusterUpdateCh.Send(clusterUpdateErr{u: u, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	u, err := clusterUpdateCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for cluster update: %v", err)
	}
	gotUpdate := u.(clusterUpdateErr)
	if gotUpdate.err == nil || !cmp.Equal(gotUpdate.u, ClusterUpdate{}) {
		t.Fatalf("unexpected clusterUpdate: (%v, %v), want: (ClusterUpdate{}, nil)", gotUpdate.u, gotUpdate.err)
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
		clusterUpdateCh.Send(clusterUpdateErr{u: u, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := ClusterUpdate{ClusterName: testEDSName}
	client.NewClusters(map[string]ClusterUpdate{
		testCDSName: wantUpdate,
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
		clusterUpdateCh1.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	// Another watch for a different name.
	clusterUpdateCh2 := testutils.NewChannel()
	client.WatchCluster(testCDSName+"2", func(update ClusterUpdate, err error) {
		clusterUpdateCh2.Send(clusterUpdateErr{u: update, err: err})
	})
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate1 := ClusterUpdate{ClusterName: testEDSName + "1"}
	wantUpdate2 := ClusterUpdate{ClusterName: testEDSName + "2"}
	client.NewClusters(map[string]ClusterUpdate{
		testCDSName + "1": wantUpdate1,
		testCDSName + "2": wantUpdate2,
	}, UpdateMetadata{})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh1, wantUpdate1, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}

	// Send another update to remove resource 1.
	client.NewClusters(map[string]ClusterUpdate{testCDSName + "2": wantUpdate2}, UpdateMetadata{})

	// Watcher 1 should get an error.
	if u, err := clusterUpdateCh1.Receive(ctx); err != nil || ErrType(u.(clusterUpdateErr).err) != ErrorTypeResourceNotFound {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v, want update with error resource not found", u, err)
	}

	// Watcher 2 should get the same update again.
	if err := verifyClusterUpdate(ctx, clusterUpdateCh2, wantUpdate2, nil); err != nil {
		t.Fatal(err)
	}

	// Send one more update without resource 1.
	client.NewClusters(map[string]ClusterUpdate{testCDSName + "2": wantUpdate2}, UpdateMetadata{})

	// Watcher 1 should not see an update.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if u, err := clusterUpdateCh1.Receive(sCtx); err != context.DeadlineExceeded {
		t.Errorf("unexpected clusterUpdate: %v, %v, want channel recv timeout", u, err)
	}

	// Watcher 2 should get the same update again.
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
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
	})
	defer cancelWatch()
	if _, err := apiClient.addWatches[ClusterResource].Receive(ctx); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantError := fmt.Errorf("testing error")
	client.NewClusters(map[string]ClusterUpdate{testCDSName: {}}, UpdateMetadata{ErrState: &UpdateErrorMetadata{Err: wantError}})
	if err := verifyClusterUpdate(ctx, clusterUpdateCh, ClusterUpdate{}, nil); err != nil {
		t.Fatal(err)
	}
}
