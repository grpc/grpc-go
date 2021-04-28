/*
 * Copyright 2021 gRPC authors.
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
 */

package cdsbalancer

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"testing"

	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	edsService              = "EDS Service"
	logicalDNSService       = "Logical DNS Service"
	edsService2             = "EDS Service 2"
	logicalDNSService2      = "Logical DNS Service 2"
	aggregateClusterService = "Aggregate Cluster Service"
)

// setupTests creates a clusterHandler with a fake xds client for control over xds client.
func setupTests(t *testing.T) (*clusterHandler, *fakeclient.Client) {
	xdsC := fakeclient.NewClient()
	ch := &clusterHandler{
		xdsClient: xdsC,
		// This is will be how the update channel is created in cds. It will be a separate channel to the buffer.Unbounded.
		// This channel will also be read from to test any cluster updates.
		updateChannel: make(chan clusterHandlerUpdate, 1),
	}
	return ch, xdsC
}

// Simplest case: the cluster handler receives a cluster name, handler starts a watch for that cluster, xds client returns
// that it is a Leaf Node (EDS or LogicalDNS), not a tree, so expectation that update is written to buffer which will be
// read by CDS LB.
func (s) TestSuccessCaseLeafNode(t *testing.T) {
	tests := []struct {
		name          string
		clusterName   string
		clusterUpdate xdsclient.ClusterUpdate
	}{
		{name: "test-update-root-cluster-EDS-success",
			clusterName: edsService,
			clusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeEDS,
				ServiceName: edsService,
			}},
		{
			name:        "test-update-root-cluster-Logical-DNS-success",
			clusterName: logicalDNSService,
			clusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeLogicalDNS,
				ServiceName: logicalDNSService,
			}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch, fakeClient := setupTests(t)
			// When you first update the root cluster, it should hit the code path which will start a cluster node for that root.
			// Updating the root cluster logically represents a ping from a ClientConn.
			ch.updateRootCluster(test.clusterName)
			// Starting a cluster node involves communicating with the xdsClient, telling it to watch a cluster.
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			if gotCluster != test.clusterName {
				t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, test.clusterName)
			}
			// Invoke callback with xds client with a certain clusterUpdate. Due to this cluster update filling out the whole
			// cluster tree, as the cluster is of a root type (EDS or Logical DNS) and not an aggregate cluster, this should
			// trigger the ClusterHandler to write to the update buffer to update the CDS policy.
			fakeClient.InvokeWatchClusterCallback(test.clusterUpdate, nil)
			select {
			case chu := <-ch.updateChannel:
				if diff := cmp.Diff(chu.chu, []xdsclient.ClusterUpdate{test.clusterUpdate}); diff != "" {
					t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from updateChannel.")
			}
		})
	}
}

// The cluster handler receives a cluster name, handler starts a watch for that cluster, xds client returns
// that it is a Leaf Node (EDS or LogicalDNS), not a tree, so expectation that first update is written to buffer which will be
// read by CDS LB. Then, send a new cluster update that is different, with the expectation that it is also written to the
// update buffer to send back to CDS.
func (s) TestSuccessCaseLeafNodeThenNewUpdate(t *testing.T) {
	tests := []struct {
		name             string
		clusterName      string
		clusterUpdate    xdsclient.ClusterUpdate
		newClusterUpdate xdsclient.ClusterUpdate
	}{
		{name: "test-update-root-cluster-then-new-update-EDS-success",
			clusterName: edsService,
			clusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeEDS,
				ServiceName: edsService,
			},
			newClusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeEDS,
				ServiceName: edsService,
				EnableLRS:   true,
			},
		},
		{
			name:        "test-update-root-cluster-then-new-update-Logical-DNS-success",
			clusterName: logicalDNSService,
			clusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeLogicalDNS,
				ServiceName: logicalDNSService,
			},
			newClusterUpdate: xdsclient.ClusterUpdate{
				ClusterType: xdsclient.ClusterTypeLogicalDNS,
				ServiceName: logicalDNSService,
				EnableLRS:   true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch, fakeClient := setupTests(t)
			ch.updateRootCluster(test.clusterName)
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			_, err := fakeClient.WaitForWatchCluster(ctx)
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			fakeClient.InvokeWatchClusterCallback(test.clusterUpdate, nil)
			select {
			case <-ch.updateChannel:
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from updateChannel.")
			}

			// Above represents same thing as the simple TestSuccessCaseLeafNode, extra behavior + validation (clusterNode
			// which is a leaf receives a changed clusterUpdate, which should ping clusterHandler, which should then write
			// to the update buffer).
			fakeClient.InvokeWatchClusterCallback(test.newClusterUpdate, nil)
			select {
			case chu := <-ch.updateChannel:
				if diff := cmp.Diff(chu.chu, []xdsclient.ClusterUpdate{test.newClusterUpdate}); diff != "" {
					t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from updateChannel.")
			}
		})
	}
}

// TestUpdateRootClusterAggregateSuccess tests the case where an aggregate cluster is a root pointing to two child clusters
// one of type EDS and the other of type LogicalDNS. This test will then send cluster updates for both the children, and
// at the end there should be a successful clusterUpdate written to the update buffer to send back to CDS.
func (s) TestUpdateRootClusterAggregateSuccess(t *testing.T) {
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != aggregateClusterService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, aggregateClusterService)
	}
	// The xdsClient telling the clusterNode that the cluster type is an aggregate cluster which will cause a lot of
	// downstream behavior. For a cluster type that isn't an aggregate, the behavior is simple. The clusterNode will
	// simply get a successful update, which will then ping the clusterHandler which will successfully build an update
	// to send to the CDS policy. In the aggregate cluster case, the handleResp callback must also start watches for
	// the aggregate cluster's children. The ping to the clusterHandler at the end of handleResp should be a no-op,
	// as neither the EDS or LogicalDNS child clusters have received an update yet.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType:             xdsclient.ClusterTypeAggregate,
		ServiceName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)

	// xds client should be called to start a watch for one of the child clusters of the aggregate, which should be an
	// EDS Service as ordering matters.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}

	// xds client should then be called to start a watch for the second child cluster, which should be of type LogicalDNS
	// as ordering matters.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != logicalDNSService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
	}

	// The handleResp() call on the root aggregate cluster should not ping the cluster handler to try and construct an update,
	// as the handleResp() callback knows that when a child is created, it cannot possibly build a successful update yet.
	// Thus, there should be nothing in the update channel.

	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Send callback for the EDS child cluster.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)

	// EDS child cluster will ping the Cluster Handler, to try an update, which still won't successfully build as the
	// LogicalDNS child of the root aggregate cluster has not yet received and handled an update.
	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Invoke callback for Logical DNS child cluster.

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)

	// Will Ping Cluster Handler, which will finally successfully build an update as all nodes in the tree of clusters
	// have received an update.  Since this cluster is an aggregate cluster comprised of two children, the returned update
	// should be length 2, as the xds cluster resolver LB policy only cares about the full list of LogicalDNS and EDS
	// clusters representing the base nodes of the tree of clusters. This list should be ordered as per the cluster update.
	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.chu, []xdsclient.ClusterUpdate{{
			ClusterType: xdsclient.ClusterTypeEDS,
			ServiceName: edsService,
		}, {
			ClusterType: xdsclient.ClusterTypeLogicalDNS,
			ServiceName: logicalDNSService,
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestUpdateRootClusterAggregateThenChangeChild tests the scenario where you have an aggregate cluster with an EDS child
// and a LogicalDNS child, then you change one of the children and send an update for the changed child. This should write
// a new update to the update buffer to send back to CDS.
func (s) TestUpdateRootClusterAggregateThenChangeChild(t *testing.T) {
	// This initial code is the same as the test for the aggregate success case, except without validations. This will get
	// this test to the point where it can change one of the children.
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType:             xdsclient.ClusterTypeAggregate,
		ServiceName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.WaitForWatchCluster(ctx)

	select {
	case <-ch.updateChannel:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType:             xdsclient.ClusterTypeAggregate,
		ServiceName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService2},
	}, nil)

	// The cluster update let's the aggregate cluster know that it's children are now edsService and logicalDNSService2,
	// which implies that the aggregateCluster lost it's old logicalDNSService child. Thus, the logicalDNSService child
	// should be deleted.
	clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != logicalDNSService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	// The handleResp() callback should then start a watch for logicalDNSService2.
	clusterNameCreated, err := fakeClient.WaitForWatchCluster(ctx)
	if clusterNameCreated != logicalDNSService2 {
		t.Fatalf("xdsClient.WatchCDS called for cluster %v, want: %v", clusterNameCreated, logicalDNSService2)
	}

	// handleResp() should try and send an update here, but it will fail as logicalDNSService2 has not yet received an update.
	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Invoke a callback for the new logicalDNSService2 - this will fill out the tree with successful updates.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService2,
	}, nil)

	// Behavior: This update make every node in the tree of cluster have received an update. Thus, at the end of this callback,
	// when you ping the clusterHandler to try and construct an update, the update should now successfully be written to
	// update buffer to send back to CDS. This new update should contain the new child of LogicalDNS2.

	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.chu, []xdsclient.ClusterUpdate{{
			ClusterType: xdsclient.ClusterTypeEDS,
			ServiceName: edsService,
		}, {
			ClusterType: xdsclient.ClusterTypeLogicalDNS,
			ServiceName: logicalDNSService2,
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestUpdateRootClusterAggregateThenChangeRootToEDS tests the situation where you have a fully updated aggregate cluster
// (where AggregateCluster success test gets you) as the root cluster, then you update that root cluster to a cluster of
// type EDS.
func (s) TestUpdateRootClusterAggregateThenChangeRootToEDS(t *testing.T) {
	// This initial code is the same as the test for the aggregate success case, except without validations. This will get
	// this test to the point where it can update the root cluster to one of type EDS.
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType:             xdsclient.ClusterTypeAggregate,
		ServiceName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.WaitForWatchCluster(ctx)

	select {
	case <-ch.updateChannel:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}

	// Changes the root aggregate cluster to a EDS cluster. This should delete the root aggregate cluster and all of it's
	// children by successfully canceling the watches for them.
	ch.updateRootCluster(edsService2)

	// Reads from the cancel channel, should first be type Aggregate, then EDS then Logical DNS.
	clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != aggregateClusterService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	clusterNameDeleted, err = fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != edsService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	clusterNameDeleted, err = fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != logicalDNSService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	// After deletion, it should start a watch for the EDS Cluster. The behavior for this EDS Cluster receiving an update
	// from xds client and then successfully writing an update to send back to CDS is already tested in the updateEDS
	// success case.
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService2 {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService2)
	}
}
