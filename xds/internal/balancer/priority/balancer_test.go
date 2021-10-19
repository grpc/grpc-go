/*
 *
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
 *
 */

package priority

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/hierarchy"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
	"google.golang.org/grpc/xds/internal/testutils"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var testBackendAddrStrs []string

const (
	testBackendAddrsCount = 12
	testRRBalancerName    = "another-round-robin"
)

type anotherRR struct {
	balancer.Builder
}

func (*anotherRR) Name() string {
	return testRRBalancerName
}

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
	balancer.Register(&anotherRR{Builder: balancer.Get(roundrobin.Name)})
}

func subConnFromPicker(t *testing.T, p balancer.Picker) func() balancer.SubConn {
	return func() balancer.SubConn {
		scst, err := p.Pick(balancer.PickInfo{})
		if err != nil {
			t.Fatalf("unexpected error from picker.Pick: %v", err)
		}
		return scst.SubConn
	}
}

// When a high priority is ready, adding/removing lower locality doesn't cause
// changes.
//
// Init 0 and 1; 0 is up, use 0; add 2, use 0; remove 2, use 0.
func (s) TestPriority_HighPriorityReady(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two children, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Add p2, it shouldn't cause any updates.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-2"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-2": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1", "child-2"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn: %s", sc)
	case sc := <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn: %v", sc)
	case <-time.After(time.Millisecond * 100):
	}

	// Remove p2, no updates.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}
}

// Lower priority is used when higher priority is not ready.
//
// Init 0 and 1; 0 is up, use 0; 0 is down, 1 is up, use 1; add 2, use 1; 1 is
// down, use 2; remove 2, use 1.
func (s) TestPriority_SwitchPriority(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p0 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc0}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p0)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Turn down 0, will start and use 1.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	// Handle SubConn creation from 1.
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Add p2, it shouldn't cause any udpates.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-2"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-2": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1", "child-2"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn, %s", sc)
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}

	// Turn down 1, use 2
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// Before 2 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p3.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	p4 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p4.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}

	// Remove 2, use 1.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// p2 SubConns are removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}

	// Should get an update with 1's old transient failure picker, to override
	// 2's old picker.
	p5 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p5.Pick(balancer.PickInfo{}); err == nil {
			t.Fatalf("want pick error non-nil, got nil")
		}
	}

	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	p6 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p6.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}
}

// Lower priority is used when higher priority turns Connecting from Ready.
// Because changing from Ready to Connecting is a failure.
//
// Init 0 and 1; 0 is up, use 0; 0 is connecting, 1 is up, use 1; 0 is ready,
// use 0.
func (s) TestPriority_HighPriorityToConnectingFromReady(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p0 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc0}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p0)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Turn 0 to Connecting, will start and use 1. Because 0 changing from Ready
	// to Connecting is a failure.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	// Handle SubConn creation from 1.
	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Turn 0 back to Ready.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// p1 subconn should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc0, scToRemove)
	}

	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p3.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// Add a lower priority while the higher priority is down.
//
// Init 0 and 1; 0 and 1 both down; add 2, use 2.
func (s) TestPriority_HigherDownWhileAddingLower(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with different priorities, each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Turn down 0, 1 is used.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail0.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	// Turn down 1, pick should error.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// Test pick failure.
	pFail1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail1.Pick(balancer.PickInfo{}); err == nil {
			t.Fatalf("want pick error non-nil, got nil")
		}
	}

	// Add p2, it should create a new SubConn.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-2"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-2": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1", "child-2"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// A new connecting picker should be updated for the new priority.
	p0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p0.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}
}

// When a higher priority becomes available, all lower priorities are closed.
//
// Init 0,1,2; 0 and 1 down, use 2; 0 up, close 1 and 2.
func (s) TestPriority_HigherReadyCloseAllLower(t *testing.T) {
	// defer time.Sleep(10 * time.Millisecond)

	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Three localities, with priorities [0,1,2], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-2"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-2": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1", "child-2"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Turn down 0, 1 is used.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail0.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// Turn down 1, 2 is used.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Before 2 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 2.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}

	// When 0 becomes ready, 0 should be used, 1 and 2 should all be closed.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// sc1 and sc2 should be removed.
	//
	// With localities caching, the lower priorities are closed after a timeout,
	// in goroutines. The order is no longer guaranteed.
	scToRemove := []balancer.SubConn{<-cc.RemoveSubConnCh, <-cc.RemoveSubConnCh}
	if !(cmp.Equal(scToRemove[0], sc1, cmp.AllowUnexported(testutils.TestSubConn{})) &&
		cmp.Equal(scToRemove[1], sc2, cmp.AllowUnexported(testutils.TestSubConn{}))) &&
		!(cmp.Equal(scToRemove[0], sc2, cmp.AllowUnexported(testutils.TestSubConn{})) &&
			cmp.Equal(scToRemove[1], sc1, cmp.AllowUnexported(testutils.TestSubConn{}))) {
		t.Errorf("RemoveSubConn, want [%v, %v], got %v", sc1, sc2, scToRemove)
	}

	// Test pick with 0.
	p0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p0.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// At init, start the next lower priority after timeout if the higher priority
// doesn't get ready.
//
// Init 0,1; 0 is not ready (in connecting), after timeout, use 1.
func (s) TestPriority_InitTimeout(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func() func() {
		old := DefaultPriorityInitTimeout
		DefaultPriorityInitTimeout = testPriorityInitTimeout
		return func() {
			DefaultPriorityInitTimeout = old
		}
	}()()

	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with different priorities, each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Keep 0 in connecting, 1 will be used after init timeout.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// Make sure new SubConn is created before timeout.
	select {
	case <-time.After(testPriorityInitTimeout * 3 / 4):
	case <-cc.NewSubConnAddrsCh:
		t.Fatalf("Got a new SubConn too early (Within timeout). Expect a new SubConn only after timeout")
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with 1.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}
}

// EDS removes all priorities, and re-adds them.
func (s) TestPriority_RemovesAllPriorities(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func() func() {
		old := DefaultPriorityInitTimeout
		DefaultPriorityInitTimeout = testPriorityInitTimeout
		return func() {
			DefaultPriorityInitTimeout = old
		}
	}()()

	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with different priorities, each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p0 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc0}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p0)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove all priorities.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: nil,
		},
		BalancerConfig: &LBConfig{
			Children:   nil,
			Priorities: nil,
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// p0 subconn should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc0, scToRemove)
	}

	// Test pick return TransientFailure.
	pFail := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail.Pick(balancer.PickInfo{}); err != ErrAllPrioritiesRemoved {
			t.Fatalf("want pick error %v, got %v", ErrAllPrioritiesRemoved, err)
		}
	}

	// Re-add two localities, with previous priorities, but different backends.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[3]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs01 := <-cc.NewSubConnAddrsCh
	if got, want := addrs01[0].Addr, testBackendAddrStrs[2]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc01 := <-cc.NewSubConnCh

	// Don't send any update to p0, so to not override the old state of p0.
	// Later, connect to p1 and then remove p1. This will fallback to p0, and
	// will send p0's old picker if they are not correctly removed.

	// p1 will be used after priority init timeout.
	addrs11 := <-cc.NewSubConnAddrsCh
	if got, want := addrs11[0].Addr, testBackendAddrStrs[3]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc11 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc11, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc11, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	p1 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc11}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove p1, to fallback to p0.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[2]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// p1 subconn should be removed.
	scToRemove1 := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove1, sc11, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc11, scToRemove1)
	}

	// Test pick return NoSubConn.
	pFail1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if scst, err := pFail1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error _, %v, got %v, _ ,%v", balancer.ErrNoSubConnAvailable, scst, err)
		}
	}

	// Send an ready update for the p0 sc that was received when re-adding
	// priorities.
	pb.UpdateSubConnState(sc01, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc01, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc01}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}
}

// Test the case where the high priority contains no backends. The low priority
// will be used.
func (s) TestPriority_HighPriorityNoEndpoints(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two localities, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove addresses from priority 0, should use p1.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// p0 will remove the subconn, and ClientConn will send a sc update to
	// shutdown.
	scToRemove := <-cc.RemoveSubConnCh
	pb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh

	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	// p1 is ready.
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// Test the case where the first and only priority is removed.
func (s) TestPriority_FirstPriorityUnavailable(t *testing.T) {
	const testPriorityInitTimeout = time.Second
	defer func(t time.Duration) {
		DefaultPriorityInitTimeout = t
	}(DefaultPriorityInitTimeout)
	DefaultPriorityInitTimeout = testPriorityInitTimeout

	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One localities, with priorities [0], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Remove the only localities.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: nil,
		},
		BalancerConfig: &LBConfig{
			Children:   nil,
			Priorities: nil,
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Wait after double the init timer timeout, to ensure it doesn't panic.
	time.Sleep(testPriorityInitTimeout * 2)
}

// When a child is moved from low priority to high.
//
// Init a(p0) and b(p1); a(p0) is up, use a; move b to p0, a to p1, use b.
func (s) TestPriority_MoveChildToHigherPriority(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two children, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Swap child with p0 and p1, the child at lower priority should now be the
	// higher priority, and be used. The old SubConn should be closed.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-1", "child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// When the new child for p0 is changed from the previous child, the
	// balancer should immediately update the picker so the picker from old
	// child is not used. In this case, the picker becomes a
	// no-subconn-available picker because this child is just started.
	pFail := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	// Old subconn should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh

	// New p0 child is ready.
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only new subconns.
	p2 := <-cc.NewPickerCh
	want2 := []balancer.SubConn{sc2}
	if err := testutils.IsRoundRobin(want2, subConnFromPicker(t, p2)); err != nil {
		t.Fatalf("want %v, got %v", want2, err)
	}
}

// When a child is in lower priority, and in use (because higher is down),
// move it from low priority to high.
//
// Init a(p0) and b(p1); a(p0) is down, use b; move b to p0, a to p1, use b.
func (s) TestPriority_MoveReadyChildToHigherPriority(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two children, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is down.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail0.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	p0 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p0)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Swap child with p0 and p1, the child at lower priority should now be the
	// higher priority, and be used. The old SubConn should be closed.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-1", "child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Old subconn from child-0 should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc0, scToRemove)
	}

	// Because this was a ready child moved to a higher priority, no new subconn
	// or picker should be updated.
	select {
	case <-cc.NewPickerCh:
		t.Fatalf("got unexpected new picker")
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn")
	case <-time.After(time.Millisecond * 100):
	}
}

// When the lowest child is in use, and is removed, should use the higher
// priority child even though it's not ready.
//
// Init a(p0) and b(p1); a(p0) is down, use b; move b to p0, a to p1, use b.
func (s) TestPriority_RemoveReadyLowestChild(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two children, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// p0 is down.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	pFail0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail0.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p1 subconns.
	p0 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p0)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove child with p1, the child at higher priority should now be used.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Old subconn from child-1 should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}

	pFail := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail.Pick(balancer.PickInfo{}); err == nil {
			t.Fatalf("want pick error <non-nil>, got %v", err)
		}
	}

	// Because there was no new child, no new subconn should be created.
	select {
	case <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn")
	case <-time.After(time.Millisecond * 100):
	}
}

// When a ready child is removed, it's kept in cache. Re-adding doesn't create subconns.
//
// Init 0; 0 is up, use 0; remove 0, only picker is updated, no subconn is
// removed; re-add 0, picker is updated.
func (s) TestPriority_ReadyChildRemovedButInCache(t *testing.T) {
	const testChildCacheTimeout = time.Second
	defer func() func() {
		old := balancergroup.DefaultSubBalancerCloseTimeout
		balancergroup.DefaultSubBalancerCloseTimeout = testChildCacheTimeout
		return func() {
			balancergroup.DefaultSubBalancerCloseTimeout = old
		}
	}()()

	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One children, with priorities [0], with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove the child, it shouldn't cause any conn changed, but picker should
	// be different.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{},
		BalancerConfig: &LBConfig{},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	pFail := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := pFail.Pick(balancer.PickInfo{}); err != ErrAllPrioritiesRemoved {
			t.Fatalf("want pick error %v, got %v", ErrAllPrioritiesRemoved, err)
		}
	}

	// But no conn changes should happen. Child balancer is in cache.
	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn: %s", sc)
	case sc := <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn: %v", sc)
	case <-time.After(time.Millisecond * 100):
	}

	// Re-add the child, shouldn't create new connections.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Test roundrobin with only p0 subconns.
	p2 := <-cc.NewPickerCh
	want2 := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want2, subConnFromPicker(t, p2)); err != nil {
		t.Fatalf("want %v, got %v", want2, err)
	}

	// But no conn changes should happen. Child balancer is just taken out from
	// the cache.
	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("got unexpected new SubConn: %s", sc)
	case sc := <-cc.RemoveSubConnCh:
		t.Fatalf("got unexpected remove SubConn: %v", sc)
	case <-time.After(time.Millisecond * 100):
	}
}

// When the policy of a child is changed.
//
// Init 0; 0 is up, use 0; change 0's policy, 0 is used.
func (s) TestPriority_ChildPolicyChange(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One children, with priorities [0], with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: roundrobin.Name}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh

	// p0 is ready.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with only p0 subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(t, p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Change the policy for the child (still roundrobin, but with a different
	// name).
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: testRRBalancerName}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Old subconn should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}

	// A new subconn should be created.
	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc2 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	pb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pickfirst with the new subconns.
	p2 := <-cc.NewPickerCh
	want2 := []balancer.SubConn{sc2}
	if err := testutils.IsRoundRobin(want2, subConnFromPicker(t, p2)); err != nil {
		t.Fatalf("want %v, got %v", want2, err)
	}
}

const inlineUpdateBalancerName = "test-inline-update-balancer"

var errTestInlineStateUpdate = fmt.Errorf("don't like addresses, empty or not")

func init() {
	stub.Register(inlineUpdateBalancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, opts balancer.ClientConnState) error {
			bd.ClientConn.UpdateState(balancer.State{
				ConnectivityState: connectivity.Ready,
				Picker:            &testutils.TestConstPicker{Err: errTestInlineStateUpdate},
			})
			return nil
		},
	})
}

// When the child policy update picker inline in a handleClientUpdate call
// (e.g., roundrobin handling empty addresses). There could be deadlock caused
// by acquiring a locked mutex.
func (s) TestPriority_ChildPolicyUpdatePickerInline(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One children, with priorities [0], with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: inlineUpdateBalancerName}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	p0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := p0.Pick(balancer.PickInfo{})
		if err != errTestInlineStateUpdate {
			t.Fatalf("picker.Pick, got err %q, want err %q", err, errTestInlineStateUpdate)
		}
	}
}

// When the child policy's configured to ignore reresolution requests, the
// ResolveNow() calls from this child should be all ignored.
func (s) TestPriority_IgnoreReresolutionRequest(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One children, with priorities [0], with one backend, reresolution is
	// ignored.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {
					Config:                     &internalserviceconfig.BalancerConfig{Name: resolveNowBalancerName},
					IgnoreReresolutionRequests: true,
				},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// This is the balancer.ClientConn that the inner resolverNowBalancer is
	// built with.
	balancerCCI, err := resolveNowBalancerCCCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout waiting for ClientConn from balancer builder")
	}
	balancerCC := balancerCCI.(balancer.ClientConn)

	// Since IgnoreReresolutionRequests was set to true, all ResolveNow() calls
	// should be ignored.
	for i := 0; i < 5; i++ {
		balancerCC.ResolveNow(resolver.ResolveNowOptions{})
	}
	select {
	case <-cc.ResolveNowCh:
		t.Fatalf("got unexpected ResolveNow() call")
	case <-time.After(time.Millisecond * 100):
	}

	// Send another update to set IgnoreReresolutionRequests to false.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {
					Config:                     &internalserviceconfig.BalancerConfig{Name: resolveNowBalancerName},
					IgnoreReresolutionRequests: false,
				},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Call ResolveNow() on the CC, it should be forwarded.
	balancerCC.ResolveNow(resolver.ResolveNowOptions{})
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for ResolveNow()")
	}

}

// When the child policy's configured to ignore reresolution requests, the
// ResolveNow() calls from this child should be all ignored, from the other
// children are forwarded.
func (s) TestPriority_IgnoreReresolutionRequestTwoChildren(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One children, with priorities [0, 1], each with one backend.
	// Reresolution is ignored for p0.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {
					Config:                     &internalserviceconfig.BalancerConfig{Name: resolveNowBalancerName},
					IgnoreReresolutionRequests: true,
				},
				"child-1": {
					Config: &internalserviceconfig.BalancerConfig{Name: resolveNowBalancerName},
				},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// This is the balancer.ClientConn from p0.
	balancerCCI0, err := resolveNowBalancerCCCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout waiting for ClientConn from balancer builder 0")
	}
	balancerCC0 := balancerCCI0.(balancer.ClientConn)

	// Set p0 to transient failure, p1 will be started.
	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// This is the balancer.ClientConn from p1.
	ctx1, cancel1 := context.WithTimeout(context.Background(), time.Second)
	defer cancel1()
	balancerCCI1, err := resolveNowBalancerCCCh.Receive(ctx1)
	if err != nil {
		t.Fatalf("timeout waiting for ClientConn from balancer builder 1")
	}
	balancerCC1 := balancerCCI1.(balancer.ClientConn)

	// Since IgnoreReresolutionRequests was set to true for p0, ResolveNow()
	// from p0 should all be ignored.
	for i := 0; i < 5; i++ {
		balancerCC0.ResolveNow(resolver.ResolveNowOptions{})
	}
	select {
	case <-cc.ResolveNowCh:
		t.Fatalf("got unexpected ResolveNow() call")
	case <-time.After(time.Millisecond * 100):
	}

	// But IgnoreReresolutionRequests was false for p1, ResolveNow() from p1
	// should be forwarded.
	balancerCC1.ResolveNow(resolver.ResolveNowOptions{})
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for ResolveNow()")
	}
}

const initIdleBalancerName = "test-init-Idle-balancer"

var errsTestInitIdle = []error{
	fmt.Errorf("init Idle balancer error 0"),
	fmt.Errorf("init Idle balancer error 1"),
}

func init() {
	for i := 0; i < 2; i++ {
		ii := i
		stub.Register(fmt.Sprintf("%s-%d", initIdleBalancerName, ii), stub.BalancerFuncs{
			UpdateClientConnState: func(bd *stub.BalancerData, opts balancer.ClientConnState) error {
				bd.ClientConn.NewSubConn(opts.ResolverState.Addresses, balancer.NewSubConnOptions{})
				return nil
			},
			UpdateSubConnState: func(bd *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
				err := fmt.Errorf("wrong picker error")
				if state.ConnectivityState == connectivity.Idle {
					err = errsTestInitIdle[ii]
				}
				bd.ClientConn.UpdateState(balancer.State{
					ConnectivityState: state.ConnectivityState,
					Picker:            &testutils.TestConstPicker{Err: err},
				})
			},
		})
	}
}

// If the high priorities send initial pickers with Idle state, their pickers
// should get picks, because policies like ringhash starts in Idle, and doesn't
// connect.
//
// Init 0, 1; 0 is Idle, use 0; 0 is down, start 1; 1 is Idle, use 1.
func (s) TestPriority_HighPriorityInitIdle(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// Two children, with priorities [0, 1], each with one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: fmt.Sprintf("%s-%d", initIdleBalancerName, 0)}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: fmt.Sprintf("%s-%d", initIdleBalancerName, 1)}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Send an Idle state update to trigger an Idle picker update.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	p0 := <-cc.NewPickerCh
	if pr, err := p0.Pick(balancer.PickInfo{}); err != errsTestInitIdle[0] {
		t.Fatalf("pick returned %v, %v, want _, %v", pr, err, errsTestInitIdle[0])
	}

	// Turn p0 down, to start p1.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Before 1 gets READY, picker should return NoSubConnAvailable, so RPCs
	// will retry.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p1.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testBackendAddrStrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc1 := <-cc.NewSubConnCh
	// Idle picker from p1 should also be forwarded.
	pb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	p2 := <-cc.NewPickerCh
	if pr, err := p2.Pick(balancer.PickInfo{}); err != errsTestInitIdle[1] {
		t.Fatalf("pick returned %v, %v, want _, %v", pr, err, errsTestInitIdle[1])
	}
}

// If the high priorities send initial pickers with Idle state, their pickers
// should get picks, because policies like ringhash starts in Idle, and doesn't
// connect. In this case, if a lower priority is added, it shouldn't switch to
// the lower priority.
//
// Init 0; 0 is Idle, use 0; add 1, use 0.
func (s) TestPriority_AddLowPriorityWhenHighIsInIdle(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	bb := balancer.Get(Name)
	pb := bb.Build(cc, balancer.BuildOptions{})
	defer pb.Close()

	// One child, with priorities [0], one backend.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: fmt.Sprintf("%s-%d", initIdleBalancerName, 0)}},
			},
			Priorities: []string{"child-0"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	addrs0 := <-cc.NewSubConnAddrsCh
	if got, want := addrs0[0].Addr, testBackendAddrStrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	sc0 := <-cc.NewSubConnCh

	// Send an Idle state update to trigger an Idle picker update.
	pb.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	p0 := <-cc.NewPickerCh
	if pr, err := p0.Pick(balancer.PickInfo{}); err != errsTestInitIdle[0] {
		t.Fatalf("pick returned %v, %v, want _, %v", pr, err, errsTestInitIdle[0])
	}

	// Add 1, should keep using 0.
	if err := pb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Addresses: []resolver.Address{
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[0]}, []string{"child-0"}),
				hierarchy.Set(resolver.Address{Addr: testBackendAddrStrs[1]}, []string{"child-1"}),
			},
		},
		BalancerConfig: &LBConfig{
			Children: map[string]*Child{
				"child-0": {Config: &internalserviceconfig.BalancerConfig{Name: fmt.Sprintf("%s-%d", initIdleBalancerName, 0)}},
				"child-1": {Config: &internalserviceconfig.BalancerConfig{Name: fmt.Sprintf("%s-%d", initIdleBalancerName, 1)}},
			},
			Priorities: []string{"child-0", "child-1"},
		},
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// The ClientConn state update triggers a priority switch, from p0 -> p0
	// (since p0 is still in use). Along with this the update, p0 also gets a
	// ClientConn state update, with the addresses, which didn't change in this
	// test (this update to the child is necessary in case the addresses are
	// different).
	//
	// The test child policy, initIdleBalancer, blindly calls NewSubConn with
	// all the addresses it receives, so this will trigger a NewSubConn with the
	// old p0 addresses. (Note that in a real balancer, like roundrobin, no new
	// SubConn will be created because the addresses didn't change).
	//
	// The check below makes sure that the addresses are still from p0, and not
	// from p1. This is good enough for the purpose of this test.
	addrsNew := <-cc.NewSubConnAddrsCh
	if got, want := addrsNew[0].Addr, testBackendAddrStrs[0]; got != want {
		// Fail if p1 is started and creates a SubConn.
		t.Fatalf("got unexpected call to NewSubConn with addr: %v, want %v", addrsNew, want)
	}
}
