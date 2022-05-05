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
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const (
	edsService              = "EDS Service"
	logicalDNSService       = "Logical DNS Service"
	edsService2             = "EDS Service 2"
	logicalDNSService2      = "Logical DNS Service 2"
	aggregateClusterService = "Aggregate Cluster Service"
)

// setupTests creates a clusterHandler with a fake xds client for control over
// xds client.
func setupTests() (*clusterHandler, *fakeclient.Client) {
	xdsC := fakeclient.NewClient()
	ch := newClusterHandler(&cdsBalancer{xdsClient: xdsC})
	return ch, xdsC
}

// Simplest case: the cluster handler receives a cluster name, handler starts a
// watch for that cluster, xds client returns that it is a Leaf Node (EDS or
// LogicalDNS), not a tree, so expectation that update is written to buffer
// which will be read by CDS LB.
func (s) TestSuccessCaseLeafNode(t *testing.T) {
	tests := []struct {
		name          string
		clusterName   string
		clusterUpdate xdsresource.ClusterUpdate
		lbPolicy      *xdsresource.ClusterLBPolicyRingHash
	}{
		{
			name:        "test-update-root-cluster-EDS-success",
			clusterName: edsService,
			clusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeEDS,
				ClusterName: edsService,
			},
		},
		{
			name:        "test-update-root-cluster-EDS-with-ring-hash",
			clusterName: logicalDNSService,
			clusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeLogicalDNS,
				ClusterName: logicalDNSService,
				LBPolicy:    &xdsresource.ClusterLBPolicyRingHash{MinimumRingSize: 10, MaximumRingSize: 100},
			},
			lbPolicy: &xdsresource.ClusterLBPolicyRingHash{MinimumRingSize: 10, MaximumRingSize: 100},
		},
		{
			name:        "test-update-root-cluster-Logical-DNS-success",
			clusterName: logicalDNSService,
			clusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeLogicalDNS,
				ClusterName: logicalDNSService,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch, fakeClient := setupTests()
			// When you first update the root cluster, it should hit the code
			// path which will start a cluster node for that root. Updating the
			// root cluster logically represents a ping from a ClientConn.
			ch.updateRootCluster(test.clusterName)
			// Starting a cluster node involves communicating with the
			// xdsClient, telling it to watch a cluster.
			ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer ctxCancel()
			gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
			if err != nil {
				t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
			}
			if gotCluster != test.clusterName {
				t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, test.clusterName)
			}
			// Invoke callback with xds client with a certain clusterUpdate. Due
			// to this cluster update filling out the whole cluster tree, as the
			// cluster is of a root type (EDS or Logical DNS) and not an
			// aggregate cluster, this should trigger the ClusterHandler to
			// write to the update buffer to update the CDS policy.
			fakeClient.InvokeWatchClusterCallback(test.clusterUpdate, nil)
			select {
			case chu := <-ch.updateChannel:
				if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{test.clusterUpdate}); diff != "" {
					t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
				}
				if diff := cmp.Diff(chu.lbPolicy, test.lbPolicy); diff != "" {
					t.Fatalf("got unexpected lb policy in cluster update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from update channel.")
			}
			// Close the clusterHandler. This is meant to be called when the CDS
			// Balancer is closed, and the call should cancel the watch for this
			// cluster.
			ch.close()
			clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
			if err != nil {
				t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
			}
			if clusterNameDeleted != test.clusterName {
				t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
			}
		})
	}
}

// The cluster handler receives a cluster name, handler starts a watch for that
// cluster, xds client returns that it is a Leaf Node (EDS or LogicalDNS), not a
// tree, so expectation that first update is written to buffer which will be
// read by CDS LB. Then, send a new cluster update that is different, with the
// expectation that it is also written to the update buffer to send back to CDS.
func (s) TestSuccessCaseLeafNodeThenNewUpdate(t *testing.T) {
	tests := []struct {
		name             string
		clusterName      string
		clusterUpdate    xdsresource.ClusterUpdate
		newClusterUpdate xdsresource.ClusterUpdate
	}{
		{name: "test-update-root-cluster-then-new-update-EDS-success",
			clusterName: edsService,
			clusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeEDS,
				ClusterName: edsService,
			},
			newClusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeEDS,
				ClusterName: edsService2,
			},
		},
		{
			name:        "test-update-root-cluster-then-new-update-Logical-DNS-success",
			clusterName: logicalDNSService,
			clusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeLogicalDNS,
				ClusterName: logicalDNSService,
			},
			newClusterUpdate: xdsresource.ClusterUpdate{
				ClusterType: xdsresource.ClusterTypeLogicalDNS,
				ClusterName: logicalDNSService2,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ch, fakeClient := setupTests()
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

			// Check that sending the same cluster update also induces an update
			// to be written to update buffer.
			fakeClient.InvokeWatchClusterCallback(test.clusterUpdate, nil)
			shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
			defer shouldNotHappenCtxCancel()
			select {
			case <-ch.updateChannel:
			case <-shouldNotHappenCtx.Done():
				t.Fatal("Timed out waiting for update from updateChannel.")
			}

			// Above represents same thing as the simple
			// TestSuccessCaseLeafNode, extra behavior + validation (clusterNode
			// which is a leaf receives a changed clusterUpdate, which should
			// ping clusterHandler, which should then write to the update
			// buffer).
			fakeClient.InvokeWatchClusterCallback(test.newClusterUpdate, nil)
			select {
			case chu := <-ch.updateChannel:
				if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{test.newClusterUpdate}); diff != "" {
					t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for update from updateChannel.")
			}
		})
	}
}

// TestUpdateRootClusterAggregateSuccess tests the case where an aggregate
// cluster is a root pointing to two child clusters one of type EDS and the
// other of type LogicalDNS. This test will then send cluster updates for both
// the children, and at the end there should be a successful clusterUpdate
// written to the update buffer to send back to CDS.
func (s) TestUpdateRootClusterAggregateSuccess(t *testing.T) {
	ch, fakeClient := setupTests()
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

	// The xdsClient telling the clusterNode that the cluster type is an
	// aggregate cluster which will cause a lot of downstream behavior. For a
	// cluster type that isn't an aggregate, the behavior is simple. The
	// clusterNode will simply get a successful update, which will then ping the
	// clusterHandler which will successfully build an update to send to the CDS
	// policy. In the aggregate cluster case, the handleResp callback must also
	// start watches for the aggregate cluster's children. The ping to the
	// clusterHandler at the end of handleResp should be a no-op, as neither the
	// EDS or LogicalDNS child clusters have received an update yet.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)

	// xds client should be called to start a watch for one of the child
	// clusters of the aggregate. The order of the children in the update
	// written to the buffer to send to CDS matters, however there is no
	// guarantee on the order it will start the watches of the children.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
		}
	}

	// xds client should then be called to start a watch for the second child
	// cluster.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
		}
	}

	// The handleResp() call on the root aggregate cluster should not ping the
	// cluster handler to try and construct an update, as the handleResp()
	// callback knows that when a child is created, it cannot possibly build a
	// successful update yet. Thus, there should be nothing in the update
	// channel.

	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Send callback for the EDS child cluster.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: edsService,
	}, nil)

	// EDS child cluster will ping the Cluster Handler, to try an update, which
	// still won't successfully build as the LogicalDNS child of the root
	// aggregate cluster has not yet received and handled an update.
	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Invoke callback for Logical DNS child cluster.

	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSService,
	}, nil)

	// Will Ping Cluster Handler, which will finally successfully build an
	// update as all nodes in the tree of clusters have received an update.
	// Since this cluster is an aggregate cluster comprised of two children, the
	// returned update should be length 2, as the xds cluster resolver LB policy
	// only cares about the full list of LogicalDNS and EDS clusters
	// representing the base nodes of the tree of clusters. This list should be
	// ordered as per the cluster update.
	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: edsService,
		}, {
			ClusterType: xdsresource.ClusterTypeLogicalDNS,
			ClusterName: logicalDNSService,
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestUpdateRootClusterAggregateThenChangeChild tests the scenario where you
// have an aggregate cluster with an EDS child and a LogicalDNS child, then you
// change one of the children and send an update for the changed child. This
// should write a new update to the update buffer to send back to CDS.
func (s) TestUpdateRootClusterAggregateThenChangeChild(t *testing.T) {
	// This initial code is the same as the test for the aggregate success case,
	// except without validations. This will get this test to the point where it
	// can change one of the children.
	ch, fakeClient := setupTests()
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: edsService,
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSService,
	}, nil)

	select {
	case <-ch.updateChannel:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}

	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService2},
	}, nil)

	// The cluster update let's the aggregate cluster know that it's children
	// are now edsService and logicalDNSService2, which implies that the
	// aggregateCluster lost it's old logicalDNSService child. Thus, the
	// logicalDNSService child should be deleted.
	clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != logicalDNSService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	// The handleResp() callback should then start a watch for
	// logicalDNSService2.
	clusterNameCreated, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if clusterNameCreated != logicalDNSService2 {
		t.Fatalf("xdsClient.WatchCDS called for cluster %v, want: %v", clusterNameCreated, logicalDNSService2)
	}

	// handleResp() should try and send an update here, but it will fail as
	// logicalDNSService2 has not yet received an update.
	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Invoke a callback for the new logicalDNSService2 - this will fill out the
	// tree with successful updates.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSService2,
	}, nil)

	// Behavior: This update make every node in the tree of cluster have
	// received an update. Thus, at the end of this callback, when you ping the
	// clusterHandler to try and construct an update, the update should now
	// successfully be written to update buffer to send back to CDS. This new
	// update should contain the new child of LogicalDNS2.

	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: edsService,
		}, {
			ClusterType: xdsresource.ClusterTypeLogicalDNS,
			ClusterName: logicalDNSService2,
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestUpdateRootClusterAggregateThenChangeRootToEDS tests the situation where
// you have a fully updated aggregate cluster (where AggregateCluster success
// test gets you) as the root cluster, then you update that root cluster to a
// cluster of type EDS.
func (s) TestUpdateRootClusterAggregateThenChangeRootToEDS(t *testing.T) {
	// This initial code is the same as the test for the aggregate success case,
	// except without validations. This will get this test to the point where it
	// can update the root cluster to one of type EDS.
	ch, fakeClient := setupTests()
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.WaitForWatchCluster(ctx)
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: edsService,
	}, nil)
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSService,
	}, nil)

	select {
	case <-ch.updateChannel:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}

	// Changes the root aggregate cluster to a EDS cluster. This should delete
	// the root aggregate cluster and all of it's children by successfully
	// canceling the watches for them.
	ch.updateRootCluster(edsService2)

	// Reads from the cancel channel, should first be type Aggregate, then EDS
	// then Logical DNS.
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

	// After deletion, it should start a watch for the EDS Cluster. The behavior
	// for this EDS Cluster receiving an update from xds client and then
	// successfully writing an update to send back to CDS is already tested in
	// the updateEDS success case.
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService2 {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService2)
	}
}

// TestHandleRespInvokedWithError tests that when handleResp is invoked with an
// error, that the error is successfully written to the update buffer.
func (s) TestHandleRespInvokedWithError(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster(edsService)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{}, errors.New("some error"))
	select {
	case chu := <-ch.updateChannel:
		if chu.err.Error() != "some error" {
			t.Fatalf("Did not receive the expected error, instead received: %v", chu.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
}

// TestSwitchClusterNodeBetweenLeafAndAggregated tests having an existing
// cluster node switch between a leaf and an aggregated cluster. When the
// cluster switches from a leaf to an aggregated cluster, it should add
// children, and when it switches back to a leaf, it should delete those new
// children and also successfully write a cluster update to the update buffer.
func (s) TestSwitchClusterNodeBetweenLeafAndAggregated(t *testing.T) {
	// Getting the test to the point where there's a root cluster which is a eds
	// leaf.
	ch, fakeClient := setupTests()
	ch.updateRootCluster(edsService2)
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: edsService2,
	}, nil)
	select {
	case <-ch.updateChannel:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
	// Switch the cluster to an aggregate cluster, this should cause two new
	// child watches to be created.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             edsService2,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)

	// xds client should be called to start a watch for one of the child
	// clusters of the aggregate. The order of the children in the update
	// written to the buffer to send to CDS matters, however there is no
	// guarantee on the order it will start the watches of the children.
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
		}
	}

	// xds client should then be called to start a watch for the second child
	// cluster.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
		}
	}

	// After starting a watch for the second child cluster, there should be no
	// more watches started on the xds client.
	shouldNotHappenCtx, shouldNotHappenCtxCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()
	gotCluster, err = fakeClient.WaitForWatchCluster(shouldNotHappenCtx)
	if err == nil {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, no more watches should be started.", gotCluster)
	}

	// The handleResp() call on the root aggregate cluster should not ping the
	// cluster handler to try and construct an update, as the handleResp()
	// callback knows that when a child is created, it cannot possibly build a
	// successful update yet. Thus, there should be nothing in the update
	// channel.

	shouldNotHappenCtx, shouldNotHappenCtxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-shouldNotHappenCtx.Done():
	}

	// Switch the cluster back to an EDS Cluster. This should cause the two
	// children to be deleted.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: edsService2,
	}, nil)

	// Should delete the two children (no guarantee of ordering deleted, which
	// is ok), then successfully write an update to the update buffer as the
	// full cluster tree has received updates.
	clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	// No guarantee of ordering, so one of the children should be deleted first.
	if clusterNameDeleted != edsService {
		if clusterNameDeleted != logicalDNSService {
			t.Fatalf("xdsClient.CancelCDS called for cluster %v, want either: %v or: %v", clusterNameDeleted, edsService, logicalDNSService)
		}
	}
	// Then the other child should be deleted.
	clusterNameDeleted, err = fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != edsService {
		if clusterNameDeleted != logicalDNSService {
			t.Fatalf("xdsClient.CancelCDS called for cluster %v, want either: %v or: %v", clusterNameDeleted, edsService, logicalDNSService)
		}
	}

	// After cancelling a watch for the second child cluster, there should be no
	// more watches cancelled on the xds client.
	shouldNotHappenCtx, shouldNotHappenCtxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shouldNotHappenCtxCancel()
	gotCluster, err = fakeClient.WaitForCancelClusterWatch(shouldNotHappenCtx)
	if err == nil {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, no more watches should be cancelled.", gotCluster)
	}

	// Then an update should successfully be written to the update buffer.
	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: edsService2,
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for update from update channel.")
	}
}

// TestExceedsMaxStackDepth tests the scenario where an aggregate cluster
// exceeds the maximum depth, which is 16. This should cause an error to be
// written to the update buffer.
func (s) TestExceedsMaxStackDepth(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster("cluster0")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	for i := 0; i <= 15; i++ {
		fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
			ClusterType:             xdsresource.ClusterTypeAggregate,
			ClusterName:             "cluster" + fmt.Sprint(i),
			PrioritizedClusterNames: []string{"cluster" + fmt.Sprint(i+1)},
		}, nil)
		if i == 15 {
			// The 16th iteration will try and create a cluster which exceeds
			// max stack depth and will thus error, so no CDS Watch will be
			// started for the child.
			continue
		}
		_, err = fakeClient.WaitForWatchCluster(ctx)
		if err != nil {
			t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
		}
	}
	select {
	case chu := <-ch.updateChannel:
		if chu.err.Error() != "aggregate cluster graph exceeds max depth" {
			t.Fatalf("Did not receive the expected error, instead received: %v", chu.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for an error to be written to update channel.")
	}
}

// TestDiamondDependency tests a diamond shaped aggregate cluster (A->[B,C];
// B->D; C->D). Due to both B and C pointing to D as it's child, it should be
// ignored for C. Once all 4 clusters have received a CDS update, an update
// should be then written to the update buffer, specifying a single Cluster D.
func (s) TestDiamondDependency(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster("clusterA")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterB", "clusterC"},
	}, nil)
	// Two watches should be started for both child clusters.
	_, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	_, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	// B -> D.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterB",
		PrioritizedClusterNames: []string{"clusterD"},
	}, nil)
	_, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	// This shouldn't cause an update to be written to the update buffer,
	// as cluster C has not received a cluster update yet.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: "clusterD",
	}, nil)

	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("an update should not have been written to the update buffer")
	case <-sCtx.Done():
	}

	// This update for C should cause an update to be written to the update
	// buffer. When you search this aggregated cluster graph, each node has
	// received an update. This update should only contain one clusterD, as
	// clusterC does not add a clusterD child update due to the clusterD update
	// already having been added as a child of clusterB.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterC",
		PrioritizedClusterNames: []string{"clusterD"},
	}, nil)

	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "clusterD",
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestIgnoreDups tests the cluster (A->[B, C]; B->[C, D]). Only one watch
// should be started for cluster C. The update written to the update buffer
// should only contain one instance of cluster C correctly as a higher priority
// than D.
func (s) TestIgnoreDups(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster("clusterA")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterB", "clusterC"},
	}, nil)
	// Two watches should be started, one for each child cluster.
	_, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	_, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	// The child cluster C should not have a watch started for it, as it is
	// already part of the aggregate cluster graph as the child of the root
	// cluster clusterA and has already had a watch started for it.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterB",
		PrioritizedClusterNames: []string{"clusterC", "clusterD"},
	}, nil)
	// Only one watch should be started, which should be for clusterD.
	name, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if name != "clusterD" {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: clusterD", name)
	}

	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err = fakeClient.WaitForWatchCluster(sCtx); err == nil {
		t.Fatalf("only one watch should have been started for the children of clusterB")
	}

	// This update should not cause an update to be written to the update
	// buffer, as each cluster in the tree has not yet received a cluster
	// update. With cluster B ignoring cluster C, the system should function as
	// if cluster C was not a child of cluster B (meaning all 4 clusters should
	// be required to get an update).
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: "clusterC",
	}, nil)
	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("an update should not have been written to the update buffer")
	case <-sCtx.Done():
	}

	// This update causes all 4 clusters in the aggregated cluster graph to have
	// received an update, so an update should be written to the update buffer
	// with only a single occurrence of cluster C as a higher priority than
	// cluster D.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: "clusterD",
	}, nil)
	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "clusterC",
		}, {
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "clusterD",
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}

	// Delete A's ref to C by updating A with only child B. Since B still has a
	// reference to C, C's watch should not be canceled, and also an update
	// should correctly be built.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterB"},
	}, nil)

	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "clusterC",
		}, {
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "clusterD",
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestErrorStateWholeTree tests the scenario where the aggregate cluster graph
// exceeds max depth. An error should be written to the update channel.
// Afterward, if a valid response comes in for another cluster, no update should
// be written to the update channel, as the aggregate cluster graph is still in
// the same error state.
func (s) TestErrorStateWholeTree(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster("cluster0")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}

	for i := 0; i <= 15; i++ {
		fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
			ClusterType:             xdsresource.ClusterTypeAggregate,
			ClusterName:             "cluster" + fmt.Sprint(i),
			PrioritizedClusterNames: []string{"cluster" + fmt.Sprint(i+1)},
		}, nil)
		if i == 15 {
			// The 16th iteration will try and create a cluster which exceeds
			// max stack depth and will thus error, so no CDS Watch will be
			// started for the child.
			continue
		}
		_, err = fakeClient.WaitForWatchCluster(ctx)
		if err != nil {
			t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
		}
	}
	select {
	case chu := <-ch.updateChannel:
		if chu.err.Error() != "aggregate cluster graph exceeds max depth" {
			t.Fatalf("Did not receive the expected error, instead received: %v", chu.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for an error to be written to update channel.")
	}

	// Invoke a cluster callback for a node in the graph that rests within the
	// allowed depth. This will cause the system to try and construct a cluster
	// update, and it shouldn't write an update as the aggregate cluster graph
	// is still in an error state. Since the graph continues to stay in an error
	// state, no new error needs to be written to the update buffer as that
	// would be redundant information.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "cluster3",
		PrioritizedClusterNames: []string{"cluster4"},
	}, nil)

	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("an update should not have been written to the update buffer")
	case <-sCtx.Done():
	}

	// Invoke the same cluster update for cluster 15, specifying it has a child
	// cluster16. This should cause an error to be written to the update buffer,
	// as it still exceeds the max depth.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "cluster15",
		PrioritizedClusterNames: []string{"cluster16"},
	}, nil)
	select {
	case chu := <-ch.updateChannel:
		if chu.err.Error() != "aggregate cluster graph exceeds max depth" {
			t.Fatalf("Did not receive the expected error, instead received: %v", chu.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for an error to be written to update channel.")
	}

	// When you remove the child of cluster15 that causes the graph to be in the
	// error state of exceeding max depth, the update should successfully
	// construct and be written to the update buffer.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType: xdsresource.ClusterTypeEDS,
		ClusterName: "cluster15",
	}, nil)

	select {
	case chu := <-ch.updateChannel:
		if diff := cmp.Diff(chu.updates, []xdsresource.ClusterUpdate{{
			ClusterType: xdsresource.ClusterTypeEDS,
			ClusterName: "cluster15",
		}}); diff != "" {
			t.Fatalf("got unexpected cluster update, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// TestNodeChildOfItself tests the scenario where the aggregate cluster graph
// has a node that has child node of itself. The case for this is A -> A, and
// since there is no base cluster (EDS or Logical DNS), no update should be
// written if it tries to build a cluster update.
func (s) TestNodeChildOfItself(t *testing.T) {
	ch, fakeClient := setupTests()
	ch.updateRootCluster("clusterA")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	// Invoke the callback informing the cluster handler that clusterA has a
	// child that it is itself. Due to this child cluster being a duplicate, no
	// watch should be started. Since there are no leaf nodes (i.e. EDS or
	// Logical DNS), no update should be written to the update buffer.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterA"},
	}, nil)
	sCtx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := fakeClient.WaitForWatchCluster(sCtx); err == nil {
		t.Fatal("Watch should not have been started for clusterA")
	}
	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("update should not have been written to update buffer")
	case <-sCtx.Done():
	}

	// Invoke the callback again informing the cluster handler that clusterA has
	// a child that it is itself. Due to this child cluster being a duplicate,
	// no watch should be started. Since there are no leaf nodes (i.e. EDS or
	// Logical DNS), no update should be written to the update buffer.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterA"},
	}, nil)

	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := fakeClient.WaitForWatchCluster(sCtx); err == nil {
		t.Fatal("Watch should not have been started for clusterA")
	}
	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("update should not have been written to update buffer, as clusterB has not received an update yet")
	case <-sCtx.Done():
	}

	// Inform the cluster handler that clusterA now has clusterB as a child.
	// This should not cancel the watch for A, as it is still the root cluster
	// and still has a ref count, not write an update to update buffer as
	// cluster B has not received an update yet, and start a new watch for
	// cluster B as it is not a duplicate.
	fakeClient.InvokeWatchClusterCallback(xdsresource.ClusterUpdate{
		ClusterType:             xdsresource.ClusterTypeAggregate,
		ClusterName:             "clusterA",
		PrioritizedClusterNames: []string{"clusterB"},
	}, nil)

	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if _, err := fakeClient.WaitForCancelClusterWatch(sCtx); err == nil {
		t.Fatal("clusterA should not have been canceled, as it is still the root cluster")
	}

	sCtx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-ch.updateChannel:
		t.Fatal("update should not have been written to update buffer, as clusterB has not received an update yet")
	case <-sCtx.Done():
	}

	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != "clusterB" {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, "clusterB")
	}
}
