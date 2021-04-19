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
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"testing"
)

const (
	edsService = "EDS Service"
	logicalDNSService = "Logical DNS Service"
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

	chu := <-ch.updateChannel // TODO: Should this also wait on a timeout?
	// Should I move these equality checks to a separate method? Seems very boilerplatey.
	// Since the cluster type is EDS and not aggregate, the update should contain a list with length 1 of cluster updates.
	if len(chu.chu) != 1 {
		t.Fatal("Cluster Update passed to CDS should only have one update as cluster type EDS")
	}
	if chu.chu[0].ClusterType != xdsclient.ClusterTypeEDS {
		t.Fatalf("ClusterUpdate Type received: %v, ClusterUpdate wanted: %v", chu, xdsclient.ClusterTypeEDS)
	}
	if chu.chu[0].ServiceName != edsService {
		t.Fatalf("ClusterUpdate ServiceName received: %v, ClusterUpdate wanted: %v", chu.chu[0].ServiceName, edsService)
	}

	// Change the root to a Logical DNS? How long should the "story" be?
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

	// TODO: wait on timeout?
	chu := <-ch.updateChannel
	if len(chu.chu) != 1 {
		t.Fatal("Cluster Update passed to CDS should only have one update as type EDS")
	}
	if chu.chu[0].ClusterType != xdsclient.ClusterTypeLogicalDNS {
		t.Fatalf("ClusterUpdate Type received: %v, ClusterUpdate wanted: %v", chu, xdsclient.ClusterTypeLogicalDNS)
	}
	if chu.chu[0].ServiceName != logicalDNSService {
		t.Fatalf("ClusterUpdate ServiceName received: %v, ClusterUpdate wanted: %v", chu.chu[0].ServiceName, logicalDNSService)
	}
}

// You can move all the functionality ^^^ upward
// When I come back, RELEARN THIS LOGIC AND PERHAPS TEST IT STEP BY STEP TO SEE IF IT ALL WORKS
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
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}

	print("after EDS watch")

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

	print("after DNS watch")

	// The handleResp() call on the root aggregate cluster will ping the cluster handler to try and construct an update.
	// However, due to the two children having not yet received an update from the xds channel, the update should not
	// successfully build. Thus, there should be nothing in the update channel.

	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ctxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-ctx.Done():


	// Send callback for the EDS child cluster.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)

	// EDS child cluster will ping the Cluster Handler, to try an update, which still won't successfully build as the
	// LogicalDNS child of the root aggregate cluster has not yet received and handled an update.
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ctxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-ctx.Done():
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











// Unit tests, not end to end, missing end to end tests adding end to end tests for xds. Make a real grpc client conn, verify that rpc are routed to same backends.
// Integration tests: grpc client, google cloud, talk to real traffic director for configuration.

// Let's say we have a situation where this component is this, this component is this, etc.
// Behavior where this thing is this, this thing is this, BEHAVIOR OF COMPONENT in this situation

// A component that you've coded, the picture of how it fits into logical system, then think of the possibilities of the state of the other components