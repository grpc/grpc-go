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
	// How will an external component actually use clusterHandler? I need to create one here that
	// I'll be able to call an API on, which will trigger the sequence of events I had planned
	xdsC := fakeclient.NewClient()
	ch := &clusterHandler{
		// Do I put the test client conn and a fake updateChannel here to verify?
		xdsClient: xdsC, // Now we can control what the xds client does
		updateChannel: make(chan clusterHandlerUpdate, 1), // This is will be how the update channel is created in cds. It will be a seperate channel to the buffer.Unbounded.
	}
	return ch, xdsC
}

// Story test cases

// This happens, then this should happen, then this should happen, etc.

// How will the API be used on the component?

// Input: A logical state of the system, Output: Behavior

func (s) TestX(t *testing.T) {
	// Tell a story with comments, logical input output streams
	// What part of this new feature do I want to test?

	// Setup steps here?

}

// "Guaranteed that when you call watchCluster(), replace client to fake client, guaranteed to give it a clusterName and control
// xds client, control behavior of client, guaranteed that callback will be called with a fixed sequence of updates.

// If this thing happens, then this should happen. Story
// What to wrap in if clause: when you call watchCluster()

// In between: control of xds client to send back a certain update

// then: callback should be called with a fixed sequence of updates

// This test tests that calling watchCluster with clustername x, (take control of the xds client here, xds client will do this)
// callback should be called with a fixed sequence of updates
func (s) TestUpdateRootCluster(t *testing.T) {
	// Story I am telling here: when this happens, this should then happen.
	// Example: call it with cluster x, xdsClient returns this, send update?
}

// Simplest case: the cluster handler receives a cluster name, handler starts a watch for that cluster, xds client returns
// that it is an EDS Cluster, not a tree, so expectation that update is written to buffer which will be read by CDS LB.
func (s) TestUpdateRootClusterEDSSuccess(t *testing.T) {
	// Setup everything, from the xds client to the cluster handler in order to set up the situation
	// () () () sitatuation of the world -> this should then happen
	// Setup this situation of the world, expect that this should happen. "This happening" can be validated in many ways
	// What components are now required in order to set up situation
	// To set up this scenario, you need a very in depth understanding of how the components plug into the system
	// Each component is a logical state machine
	// DNS Resolver - setup enviornment where dns component ()<-(ClientConn) returns errors, and then you test the behavior of the dns component (polling, or I guess polling until the ClientConn stops returning an error)
	// (This is happening) (This is happening) (This is happening state + actions) (This is happening)
	// (This is happening) (This is happening) (This is happening) (This is happening)

	// The situation involves a ping from CDS and also a clusterwatch started on the client and something returned from client
	// Ping is an API call, so you don't need to mock the CDS, as CDS simply calls the API
	// However, you do need this fakeClient as you have to validate that a clusterWatch was started for a specific cluster
	// and you also need to mock the callback call with the cluster update and error. Luckily, the fake client provides
	// that functionality for you.
	ch, fakeClient := setupTests(t)
	// When you first update the root cluster, it should hit == nil codepath which, start a cluster node for that root.
	// ENTRANCE, will cause a bunch of behaviors to happen downstream of entering
	ch.updateRootCluster(edsService) // Logically represents a ping from CDS, no need to mock as you just call API with that string

	// Starting a cluster node involves communicating with the xdsClient, telling it to watch a cluster.
	// c.cancelFunc = xdsClient.WatchCluster(clusterName, c.handleResp)
	// verify here that the xds client actually gets pinged to watch cluster
	// This is the fake xds client call (setupWithWatch)

	// *** Validation for event Watch Cluster***
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx) // Similar to DNS Test, logical wait for x seconds event should happen in that time frame
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}
	// *** end Validation for event Watch Cluster***

	// Invoke callback with xds client with a certain clusterUpdate All the component is doing is taking this and
	// writing it to a buffer, I don't think any validations in CDS logic in watcher happen because it invokes the
	// callback DIRECTLY.
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{ // ENTRANCE
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)

	// Callback for a node should call cluster handler to try and construct an update to send back to CDS, which should
	// work as tree is logically complete (should get written to a buffer)

	// Perhaps verify that there was something written to updateBuffer, by reading from this update channel
	// the same way that CDS would read from it.
	// Since the cluster type is EDS and not aggregate, the update should contain a list with length 1 of cluster updates.
	// Wait, it's literally just the ClusterUpdate the xds client writes that it converted from a proto update from xds.
	// So, this is literally a list of what you tell the fake xds client to invoke callback with, the component logic is
	// simply writing to this buffer.
	/*chuWant := clusterHandlerUpdate{
		[]xdsclient.ClusterUpdate{{
			ClusterType: xdsclient.ClusterTypeEDS,
			ServiceName: "EDS Service",
			}},
		nil,
	}*/

	// ***** Validating behavior of writing a cluster handler update to an update channel
	chu := <-ch.updateChannel // Should this also wait on a timeout?
	// Should I move these equality things to a separate method?
	// Since the cluster type is EDS and not aggregate, the update should contain a list with length 1 of cluster updates.
	if len(chu.chu) != 1 {
		t.Fatal("Cluster Update passed to CDS should only have one update as type EDS")
	}
	if chu.chu[0].ClusterType != xdsclient.ClusterTypeEDS {
		t.Fatalf("ClusterUpdate Type received: %v, ClusterUpdate wanted: %v", chu, xdsclient.ClusterTypeEDS)
	}
	if chu.chu[0].ServiceName != edsService {
		t.Fatalf("ClusterUpdate ServiceName received: %v, ClusterUpdate wanted: %v", chu.chu[0].ServiceName, edsService)
	}
	// ***** End validating behavior of writing a cluster handler update to an update channel


	// Change the root to a Logical DNS?
}




// Another simple case without all the comments, see if I can generalize
func (s) TestUpdateRootClusterLogicalDNSSuccess(t *testing.T) {
	// Logical setup of components, all you need is the component tested and a fake client
	ch, fakeClient := setupTests(t)
	// First entrance function, we've constructed the clusterHandler from setup, represents a
	// ping from ClientConn
	ch.updateRootCluster(logicalDNSService)

	// Validate what that entrance function causes, which is this event WatchCluster
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != logicalDNSService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
	}
	// End validation
	// Even though code is held in xdsClient, it is an entrance function to the same memory
	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)

	// That entrance will cause downstream behavior, which is the node telling the cluster handler
	// to try an update, then building an update, then writing to a buffer to pass to CDS, which is
	// what we can validate here.
	// *** Start validation on downstream behavior
	// This definetly needs to wait on a timeout as this can be a forever wait
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
	// *** End validation on downstream behavior
}

// You can move all the functionality ^^^ upward
// When I come back, RELEARN THIS LOGIC AND PERHAPS TEST IT STEP BY STEP TO SEE IF IT ALL WORKS
func (s) TestUpdateRootClusterAggregateSuccess(t *testing.T) {
	ch, fakeClient := setupTests(t)
	ch.updateRootCluster(aggregateClusterService)

	// No need for validation, as had done that previously
	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	gotCluster, err := fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != aggregateClusterService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, aggregateClusterService)
	}
	// Perhaps move everything up here ^^^ into a function, as this is very reusable

	go func(){fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeAggregate,
		ServiceName: aggregateClusterService,
		PrioritizedClusterNames: []string{edsService, logicalDNSService},
	}, nil)}()

	// The watch telling the cluster (through calling a callback) that the cluster type is an aggregate cluster
	// should cause some downstream behavior.
	// In the LogicalDNS case, the only thing that happened was you got a successful update, which we verified
	// manually through a bunch of equality checks

	// In the aggregate case, you also call createClusterNode for the children, you should expect another watchCluster call
	// on the xds client (this might require some tweaking of the current implementation of the xds client)

	// xds client should be called to start a watch for EDS Service
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout) // How do we fix this problem?
	defer ctxCancel()
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx) // I'm pretty sure what the problem boils down to is you can't
	// have a wait as it doesn't come step by step, it calls one, will ping fake client, will call another, will ping fake client again.
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != edsService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, edsService)
	}

	print("after EDS watch")

	// xds client should also be called to start a watch for Logical DNS Service
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestTimeout) // How do we fix this problem?
	defer ctxCancel()
	gotCluster, err = fakeClient.WaitForWatchCluster(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchCDS failed with error: %v", err)
	}
	if gotCluster != logicalDNSService {
		t.Fatalf("xdsClient.WatchCDS called for cluster: %v, want: %v", gotCluster, logicalDNSService)
	}

	print("after DNS watch")

	// ** Start validation for no update being written to buffer

	// In this case, you also ping cluster handler to try and construct an update. However, it won't succesfully
	// build an update, so you should make sure there is nothing put in the update channel.

	// How do I send test an invalid codepath without having the tests take forever? - The answer is defaultTestShortTimeout

	// Make sure that invoking the callback doesn't cause an update to be written to an update buffer (to send to CDS)
	// make sure it times out - what Menghan mentioned, how do we make sure it doesn't take a bunch of testing time?

	// After not building an update, wait this doesn't happen yet

	// Make sure that the handler doesn't succesfully build a cluster handler which it will then put on the update buffer
	// to send to CDS

	// Problem statement to figure out: How to use this ctx in a way that allows Test to check if event happened in
	// it's shortened time frame

	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout) // We can use time here as well
	defer ctxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-ctx.Done(): // Event didn't happen, sweet
	}

	// Rather than [ event     ] it's [] <- in a smaller time frame, event should not happen

	// ** End validation for no update being written to buffer



	// Send callback for EDS - logically from xds client

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeEDS,
		ServiceName: edsService,
	}, nil)

	// Will Ping Cluster Handler, still won't successfully build an update
	ctx, ctxCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ctxCancel()

	select {
	case <-ch.updateChannel:
		t.Fatal("Cluster Handler wrote an update to updateChannel when it shouldn't have, as each node in the full cluster tree has not yet received an update")
	case <-ctx.Done(): // Event didn't happen, sweet
	}

	// Send callback for Logical DNS - logically from xds client

	fakeClient.InvokeWatchClusterCallback(xdsclient.ClusterUpdate{
		ClusterType: xdsclient.ClusterTypeLogicalDNS,
		ServiceName: logicalDNSService,
	}, nil)

	// Will Ping Cluster Handler, will finally successfully build an update
	// Since this cluster is an aggregate cluster comprised of two children,
	// the returned update should be length 2.
	chu := <-ch.updateChannel
	if len(chu.chu) != 2 {
		t.Fatal("Cluster Update passed to CDS should have a length of 2 as it is an aggregate cluster.")
	}

	// Should I also delete the root node up here?, or try a shift of the root node to CDS
}











// Unit tests, not end to end, missing end to end tests adding end to end tests for xds. Make a real grpc client conn, verify that rpc are routed to same backends.
// Integration tests: grpc client, google cloud, talk to real traffic director for configuration.

// Let's say we have a situation where this component is this, this component is this, etc.
// Behavior where this thing is this, this thing is this, BEHAVIOR OF COMPONENT in this situation

// A component that you've coded, the picture of how it fits into logical system, then think of the possibilities of the state of the other components