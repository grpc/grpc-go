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
	"testing"

	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

const (
	edsService = "EDS Service"
	logicalDNSService = "Logical DNS Service"
	edsService2 = "EDS Service 2"
	logicalDNSService2 = "Logical DNS Service 2"
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
// that it is an EDS Cluster, not a tree, so expectation that update is written to buffer which will be read by CDS LB.
func (s) TestUpdateRootClusterEDSSuccess(t *testing.T) {
	ch, fakeClient := setupTests(t)
	// When you first update the root cluster, it should hit the code path which will start a cluster node for that root.
	// Updating the root cluster logically represents a ping from a ClientConn.
	ch.updateRootCluster(edsService)

	// Starting a cluster node involves communicating with the xdsClient, telling it to watch a cluster.
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}

	// Invoke callback with xds client with a certain clusterUpdate. Due to this cluster update filling out the whole
	// cluster tree, as the cluster is of type EDS and not an aggregate cluster, this should trigger the ClusterHandler
	// to write to the update buffer to update the CDS policy.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)

	select {
		case chu := <-ch.updateChannel:
			if len(chu.chu) != 1 {
				t.Fatal("Cluster Update passed to CDS should only have one update as cluster type EDS")
			}
			if chu.chu[0].ClusterType != xdsclient.ClusterTypeEDS {
				t.Fatalf("ClusterUpdate Type received: %v, ClusterUpdate wanted: %v", chu, xdsclient.ClusterTypeEDS)
			}
			if chu.chu[0].ServiceName != edsService {
				t.Fatalf("ClusterUpdate ServiceName received: %v, ClusterUpdate wanted: %v", chu.chu[0].ServiceName, edsService)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for update from updateChannel.")
	}

}

// This test is the same as the prior test, expect that the cluster type is of LogicalDNS rather than EDS.
func (s) TestUpdateRootClusterLogicalDNSSuccess(t *testing.T) {
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(logicalDNSService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != logicalDNSService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
	}

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)

	select {
		case chu := <-ch.updateChannel:
			if len(chu.chu) != 1 {
				t.Fatal("Cluster Update passed to CDS should only have one update as type EDS")
			}
			if chu.chu[0].ClusterType != xdsclient.ClusterTypeLogicalDNS {
				t.Fatalf("ClusterUpdate Type received: %v, ClusterUpdate wanted: %v", chu, xdsclient.ClusterTypeLogicalDNS)
			}
			if chu.chu[0].ServiceName != logicalDNSService {
				t.Fatalf("ClusterUpdate ServiceName received: %v, ClusterUpdate wanted: %v", chu.chu[0].ServiceName, logicalDNSService)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for update from updateChannel.")
	}
}

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
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)

	// xds client should be called to start a watch for one of the child clusters of the aggregate, which is either an
	// EDS Service or a LogicalDNS. The construction/iteration through the map in handleResp() is nondeterministic, so
	// might start watch for EDS first or Logical DNS first. This does not really matter in terms of the functionality
	// of the clusterHandler.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want either: %v or %v", gotCluster, edsService, logicalDNSService)
		}
	}

	// xds client should also be called to start a watch for the second child cluster.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		if gotCluster != logicalDNSService {
			t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
		}
	}

	// The handleResp() call on the root aggregate cluster will ping the cluster handler to try and construct an update.
	// However, due to the two children having not yet received an update from the xds channel, the update should not
	// successfully build. Thus, there should be nothing in the update channel.

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
	// clusters representing the base nodes of the tree of clusters.
	select {
	case chu := <-ch.updateChannel:
		if len(chu.chu) != 2 {
			t.Fatalf ("Cluster Update passed to CDS should have a length of 2 as it is an aggregate cluster instead of. %v", len(chu.chu))
		}
		// Validate the child names, one child should be edsService and the other logicalDNSService.
		for _, clusterUpdate := range chu.chu  {
			if clusterUpdate.ServiceName != edsService {
				if clusterUpdate.ServiceName != logicalDNSService {
					t.Fatalf("Cluster Update returned to CDS has an unexpected clusterName: %v", clusterUpdate.ServiceName)
				}
			}
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for the cluster update to be written to the update buffer.")
	}
}

// The first set of tests represents "modify" test. These tests will switch the aggregate cluster to one child and one child with change.
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
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
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

	// OKAY, NEW STUFF HERE! I know what I'll do: I'll change LogicalDNS to LogicalDNS2
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService2},
	}, nil)
	// Behavior: deletes first

	// Validation:

	// The cluster update let's the aggregate cluster know that it's children are now edsService and logicalDNSService2,
	// which implies that the aggregateCluster lost it's old logicalDNSService child. Thus, the logicalDNSService child
	// should be deleted.
	// Read from scaled channel (read will happen in fake xds client), should be logicalDNSService
	clusterNameDeleted, err := fakeClient.WaitForCancelClusterWatch(ctx)
	if err != nil {
		t.Fatalf("xdsClient.CancelCDS failed with error: %v", err)
	}
	if clusterNameDeleted != logicalDNSService {
		t.Fatalf("xdsClient.CancelCDS called for cluster %v, want: %v", clusterNameDeleted, logicalDNSService)
	}

	// Behavior: then starts a watch for LogicalDNS2
	clusterNameCreated, err := fakeClient.WaitForWatchCluster(ctx)

	// Validation
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

	// Invoke a callback for the new logicalDNSService2.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService2,
	}, nil)

	// Behavior: This update make every node in the tree of cluster have received an update. Thus, at the end of this callback,
	// when you ping the clusterHandler to try and construct an update, the update should now successfully to update buffer
	// to send back to CDS. This new update should contain the new child of LogicalDNS2.

	select {
	case chu := <-ch.updateChannel:
		if len(chu.chu) != 2 {
			t.Fatalf("Cluster Update passed to CDS should have a length of 2. Instead of: %v", len(chu.chu))
		}
		// Validate the child names, one child should be edsService and the other logicalDNSService.
		for _, clusterUpdate := range chu.chu  {
			if clusterUpdate.ServiceName != edsService {
				if clusterUpdate.ServiceName != logicalDNSService2 {
					t.Fatalf("Cluster Update returned to CDS has an unexpected clusterName: %v", clusterUpdate.ServiceName)
				}
			}
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
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
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
	ch.updateRootCluster(edsService)

	// Reads from the cancel channel, should fiorst be type Aggregate, then arbitrary LogicalDNS and EDS in a certain order




	// After deletion, it should start a watch for the EDS Cluster. The behavior for this EDS Cluster receiving an update
	// from xds client and then successfully writing an update to send back to CDS is already tested in the updateEDS
	// success case.
}










/*
// This test tests whether updating the root aggregate cluster as a new aggregate cluster with less children successfully deletes child and writes update with deleted child.
func (s) TestUpdateRootClusterAggregateThenDeleteChild(t *testing.T) {
	// Put the same thing as I had in UpdateRootClusterAggregateSuccess, expect no validations, then update it and add validations.
	// Due to there being a test for the first update for an aggregate cluster successfully writing to a buffer, there
	// will be no validations in this test until this test gets to same stage (has an aggregate cluster with a child EDS
	// and LogicalDNS as the root node in cluster handler). (ACTUALLY, SHIFT THIS FROM HAVING TWO CHILDREN TO HAVING THREE
	// CHILDREN)
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)

	go func(){
		fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
			ClusterType: xdsclient.ClusterTypeAggregate,
			ServiceName: aggregateClusterService,
			PrioritizedClusterNames: []string{edsService, logicalDNSService},
		}, nil)
	}()

	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)
	// Do we need a seperate ctx here?
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)

	// No need to read from buffer as the update channel will be drained and replaced with most recent update.

	// New logic, which involves invoking the update callback for the root aggregate node, deleting the EDS child.
}

// This test tests whether updating the root aggregate cluster with a changed child successfully writes update with changed child.
func (s) TestUpdateRootClusterAggregateThenUpdateChildToAnAggregate(t *testing.T) {
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)

	go func(){
		fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
			ClusterType: xdsclient.ClusterTypeAggregate,
			ServiceName: aggregateClusterService,
			PrioritizedClusterNames: []string{edsService, logicalDNSService},
		}, nil)
	}()

	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)
	// Do we need a seperate ctx here?
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)

	// No need to read from buffer as the update channel will be drained and replaced with most recent update.

	// New logic, which involves invoking the update callback for the root aggregate node, deleting the LogicalDNS child and
	// replacing it with an aggregate cluster.

	// Go() call?
	// This update adds a second EDS service to the aggregate cluster's children. This should start a watch for the new
	// second EDS service child. Afterward, it should successfully send back an update to CDS (by writing to update buffer)
	// with the new child.
	go func(){
		fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
			ClusterType: xdsclient.ClusterTypeAggregate,
			ServiceName: aggregateClusterService,
			PrioritizedClusterNames: []string{edsService, logicalDNSService, edsService2},
		}, nil)
	}()

	// That update from xdsClient should start a watch for the new second EDS service child.

	// *** Start validation on start watch
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	fakeClient.WaitForWatchCluster(ctx)
	// *** End validation on start watch


	// Shouldn't update, as full nodes have not all received updates.

	// *** Start validation on not writing an update ***

	// *** End validation on not writing an update ***


	// THEN YOU HAVE TO NOW INVOKE A CALLBACK TO UPDATE NEW NODE






	// Should update here as the aggregate cluster's full nodes have all received updates.

	// *** Start validation on updating CDS with new update

	// *** End validation on updating CDS with new update



	// This update deletes EDS service 2 and adds DNS service 2 to the aggregate cluster's children. This should delete
	// EDS Service 2 from the aggregate cluster's children and also start a watch for the new DNS service 2 child cluster.
	// Afterward, it should successfully send back an update to CDS (by writing to update buffer) with the new children.
	go func(){
		fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
			ClusterType: xdsclient.ClusterTypeAggregate,
			ServiceName: aggregateClusterService,
			PrioritizedClusterNames: []string{edsService, logicalDNSService, logicalDNSService2},
		}, nil)
	}()

	// The update from xdsClient should delete EDS Service 2

	// *** Start validation on deleting EDS service 2 from aggregate clusters children.

	// *** End validation on deleting EDS service 2 from aggregate clusters children.



	// The update should also start a watch for new second EDS service child

	// *** Start validation on starting a watch for new second EDS service child.

	// *** End validation on starting a watch for new second EDS service child.



	// Shouldn't update, as full nodes have not all received updates.

	// *** Start validation on not writing an update ***

	// *** End validation on not writing an update ***


	// THEN YOU HAVE TO NOW INVOKE A CALLBACK TO UPDATE NEW NODE



	// Should update here as aggregate cluster's full nodes have all received updates.

	// *** Start validation on updating CDS with new update.

	// *** End validation on updating CDS with new update.

}

// This test tests whether switching the root cluster from an aggregate cluster to a non aggregate cluster successfully
// deletes all nodes in aggregate cluster and also starts a node for the new root. (Will test updating root rather than handleResp callback)
func (s) TestUpdateRootClusterAggregateToEDS(t *testing.T) {

}
*/
