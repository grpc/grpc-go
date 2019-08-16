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

package edsbalancer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
)

// Init 0; 0 is up, use 0; 0 is down, use 0; 0 is up, use 0.
func TestPriorityBalancerGroup_one(t *testing.T) {
	cc := newTestClientConn(t)
	pbg := newPriorityBalancerGroup(cc, nil)

	pbg.add(testBalancerIDs[0], 1, rrBuilder)
	pbg.handleResolvedAddrs(testBalancerIDs[0], testBackendAddrs[0:1])

	pbg.start()

	sc1 := <-cc.newSubConnCh
	pbg.handleSubConnStateChange(sc1, connectivity.Connecting)
	pbg.handleSubConnStateChange(sc1, connectivity.Ready)

	// Test pick with one backend.
	p1 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc1) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc1)
		}
	}

	pbg.handleSubConnStateChange(sc1, connectivity.TransientFailure)
	// Test pick with error.
	p2 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		if _, _, err := p2.Pick(context.Background(), balancer.PickOptions{}); err != balancer.ErrTransientFailure {
			t.Fatalf("want pick error %v, got %v", balancer.ErrTransientFailure, err)
		}
	}

	pbg.handleSubConnStateChange(sc1, connectivity.Ready)
	p3 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p3.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc1) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc1)
		}
	}
}

// When a high priority is ready, adding/removing lower locality doesn't cause
// changes.
//
// Init 0 and 1; 0 is up, use 0; add 2, use 0; remove 2, use 0.
func TestPriorityBalancerGroup_HighPriorityReady(t *testing.T) {
	cc := newTestClientConn(t)
	var (
		pbgs  []*priorityBalancerGroup
		lower *priorityBalancerGroup
	)
	// Create priorities [0,1], where 0->1.
	for i := 1; i >= 0; i-- {
		pbg := newPriorityBalancerGroup(cc, nil)
		pbg.add(testBalancerIDs[i], 1, rrBuilder)
		pbg.handleResolvedAddrs(testBalancerIDs[i], testBackendAddrs[i:i+1])

		pbgs = append([]*priorityBalancerGroup{pbg}, pbgs...)
		pbg.setNext(lower)
		lower = pbg
	}

	// Start priority 0.
	lower.start()

	addrs0 := <-cc.newSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.newSubConnCh
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Connecting)
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Ready)

	// Test pick with one backend.
	p1 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc0) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc0)
		}
	}

	// Add p2, it shouldn't cause any udpates.
	p := 2
	pbg := newPriorityBalancerGroup(cc, nil)
	pbg.add(testBalancerIDs[p], 1, rrBuilder)
	pbg.handleResolvedAddrs(testBalancerIDs[p], testBackendAddrs[p:p+1])
	pbgs = append(pbgs, pbg)
	pbgs[p-1].setNext(pbg)

	select {
	case <-cc.newPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.newSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.removeSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}

	// Remove p2, no updates.
	pbgs[p-1].setNext(nil)
	pbg.close()

	select {
	case <-cc.newPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.newSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.removeSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}
}

// Lower priority is used when higher priority is not ready.
//
// Init 0 and 1; 0 is up, use 0; 0 is down, 1 is up, use 1; add 2, use 1; 1 is
// down, use 2; remove 2, use 1.
func TestPriorityBalancerGroup_SwitchPriority(t *testing.T) {
	cc := newTestClientConn(t)
	var (
		pbgs  []*priorityBalancerGroup
		lower *priorityBalancerGroup
	)
	// Create priorities [0,1], where 0->1.
	for i := 1; i >= 0; i-- {
		pbg := newPriorityBalancerGroup(cc, nil)
		pbg.add(testBalancerIDs[i], 1, rrBuilder)
		pbg.handleResolvedAddrs(testBalancerIDs[i], testBackendAddrs[i:i+1])

		pbgs = append([]*priorityBalancerGroup{pbg}, pbgs...)
		pbg.setNext(lower)
		lower = pbg
	}

	// Start priority 0.
	lower.start()

	addrs0 := <-cc.newSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.newSubConnCh
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Connecting)
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Ready)

	// Test pick with one backend.
	p0 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p0.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc0) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc0)
		}
	}

	// Turn down 0, 1 is used.
	pbgs[0].handleSubConnStateChange(sc0, connectivity.TransientFailure)
	addrs1 := <-cc.newSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.newSubConnCh
	pbgs[1].handleSubConnStateChange(sc1, connectivity.Connecting)
	pbgs[1].handleSubConnStateChange(sc1, connectivity.Ready)

	// Test pick with 1.
	p1 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc1) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc1)
		}
	}

	// Add p2, it shouldn't cause any udpates.
	p := 2
	pbg := newPriorityBalancerGroup(cc, nil)
	pbg.add(testBalancerIDs[p], 1, rrBuilder)
	pbg.handleResolvedAddrs(testBalancerIDs[p], testBackendAddrs[p:p+1])
	pbgs = append(pbgs, pbg)
	pbgs[p-1].setNext(pbg)

	select {
	case <-cc.newPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.newSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.removeSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}

	// Turn down 1, use 2
	pbgs[1].handleSubConnStateChange(sc1, connectivity.TransientFailure)
	addrs2 := <-cc.newSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.newSubConnCh
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Connecting)
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Ready)

	// Test pick with 2.
	p2 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p2.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc2) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc2)
		}
	}

	// Remove 2, use 1.
	pbgs[1].setNext(nil)
	pbgs[2].close()

	scToRemove := <-cc.removeSubConnCh
	if !reflect.DeepEqual(scToRemove, sc2) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}

	// Should get an update with 1's old picker, to override 2's old picker.
	p3 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		if _, _, err := p3.Pick(context.Background(), balancer.PickOptions{}); err != balancer.ErrTransientFailure {
			t.Fatalf("want pick error %v, got %v", balancer.ErrTransientFailure, err)
		}
	}
}

// Add a lower priority while the higher priority is down.
//
// Init 0 and 1; 0 and 1 both down; add 2, use 2.
func TestPriorityBalancerGroup_HigherDownWhileAddingLower(t *testing.T) {
	cc := newTestClientConn(t)
	var (
		pbgs  []*priorityBalancerGroup
		lower *priorityBalancerGroup
	)
	// Create priorities [0,1], where 0->1.
	for i := 1; i >= 0; i-- {
		pbg := newPriorityBalancerGroup(cc, nil)
		pbg.add(testBalancerIDs[i], 1, rrBuilder)
		pbg.handleResolvedAddrs(testBalancerIDs[i], testBackendAddrs[i:i+1])

		pbgs = append([]*priorityBalancerGroup{pbg}, pbgs...)
		pbg.setNext(lower)
		lower = pbg
	}

	// Start priority 0.
	lower.start()

	addrs0 := <-cc.newSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.newSubConnCh

	// Turn down 0, 1 is used.
	pbgs[0].handleSubConnStateChange(sc0, connectivity.TransientFailure)
	addrs1 := <-cc.newSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.newSubConnCh
	pbgs[1].handleSubConnStateChange(sc1, connectivity.TransientFailure)

	// Test pick failure.
	pFail := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		if _, _, err := pFail.Pick(context.Background(), balancer.PickOptions{}); err != balancer.ErrTransientFailure {
			t.Fatalf("want pick error %v, got %v", balancer.ErrTransientFailure, err)
		}
	}

	// Add p2, it should create a new SubConn.
	p := 2
	pbg := newPriorityBalancerGroup(cc, nil)
	pbg.add(testBalancerIDs[p], 1, rrBuilder)
	pbg.handleResolvedAddrs(testBalancerIDs[p], testBackendAddrs[p:p+1])
	pbgs = append(pbgs, pbg)
	pbgs[p-1].setNext(pbg)

	addrs2 := <-cc.newSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.newSubConnCh
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Connecting)
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Ready)

	// Test pick with 2.
	p2 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p2.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc2) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc2)
		}
	}
}

// When a higher priority becomes available, all lower priorities are closed.
//
// Init 0,1,2; 0 and 1 down, use 2; 0 up, close 1 and 2.
func TestPriorityBalancerGroup_HigherReadyCloseAllLower(t *testing.T) {
	defer time.Sleep(10 * time.Millisecond)
	cc := newTestClientConn(t)
	var (
		pbgs  []*priorityBalancerGroup
		lower *priorityBalancerGroup
	)
	// Create priorities [0,1,2], where 0->1->2.
	for i := 2; i >= 0; i-- {
		pbg := newPriorityBalancerGroup(cc, nil)
		pbg.add(testBalancerIDs[i], 1, rrBuilder)
		pbg.handleResolvedAddrs(testBalancerIDs[i], testBackendAddrs[i:i+1])

		pbgs = append([]*priorityBalancerGroup{pbg}, pbgs...)
		pbg.setNext(lower)
		lower = pbg
	}

	// Start priority 0.
	lower.start()

	addrs0 := <-cc.newSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.newSubConnCh

	// Turn down 0, 1 is used.
	pbgs[0].handleSubConnStateChange(sc0, connectivity.TransientFailure)
	addrs1 := <-cc.newSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.newSubConnCh
	// Turn down 1, 2 is used.
	pbgs[1].handleSubConnStateChange(sc1, connectivity.TransientFailure)
	addrs2 := <-cc.newSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.newSubConnCh
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Connecting)
	pbgs[2].handleSubConnStateChange(sc2, connectivity.Ready)

	// Test pick with 2.
	p2 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p2.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc2) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc2)
		}
	}

	// When 0 becomes ready, 0 should be used, 1 and 2 should all be closed.
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Ready)

	// sc1 and sc2 should be removed.
	//
	// With localities caching, the lower priorities are closed after a timeout,
	// in goroutines. The order is no longer guaranteed.
	scToRemove := []balancer.SubConn{<-cc.removeSubConnCh, <-cc.removeSubConnCh}
	if !(reflect.DeepEqual(scToRemove[0], sc1) && reflect.DeepEqual(scToRemove[1], sc2)) &&
		!(reflect.DeepEqual(scToRemove[0], sc2) && reflect.DeepEqual(scToRemove[1], sc1)) {
		t.Errorf("RemoveSubConn, want [%v, %v], got %v", sc1, sc2, scToRemove)
	}

	// Test pick with 0.
	p0 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p0.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc0) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc0)
		}
	}
}

// At init, start the next lower priority after timeout if the higher priority
// doesn't get ready.
//
// Init 0,1; 0 is not ready (in connecting), after timeout, use 1.
func TestPriorityBalancerGroup_InitTimeout(t *testing.T) {
	cc := newTestClientConn(t)
	var (
		pbgs  []*priorityBalancerGroup
		lower *priorityBalancerGroup
	)
	// Create priorities [0,1], where 0->1.
	for i := 1; i >= 0; i-- {
		pbg := newPriorityBalancerGroup(cc, nil)
		pbg.add(testBalancerIDs[i], 1, rrBuilder)
		pbg.handleResolvedAddrs(testBalancerIDs[i], testBackendAddrs[i:i+1])

		pbgs = append([]*priorityBalancerGroup{pbg}, pbgs...)
		pbg.setNext(lower)
		lower = pbg
	}

	// Start priority 0.
	const testInitTimeout = time.Second
	lower.initTimeout = testInitTimeout
	lower.start()

	addrs0 := <-cc.newSubConnAddrsCh
	if got, want := addrs0[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.newSubConnCh

	// Keep 0 in connecting, 1 will be used after init timeout.
	pbgs[0].handleSubConnStateChange(sc0, connectivity.Connecting)

	// Make sure new SubConn is created before timeout.
	select {
	case <-time.After(testInitTimeout * 3 / 4):
	case <-cc.newSubConnAddrsCh:
		t.Fatalf("Got a new SubConn too early (Within timeout). Expect a new SubConn only after timeout")
	}

	addrs1 := <-cc.newSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.newSubConnCh

	pbgs[1].handleSubConnStateChange(sc1, connectivity.Connecting)
	pbgs[1].handleSubConnStateChange(sc1, connectivity.Ready)

	// Test pick with 1.
	p1 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc1) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc1)
		}
	}
}
