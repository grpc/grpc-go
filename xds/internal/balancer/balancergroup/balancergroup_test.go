/*
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

// All tests in this file are combination of balancer group and
// weighted_balancerstate_aggregator, aka weighted_target tests. The difference
// is weighted_target tests cannot add sub-balancers to balancer group directly,
// they instead uses balancer config to control sub-balancers. Even though not
// very suited, the tests still cover all the functionality.
//
// TODO: the tests should be moved to weighted_target, and balancer group's
// tests should use a mock balancerstate_aggregator.

package balancergroup

import (
	"fmt"
	"testing"
	"time"

	orcapb "github.com/cncf/udpa/go/udpa/data/orca/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/weightedtarget/weightedaggregator"
	"google.golang.org/grpc/xds/internal/client/load"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	rrBuilder        = balancer.Get(roundrobin.Name)
	pfBuilder        = balancer.Get(grpc.PickFirstBalancerName)
	testBalancerIDs  = []string{"b1", "b2", "b3"}
	testBackendAddrs []resolver.Address
)

const testBackendAddrsCount = 12

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrs = append(testBackendAddrs, resolver.Address{Addr: fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i)})
	}

	// Disable caching for all tests. It will be re-enabled in caching specific
	// tests.
	DefaultSubBalancerCloseTimeout = time.Millisecond
}

func subConnFromPicker(p balancer.Picker) func() balancer.SubConn {
	return func() balancer.SubConn {
		scst, _ := p.Pick(balancer.PickInfo{})
		return scst.SubConn
	}
}

func newTestBalancerGroup(t *testing.T, loadStore load.PerClusterReporter) (*testutils.TestClientConn, *weightedaggregator.Aggregator, *BalancerGroup) {
	cc := testutils.NewTestClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(cc, gator, loadStore, nil)
	bg.Start()
	return cc, gator, bg
}

// 1 balancer, 1 backend -> 2 backends -> 1 backend.
func (s) TestBalancerGroup_OneRR_AddRemoveBackend(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add one balancer to group.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	// Send one resolved address.
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})

	// Send subconn state change.
	sc1 := <-cc.NewSubConnCh
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Send two addresses.
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	// Expect one new subconn, send state update.
	sc2 := <-cc.NewSubConnCh
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin pick.
	p2 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove the first address.
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[1:2]}})
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	bg.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	// Test pick with only the second subconn.
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _ := p3.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSC.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSC, sc2)
		}
	}
}

// 2 balancers, each with 1 backend.
func (s) TestBalancerGroup_TwoRR_OneBackend(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send one resolved address to both
	// balancers.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc1 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc2 := <-cc.NewSubConnCh

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// 2 balancers, each with more than 1 backends.
func (s) TestBalancerGroup_TwoRR_MoreBackends(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send one resolved address to both
	// balancers.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	sc1 := <-cc.NewSubConnCh
	sc2 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})
	sc3 := <-cc.NewSubConnCh
	sc4 := <-cc.NewSubConnCh

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Turn sc2's connection down, should be RR between balancers.
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	p2 := <-cc.NewPickerCh
	// Expect two sc1's in the result, because balancer1 will be picked twice,
	// but there's only one sc in it.
	want = []balancer.SubConn{sc1, sc1, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove sc3's addresses.
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[3:4]}})
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc3, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc3, scToRemove)
	}
	bg.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})
	p3 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc1, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Turn sc1's connection down.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	p4 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p4)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Turn last connection to connecting.
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	p5 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p5.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick error %v, got %v", balancer.ErrNoSubConnAvailable, err)
		}
	}

	// Turn all connections down.
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	p6 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p6.Pick(balancer.PickInfo{}); err != balancer.ErrTransientFailure {
			t.Fatalf("want pick error %v, got %v", balancer.ErrTransientFailure, err)
		}
	}
}

// 2 balancers with different weights.
func (s) TestBalancerGroup_TwoRR_DifferentWeight_MoreBackends(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	sc1 := <-cc.NewSubConnCh
	sc2 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})
	sc3 := <-cc.NewSubConnCh
	sc4 := <-cc.NewSubConnCh

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc1, sc2, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// totally 3 balancers, add/remove balancer.
func (s) TestBalancerGroup_ThreeRR_RemoveBalancer(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add three balancers to group and send one resolved address to both
	// balancers.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc1 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[1:2]}})
	sc2 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[2], 1)
	bg.Add(testBalancerIDs[2], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[2], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[1:2]}})
	sc3 := <-cc.NewSubConnCh

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2, sc3}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove the second balancer, while the others two are ready.
	gator.Remove(testBalancerIDs[1])
	bg.Remove(testBalancerIDs[1])
	gator.BuildAndUpdate()
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc1, sc3}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// move balancer 3 into transient failure.
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Remove the first balancer, while the third is transient failure.
	gator.Remove(testBalancerIDs[0])
	bg.Remove(testBalancerIDs[0])
	gator.BuildAndUpdate()
	scToRemove = <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		if _, err := p3.Pick(balancer.PickInfo{}); err != balancer.ErrTransientFailure {
			t.Fatalf("want pick error %v, got %v", balancer.ErrTransientFailure, err)
		}
	}
}

// 2 balancers, change balancer weight.
func (s) TestBalancerGroup_TwoRR_ChangeWeight_MoreBackends(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	sc1 := <-cc.NewSubConnCh
	sc2 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})
	sc3 := <-cc.NewSubConnCh
	sc4 := <-cc.NewSubConnCh

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc1, sc2, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	gator.UpdateWeight(testBalancerIDs[0], 3)
	gator.BuildAndUpdate()

	// Test roundrobin with new weight.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc1, sc1, sc1, sc2, sc2, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func (s) TestBalancerGroup_LoadReport(t *testing.T) {
	loadStore := load.NewStore()
	const (
		testCluster    = "test-cluster"
		testEDSService = "test-eds-service"
	)
	cc, gator, bg := newTestBalancerGroup(t, loadStore.PerCluster(testCluster, testEDSService))

	backendToBalancerID := make(map[balancer.SubConn]string)

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	sc1 := <-cc.NewSubConnCh
	sc2 := <-cc.NewSubConnCh
	backendToBalancerID[sc1] = testBalancerIDs[0]
	backendToBalancerID[sc2] = testBalancerIDs[0]

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})
	sc3 := <-cc.NewSubConnCh
	sc4 := <-cc.NewSubConnCh
	backendToBalancerID[sc3] = testBalancerIDs[1]
	backendToBalancerID[sc4] = testBalancerIDs[1]

	// Send state changes for both subconns.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	bg.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	// bg1 has a weight of 2, while bg2 has a weight of 1. So, we expect 20 of
	// these picks to go to bg1 and 10 of them to bg2. And since there are two
	// subConns in each group, we expect the picks to be equally split between
	// the subConns. We do not call Done() on picks routed to sc1, so we expect
	// these to show up as pending rpcs.
	wantStoreData := []*load.Data{{
		Cluster: testCluster,
		Service: testEDSService,
		LocalityStats: map[string]load.LocalityData{
			testBalancerIDs[0]: {
				RequestStats: load.RequestData{Succeeded: 10, InProgress: 10},
				LoadStats: map[string]load.ServerLoadData{
					"cpu_utilization": {Count: 10, Sum: 100},
					"mem_utilization": {Count: 10, Sum: 50},
					"pic":             {Count: 10, Sum: 31.4},
					"piu":             {Count: 10, Sum: 31.4},
				},
			},
			testBalancerIDs[1]: {
				RequestStats: load.RequestData{Succeeded: 10},
				LoadStats: map[string]load.ServerLoadData{
					"cpu_utilization": {Count: 10, Sum: 100},
					"mem_utilization": {Count: 10, Sum: 50},
					"pic":             {Count: 10, Sum: 31.4},
					"piu":             {Count: 10, Sum: 31.4},
				},
			},
		},
	}}
	for i := 0; i < 30; i++ {
		scst, _ := p1.Pick(balancer.PickInfo{})
		if scst.Done != nil && scst.SubConn != sc1 {
			scst.Done(balancer.DoneInfo{
				ServerLoad: &orcapb.OrcaLoadReport{
					CpuUtilization: 10,
					MemUtilization: 5,
					RequestCost:    map[string]float64{"pic": 3.14},
					Utilization:    map[string]float64{"piu": 3.14},
				},
			})
		}
	}

	gotStoreData := loadStore.Stats([]string{testCluster})
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty(), cmpopts.EquateApprox(0, 0.1), cmpopts.IgnoreFields(load.Data{}, "ReportInterval")); diff != "" {
		t.Errorf("store.stats() returned unexpected diff (-want +got):\n%s", diff)
	}
}

// Create a new balancer group, add balancer and backends, but not start.
// - b1, weight 2, backends [0,1]
// - b2, weight 1, backends [2,3]
// Start the balancer group and check behavior.
//
// Close the balancer group, call add/remove/change weight/change address.
// - b2, weight 3, backends [0,3]
// - b3, weight 1, backends [1,2]
// Start the balancer group again and check for behavior.
func (s) TestBalancerGroup_start_close(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(cc, gator, nil, nil)

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})

	bg.Start()

	m1 := make(map[resolver.Address]balancer.SubConn)
	for i := 0; i < 4; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m1[addrs[0]] = sc
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		m1[testBackendAddrs[0]], m1[testBackendAddrs[0]],
		m1[testBackendAddrs[1]], m1[testBackendAddrs[1]],
		m1[testBackendAddrs[2]], m1[testBackendAddrs[3]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	gator.Stop()
	bg.Close()
	for i := 0; i < 4; i++ {
		bg.UpdateSubConnState(<-cc.RemoveSubConnCh, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})
	}

	// Add b3, weight 1, backends [1,2].
	gator.Add(testBalancerIDs[2], 1)
	bg.Add(testBalancerIDs[2], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[2], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[1:3]}})

	// Remove b1.
	gator.Remove(testBalancerIDs[0])
	bg.Remove(testBalancerIDs[0])

	// Update b2 to weight 3, backends [0,3].
	gator.UpdateWeight(testBalancerIDs[1], 3)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: append([]resolver.Address(nil), testBackendAddrs[0], testBackendAddrs[3])}})

	gator.Start()
	bg.Start()

	m2 := make(map[resolver.Address]balancer.SubConn)
	for i := 0; i < 4; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m2[addrs[0]] = sc
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	// Test roundrobin on the last picker.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{
		m2[testBackendAddrs[0]], m2[testBackendAddrs[0]], m2[testBackendAddrs[0]],
		m2[testBackendAddrs[3]], m2[testBackendAddrs[3]], m2[testBackendAddrs[3]],
		m2[testBackendAddrs[1]], m2[testBackendAddrs[2]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// Test that balancer group start() doesn't deadlock if the balancer calls back
// into balancer group inline when it gets an update.
//
// The potential deadlock can happen if we
//  - hold a lock and send updates to balancer (e.g. update resolved addresses)
//  - the balancer calls back (NewSubConn or update picker) in line
// The callback will try to hold hte same lock again, which will cause a
// deadlock.
//
// This test starts the balancer group with a test balancer, will updates picker
// whenever it gets an address update. It's expected that start() doesn't block
// because of deadlock.
func (s) TestBalancerGroup_start_close_deadlock(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(cc, gator, nil, nil)

	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], &testutils.TestConstBalancerBuilder{})
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], &testutils.TestConstBalancerBuilder{})
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})

	bg.Start()
}

// Test that at init time, with two sub-balancers, if one sub-balancer reports
// transient_failure, the picks won't fail with transient_failure, and should
// instead wait for the other sub-balancer.
func (s) TestBalancerGroup_InitOneSubBalancerTransientFailure(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send one resolved address to both
	// balancers.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc1 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	<-cc.NewSubConnCh

	// Set one subconn to TransientFailure, this will trigger one sub-balancer
	// to report transient failure.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		r, err := p1.Pick(balancer.PickInfo{})
		if err != balancer.ErrNoSubConnAvailable {
			t.Fatalf("want pick to fail with %v, got result %v, err %v", balancer.ErrNoSubConnAvailable, r, err)
		}
	}
}

// Test that with two sub-balancers, both in transient_failure, if one turns
// connecting, the overall state stays in transient_failure, and all picks
// return transient failure error.
func (s) TestBalancerGroup_SubBalancerTurnsConnectingFromTransientFailure(t *testing.T) {
	cc, gator, bg := newTestBalancerGroup(t, nil)

	// Add two balancers to group and send one resolved address to both
	// balancers.
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], pfBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc1 := <-cc.NewSubConnCh

	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], pfBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:1]}})
	sc2 := <-cc.NewSubConnCh

	// Set both subconn to TransientFailure, this will put both sub-balancers in
	// transient failure.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	bg.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		r, err := p1.Pick(balancer.PickInfo{})
		if err != balancer.ErrTransientFailure {
			t.Fatalf("want pick to fail with %v, got result %v, err %v", balancer.ErrTransientFailure, r, err)
		}
	}

	// Set one subconn to Connecting, it shouldn't change the overall state.
	bg.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		r, err := p2.Pick(balancer.PickInfo{})
		if err != balancer.ErrTransientFailure {
			t.Fatalf("want pick to fail with %v, got result %v, err %v", balancer.ErrTransientFailure, r, err)
		}
	}
}

func replaceDefaultSubBalancerCloseTimeout(n time.Duration) func() {
	old := DefaultSubBalancerCloseTimeout
	DefaultSubBalancerCloseTimeout = n
	return func() { DefaultSubBalancerCloseTimeout = old }
}

// initBalancerGroupForCachingTest creates a balancer group, and initialize it
// to be ready for caching tests.
//
// Two rr balancers are added to bg, each with 2 ready subConns. A sub-balancer
// is removed later, so the balancer group returned has one sub-balancer in its
// own map, and one sub-balancer in cache.
func initBalancerGroupForCachingTest(t *testing.T) (*weightedaggregator.Aggregator, *BalancerGroup, *testutils.TestClientConn, map[resolver.Address]balancer.SubConn) {
	cc := testutils.NewTestClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(cc, gator, nil, nil)

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})

	bg.Start()

	m1 := make(map[resolver.Address]balancer.SubConn)
	for i := 0; i < 4; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m1[addrs[0]] = sc
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		m1[testBackendAddrs[0]], m1[testBackendAddrs[0]],
		m1[testBackendAddrs[1]], m1[testBackendAddrs[1]],
		m1[testBackendAddrs[2]], m1[testBackendAddrs[3]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	gator.Remove(testBalancerIDs[1])
	bg.Remove(testBalancerIDs[1])
	gator.BuildAndUpdate()
	// Don't wait for SubConns to be removed after close, because they are only
	// removed after close timeout.
	for i := 0; i < 10; i++ {
		select {
		case <-cc.RemoveSubConnCh:
			t.Fatalf("Got request to remove subconn, want no remove subconn (because subconns were still in cache)")
		default:
		}
		time.Sleep(time.Millisecond)
	}
	// Test roundrobin on the with only sub-balancer0.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{
		m1[testBackendAddrs[0]], m1[testBackendAddrs[1]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	return gator, bg, cc, m1
}

// Test that if a sub-balancer is removed, and re-added within close timeout,
// the subConns won't be re-created.
func (s) TestBalancerGroup_locality_caching(t *testing.T) {
	defer replaceDefaultSubBalancerCloseTimeout(10 * time.Second)()
	gator, bg, cc, addrToSC := initBalancerGroupForCachingTest(t)

	// Turn down subconn for addr2, shouldn't get picker update because
	// sub-balancer1 was removed.
	bg.UpdateSubConnState(addrToSC[testBackendAddrs[2]], balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	for i := 0; i < 10; i++ {
		select {
		case <-cc.NewPickerCh:
			t.Fatalf("Got new picker, want no new picker (because the sub-balancer was removed)")
		default:
		}
		time.Sleep(time.Millisecond)
	}

	// Sleep, but sleep less then close timeout.
	time.Sleep(time.Millisecond * 100)

	// Re-add sub-balancer-1, because subconns were in cache, no new subconns
	// should be created. But a new picker will still be generated, with subconn
	// states update to date.
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)

	p3 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		addrToSC[testBackendAddrs[0]], addrToSC[testBackendAddrs[0]],
		addrToSC[testBackendAddrs[1]], addrToSC[testBackendAddrs[1]],
		// addr2 is down, b2 only has addr3 in READY state.
		addrToSC[testBackendAddrs[3]], addrToSC[testBackendAddrs[3]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	for i := 0; i < 10; i++ {
		select {
		case <-cc.NewSubConnAddrsCh:
			t.Fatalf("Got new subconn, want no new subconn (because subconns were still in cache)")
		default:
		}
		time.Sleep(time.Millisecond * 10)
	}
}

// Sub-balancers are put in cache when they are removed. If balancer group is
// closed within close timeout, all subconns should still be rmeoved
// immediately.
func (s) TestBalancerGroup_locality_caching_close_group(t *testing.T) {
	defer replaceDefaultSubBalancerCloseTimeout(10 * time.Second)()
	_, bg, cc, addrToSC := initBalancerGroupForCachingTest(t)

	bg.Close()
	// The balancer group is closed. The subconns should be removed immediately.
	removeTimeout := time.After(time.Millisecond * 500)
	scToRemove := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[0]]: 1,
		addrToSC[testBackendAddrs[1]]: 1,
		addrToSC[testBackendAddrs[2]]: 1,
		addrToSC[testBackendAddrs[3]]: 1,
	}
	for i := 0; i < len(scToRemove); i++ {
		select {
		case sc := <-cc.RemoveSubConnCh:
			c := scToRemove[sc]
			if c == 0 {
				t.Fatalf("Got removeSubConn for %v when there's %d remove expected", sc, c)
			}
			scToRemove[sc] = c - 1
		case <-removeTimeout:
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be removed")
		}
	}
}

// Sub-balancers in cache will be closed if not re-added within timeout, and
// subConns will be removed.
func (s) TestBalancerGroup_locality_caching_not_readd_within_timeout(t *testing.T) {
	defer replaceDefaultSubBalancerCloseTimeout(time.Second)()
	_, _, cc, addrToSC := initBalancerGroupForCachingTest(t)

	// The sub-balancer is not re-added withtin timeout. The subconns should be
	// removed.
	removeTimeout := time.After(DefaultSubBalancerCloseTimeout)
	scToRemove := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[2]]: 1,
		addrToSC[testBackendAddrs[3]]: 1,
	}
	for i := 0; i < len(scToRemove); i++ {
		select {
		case sc := <-cc.RemoveSubConnCh:
			c := scToRemove[sc]
			if c == 0 {
				t.Fatalf("Got removeSubConn for %v when there's %d remove expected", sc, c)
			}
			scToRemove[sc] = c - 1
		case <-removeTimeout:
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be removed")
		}
	}
}

// Wrap the rr builder, so it behaves the same, but has a different pointer.
type noopBalancerBuilderWrapper struct {
	balancer.Builder
}

// After removing a sub-balancer, re-add with same ID, but different balancer
// builder. Old subconns should be removed, and new subconns should be created.
func (s) TestBalancerGroup_locality_caching_readd_with_different_builder(t *testing.T) {
	defer replaceDefaultSubBalancerCloseTimeout(10 * time.Second)()
	gator, bg, cc, addrToSC := initBalancerGroupForCachingTest(t)

	// Re-add sub-balancer-1, but with a different balancer builder. The
	// sub-balancer was still in cache, but cann't be reused. This should cause
	// old sub-balancer's subconns to be removed immediately, and new subconns
	// to be created.
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], &noopBalancerBuilderWrapper{rrBuilder})

	// The cached sub-balancer should be closed, and the subconns should be
	// removed immediately.
	removeTimeout := time.After(time.Millisecond * 500)
	scToRemove := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[2]]: 1,
		addrToSC[testBackendAddrs[3]]: 1,
	}
	for i := 0; i < len(scToRemove); i++ {
		select {
		case sc := <-cc.RemoveSubConnCh:
			c := scToRemove[sc]
			if c == 0 {
				t.Fatalf("Got removeSubConn for %v when there's %d remove expected", sc, c)
			}
			scToRemove[sc] = c - 1
		case <-removeTimeout:
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be removed")
		}
	}

	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[4:6]}})

	newSCTimeout := time.After(time.Millisecond * 500)
	scToAdd := map[resolver.Address]int{
		testBackendAddrs[4]: 1,
		testBackendAddrs[5]: 1,
	}
	for i := 0; i < len(scToAdd); i++ {
		select {
		case addr := <-cc.NewSubConnAddrsCh:
			c := scToAdd[addr[0]]
			if c == 0 {
				t.Fatalf("Got newSubConn for %v when there's %d new expected", addr, c)
			}
			scToAdd[addr[0]] = c - 1
			sc := <-cc.NewSubConnCh
			addrToSC[addr[0]] = sc
			bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
			bg.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
		case <-newSCTimeout:
			t.Fatalf("timeout waiting for subConns (from new sub-balancer) to be newed")
		}
	}

	// Test roundrobin on the new picker.
	p3 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		addrToSC[testBackendAddrs[0]], addrToSC[testBackendAddrs[0]],
		addrToSC[testBackendAddrs[1]], addrToSC[testBackendAddrs[1]],
		addrToSC[testBackendAddrs[4]], addrToSC[testBackendAddrs[5]],
	}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}
