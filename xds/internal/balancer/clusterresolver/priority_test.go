// +build go1.12

/*
 *
 * Copyright 2019 gRPC authors.
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

package clusterresolver

import (
	"context"
	"testing"
	"time"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/testutils"
)

// When a high priority is ready, adding/removing lower locality doesn't cause
// changes.
//
// Init 0 and 1; 0 is up, use 0; add 2, use 0; remove 2, use 0.
func (s) TestEDSPriority_HighPriorityReady(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	// Two localities, with priorities [0, 1], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Add p2, it shouldn't cause any updates.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	clab2.AddLocality(testSubZones[2], 1, 2, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(defaultTestShortTimeout):
	}

	// Remove p2, no updates.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab3.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab3.Build()), nil)

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(defaultTestShortTimeout):
	}
}

// Lower priority is used when higher priority is not ready.
//
// Init 0 and 1; 0 is up, use 0; 0 is down, 1 is up, use 1; add 2, use 1; 1 is
// down, use 2; remove 2, use 1.
func (s) TestEDSPriority_SwitchPriority(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	// Two localities, with priorities [0, 1], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is ready.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}

	// Turn down 0, 1 is used.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Add p2, it shouldn't cause any updates.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	clab2.AddLocality(testSubZones[2], 1, 2, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(defaultTestShortTimeout):
	}

	// Turn down 1, use 2
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}

	// Remove 2, use 1.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab3.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab3.Build()), nil)

	// p2 SubConns are removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}

	// Should get an update with 1's old picker, to override 2's old picker.
	if err := testErrPickerFromCh(cc.NewPickerCh, balancer.ErrTransientFailure); err != nil {
		t.Fatal(err)
	}

}

// Add a lower priority while the higher priority is down.
//
// Init 0 and 1; 0 and 1 both down; add 2, use 2.
func (s) TestEDSPriority_HigherDownWhileAddingLower(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with different priorities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Turn down 0, 1 is used.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	// Turn down 1, pick should error.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// Test pick failure.
	if err := testErrPickerFromCh(cc.NewPickerCh, balancer.ErrTransientFailure); err != nil {
		t.Fatal(err)
	}

	// Add p2, it should create a new SubConn.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	clab2.AddLocality(testSubZones[2], 1, 2, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)
	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}

}

// When a higher priority becomes available, all lower priorities are closed.
//
// Init 0,1,2; 0 and 1 down, use 2; 0 up, close 1 and 2.
func (s) TestEDSPriority_HigherReadyCloseAllLower(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with priorities [0,1,2], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	clab1.AddLocality(testSubZones[2], 1, 2, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Turn down 0, 1 is used.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	// Turn down 1, 2 is used.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}

	// When 0 becomes ready, 0 should be used, 1 and 2 should all be closed.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	var (
		scToRemove    []balancer.SubConn
		scToRemoveMap = make(map[balancer.SubConn]struct{})
	)
	// Each subconn is removed twice. This is OK in production, but it makes
	// testing harder.
	//
	// The sub-balancer to be closed is priority's child, clusterimpl, who has
	// weightedtarget as children.
	//
	// - When clusterimpl is removed from priority's balancergroup, all its
	// subconns are removed once.
	// - When clusterimpl is closed, it closes weightedtarget, and this
	// weightedtarget's balancer removes all the same subconns again.
	for i := 0; i < 4; i++ {
		// We expect 2 subconns, so we recv from channel 4 times.
		scToRemoveMap[<-cc.RemoveSubConnCh] = struct{}{}
	}
	for sc := range scToRemoveMap {
		scToRemove = append(scToRemove, sc)
	}

	// sc1 and sc2 should be removed.
	//
	// With localities caching, the lower priorities are closed after a timeout,
	// in goroutines. The order is no longer guaranteed.
	if !(cmp.Equal(scToRemove[0], sc1, cmp.AllowUnexported(testutils.TestSubConn{})) &&
		cmp.Equal(scToRemove[1], sc2, cmp.AllowUnexported(testutils.TestSubConn{}))) &&
		!(cmp.Equal(scToRemove[0], sc2, cmp.AllowUnexported(testutils.TestSubConn{})) &&
			cmp.Equal(scToRemove[1], sc1, cmp.AllowUnexported(testutils.TestSubConn{}))) {
		t.Errorf("RemoveSubConn, want [%v, %v], got %v", sc1, sc2, scToRemove)
	}

	// Test pick with 0.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}
}

// At init, start the next lower priority after timeout if the higher priority
// doesn't get ready.
//
// Init 0,1; 0 is not ready (in connecting), after timeout, use 1.
func (s) TestEDSPriority_InitTimeout(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func() func() {
		old := priority.DefaultPriorityInitTimeout
		priority.DefaultPriorityInitTimeout = testPriorityInitTimeout
		return func() {
			priority.DefaultPriorityInitTimeout = old
		}
	}()()

	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with different priorities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Keep 0 in connecting, 1 will be used after init timeout.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// Make sure new SubConn is created before timeout.
	select {
	case <-time.After(testPriorityInitTimeout * 3 / 4):
	case <-cc.NewSubConnAddrsCh:
		t.Fatalf("Got a new SubConn too early (Within timeout). Expect a new SubConn only after timeout")
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}
}

// Add localities to existing priorities.
//
//  - start with 2 locality with p0 and p1
//  - add localities to existing p0 and p1
func (s) TestEDSPriority_MultipleLocalities(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with different priorities, each with one backend.
	clab0 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab0.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab0.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab0.Build()), nil)
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}

	// Turn down p0 subconns, p1 subconns will be created.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Reconnect p0 subconns, p1 subconn will be closed.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}

	// Add two localities, with two priorities, with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	clab1.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:3], nil)
	clab1.AddLocality(testSubZones[3], 1, 1, testEndpointAddrs[3:4], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only two p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0, sc2}); err != nil {
		t.Fatal(err)
	}

	// Turn down p0 subconns, p1 subconns will be created.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	sc3 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	sc4 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc3, sc4}); err != nil {
		t.Fatal(err)
	}
}

// EDS removes all localities, and re-adds them.
func (s) TestEDSPriority_RemovesAllLocalities(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func() func() {
		old := priority.DefaultPriorityInitTimeout
		priority.DefaultPriorityInitTimeout = testPriorityInitTimeout
		return func() {
			priority.DefaultPriorityInitTimeout = old
		}
	}()()

	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with different priorities, each with one backend.
	clab0 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab0.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab0.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab0.Build()), nil)
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}

	// Remove all priorities.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	// p0 subconn should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	<-cc.RemoveSubConnCh // Drain the duplicate subconn removed.
	if !cmp.Equal(scToRemove, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc0, scToRemove)
	}

	// time.Sleep(time.Second)

	// Test pick return TransientFailure.
	if err := testErrPickerFromCh(cc.NewPickerCh, priority.ErrAllPrioritiesRemoved); err != nil {
		t.Fatal(err)
	}

	// Re-add two localities, with previous priorities, but different backends.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[3:4], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)
	addrs01 := <-cc.NewSubConnAddrsCh
	if got, want := addrs01[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc01 := <-cc.NewSubConnCh

	// Don't send any update to p0, so to not override the old state of p0.
	// Later, connect to p1 and then remove p1. This will fallback to p0, and
	// will send p0's old picker if they are not correctly removed.

	// p1 will be used after priority init timeout.
	addrs11 := <-cc.NewSubConnAddrsCh
	if got, want := addrs11[0].Addr, testEndpointAddrs[3]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc11 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc11, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc11, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc11}); err != nil {
		t.Fatal(err)
	}

	// Remove p1 from EDS, to fallback to p0.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab3.Build()), nil)

	// p1 subconn should be removed.
	scToRemove1 := <-cc.RemoveSubConnCh
	<-cc.RemoveSubConnCh // Drain the duplicate subconn removed.
	if !cmp.Equal(scToRemove1, sc11, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc11, scToRemove1)
	}

	// Test pick return TransientFailure.
	if err := testErrPickerFromCh(cc.NewPickerCh, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatal(err)
	}

	// Send an ready update for the p0 sc that was received when re-adding
	// localities to EDS.
	edsb.UpdateSubConnState(sc01, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc01, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc01}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(defaultTestShortTimeout):
	}
}

// Test the case where the high priority contains no backends. The low priority
// will be used.
func (s) TestEDSPriority_HighPriorityNoEndpoints(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with priorities [0, 1], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Remove addresses from priority 0, should use p1.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, nil, nil)
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)
	// p0 will remove the subconn, and ClientConn will send a sc update to
	// shutdown.
	scToRemove := <-cc.RemoveSubConnCh
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh

	// p1 is ready.
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}
}

// Test the case where the high priority contains no healthy backends. The low
// priority will be used.
func (s) TestEDSPriority_HighPriorityAllUnhealthy(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// Two localities, with priorities [0, 1], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Set priority 0 endpoints to all unhealthy, should use p1.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], &testutils.AddLocalityOptions{
		Health: []corepb.HealthStatus{corepb.HealthStatus_UNHEALTHY},
	})
	clab2.AddLocality(testSubZones[1], 1, 1, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)
	// p0 will remove the subconn, and ClientConn will send a sc update to
	// transient failure.
	scToRemove := <-cc.RemoveSubConnCh
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh

	// p1 is ready.
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}
}

// Test the case where the first and only priority is removed.
func (s) TestEDSPriority_FirstPriorityRemoved(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func() func() {
		old := priority.DefaultPriorityInitTimeout
		priority.DefaultPriorityInitTimeout = testPriorityInitTimeout
		return func() {
			priority.DefaultPriorityInitTimeout = old
		}
	}()()

	_, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()
	// One localities, with priorities [0], each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	// Remove the only localities.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := cc.WaitForErrPicker(ctx); err != nil {
		t.Fatal(err)
	}
}

// Watch resources from EDS and DNS, with EDS as the higher priority. Lower
// priority is used when higher priority is not ready.
func (s) TestFallbackToDNS(t *testing.T) {
	const testDNSEndpointAddr = "3.1.4.1:5"
	// dnsTargetCh, dnsCloseCh, resolveNowCh, dnsR, cleanup := setupDNS()
	dnsTargetCh, _, resolveNowCh, dnsR, cleanupDNS := setupDNS()
	defer cleanupDNS()
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	if err := edsb.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			DiscoveryMechanisms: []DiscoveryMechanism{
				{
					Type:    DiscoveryMechanismTypeEDS,
					Cluster: testClusterName,
				},
				{
					Type:        DiscoveryMechanismTypeLogicalDNS,
					DNSHostname: testDNSTarget,
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer ctxCancel()
	select {
	case target := <-dnsTargetCh:
		if diff := cmp.Diff(target, resolver.Target{Scheme: "dns", Endpoint: testDNSTarget}); diff != "" {
			t.Fatalf("got unexpected DNS target to watch, diff (-got, +want): %v", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for building DNS resolver")
	}

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	// Also send a DNS update, because the balancer needs both updates from all
	// resources to move on.
	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: testDNSEndpointAddr}}})

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is ready.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc0}); err != nil {
		t.Fatal(err)
	}

	// Turn down 0, p1 (DNS) will be used.
	edsb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// The transient failure above should not trigger a re-resolve to the DNS
	// resolver. Need to read to clear the channel, to avoid potential deadlock
	// writing to the channel later.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer shortCancel()
	select {
	case <-resolveNowCh:
		t.Fatal("unexpected re-resolve trigger by transient failure from EDS endpoint")
	case <-shortCtx.Done():
	}

	// The addresses used to create new SubConn should be the DNS endpoint.
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testDNSEndpointAddr; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// Turn down the DNS endpoint, this should trigger an re-resolve in the DNS
	// resolver.
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// The transient failure above should trigger a re-resolve to the DNS
	// resolver. Need to read to clear the channel, to avoid potential deadlock
	// writing to the channel later.
	select {
	case <-resolveNowCh:
	case <-ctx.Done():
		t.Fatal("Timed out waiting for re-resolve")
	}
}
