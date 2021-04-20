/*
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
	// Does this need a go behind it?
	go func(){fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)}()

	// The xdsClient telling the clusterNode that the cluster type is an aggregate cluster which will cause a lot of
	// downstream behavior. For a cluster type that isn't an aggregate, the behavior is simple. The clusterNode will
	// simply get a successful update, which will then ping the clusterHandler which will successfully build an update
	// to send to the CDS policy. In the aggregate cluster case, the handleResp callback must also start watches for
	// the aggregate cluster's children.

	// xds client should be called to start a watch for the first? child cluster, which is an EDS Service.
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}

	// xds client should also be called to start a watch for the second child cluster, which is a Logical DNS Service.
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != logicalDNSService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
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

	// Send callback for Logical DNS child cluster.

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)

	// Will Ping Cluster Handler, which will finally successfully build an update as all nodes in the tree of clusters
	// have received an update.  Since this cluster is an aggregate cluster comprised of two children, the returned update
	// should be length 2, as the xds cluster resolver LB policy only cares about the full list of LogicalDNS and EDS
	// clusters representing the base nodes of the tree of clusters.
	chu := <-ch.updateChannel
	if len(chu.chu) != 2 {
		t.Fatal("Cluster Update passed to CDS should have a length of 2 as it is an aggregate cluster.")
	}

	// Should I also delete the root node up here? or try a shift of the root node to CDS, similar question to first test
	// case, what should the scope of a test case be?
}

// From this aggregate cluster, branch it off into two (or three) more tests

// The first set of tests represents "modify" test. These tests will switch the aggregate cluster to one child and one child with change.

// The second set of tests represents the "delete" test. For this test I will have the root aggregate cluster be changed to an EDS cluster, which should
// delete the root aggregate cluster.

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

}*/
