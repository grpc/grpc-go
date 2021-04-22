// +build go1.12

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

package edsbalancer

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/xds/env"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
	"google.golang.org/grpc/xds/internal/client"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	testClusterNames  = []string{"test-cluster-1", "test-cluster-2"}
	testSubZones      = []string{"I", "II", "III", "IV"}
	testEndpointAddrs []string
)

const testBackendAddrsCount = 12

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testEndpointAddrs = append(testEndpointAddrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

// One locality
//  - add backend
//  - remove backend
//  - replace backend
//  - change drop rate
func (s) TestEDS_OneLocality(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Pick with only the first backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// The same locality, add one more backend.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab2.Build()))

	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p2 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// The same locality, delete first backend.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[1:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab3.Build()))

	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.handleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with only the second subconn.
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p3.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}

	// The same locality, replace backend.
	clab4 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab4.Build()))

	sc3 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc3, connectivity.Ready)
	scToRemove = <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}
	edsb.handleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with only the third subconn.
	p4 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p4.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc3, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc3)
		}
	}

	// The same locality, different drop rate, dropping 50%.
	clab5 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], map[string]uint32{"test-drop": 50})
	clab5.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab5.Build()))

	// Picks with drops.
	p5 := <-cc.NewPickerCh
	for i := 0; i < 100; i++ {
		_, err := p5.Pick(balancer.PickInfo{})
		// TODO: the dropping algorithm needs a design. When the dropping algorithm
		// is fixed, this test also needs fix.
		if i < 50 && err == nil {
			t.Errorf("The first 50%% picks should be drops, got error <nil>")
		} else if i > 50 && err != nil {
			t.Errorf("The second 50%% picks should be non-drops, got error %v", err)
		}
	}

	// The same locality, remove drops.
	clab6 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab6.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab6.Build()))

	// Pick without drops.
	p6 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p6.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc3, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc3)
		}
	}
}

// 2 locality
//  - start with 2 locality
//  - add locality
//  - remove locality
//  - address change for the <not-the-first> locality
//  - update locality weight
func (s) TestEDS_TwoLocalities(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	// Two localities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Add the second locality later to make sure sc2 belongs to the second
	// locality. Otherwise the test is flaky because of a map is used in EDS to
	// keep localities.
	clab1.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Add another locality, with one backend.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab2.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab2.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:3], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab2.Build()))

	sc3 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc3, connectivity.Ready)

	// Test roundrobin with three subconns.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc1, sc2, sc3}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove first locality.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab3.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:3], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab3.Build()))

	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.handleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with two subconns (without the first one).
	p3 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc2, sc3}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Add a backend to the last locality.
	clab4 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab4.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab4.Build()))

	sc4 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc4, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc4, connectivity.Ready)

	// Test pick with two subconns (without the first one).
	p4 := <-cc.NewPickerCh
	// Locality-1 will be picked twice, and locality-2 will be picked twice.
	// Locality-1 contains only sc2, locality-2 contains sc3 and sc4. So expect
	// two sc2's and sc3, sc4.
	want = []balancer.SubConn{sc2, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p4)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Change weight of the locality[1].
	clab5 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab5.AddLocality(testSubZones[1], 2, 0, testEndpointAddrs[1:2], nil)
	clab5.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab5.Build()))

	// Test pick with two subconns different locality weight.
	p5 := <-cc.NewPickerCh
	// Locality-1 will be picked four times, and locality-2 will be picked twice
	// (weight 2 and 1). Locality-1 contains only sc2, locality-2 contains sc3 and
	// sc4. So expect four sc2's and sc3, sc4.
	want = []balancer.SubConn{sc2, sc2, sc2, sc2, sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p5)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Change weight of the locality[1] to 0, it should never be picked.
	clab6 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab6.AddLocality(testSubZones[1], 0, 0, testEndpointAddrs[1:2], nil)
	clab6.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab6.Build()))

	// Changing weight of locality[1] to 0 caused it to be removed. It's subconn
	// should also be removed.
	//
	// NOTE: this is because we handle locality with weight 0 same as the
	// locality doesn't exist. If this changes in the future, this removeSubConn
	// behavior will also change.
	scToRemove2 := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove2, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove2)
	}

	// Test pick with two subconns different locality weight.
	p6 := <-cc.NewPickerCh
	// Locality-1 will be not be picked, and locality-2 will be picked.
	// Locality-2 contains sc3 and sc4. So expect sc3, sc4.
	want = []balancer.SubConn{sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p6)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// The EDS balancer gets EDS resp with unhealthy endpoints. Test that only
// healthy ones are used.
func (s) TestEDS_EndpointsHealth(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	// Two localities, each 3 backend, one Healthy, one Unhealthy, one Unknown.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:6], &testutils.AddLocalityOptions{
		Health: []corepb.HealthStatus{
			corepb.HealthStatus_HEALTHY,
			corepb.HealthStatus_UNHEALTHY,
			corepb.HealthStatus_UNKNOWN,
			corepb.HealthStatus_DRAINING,
			corepb.HealthStatus_TIMEOUT,
			corepb.HealthStatus_DEGRADED,
		},
	})
	clab1.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[6:12], &testutils.AddLocalityOptions{
		Health: []corepb.HealthStatus{
			corepb.HealthStatus_HEALTHY,
			corepb.HealthStatus_UNHEALTHY,
			corepb.HealthStatus_UNKNOWN,
			corepb.HealthStatus_DRAINING,
			corepb.HealthStatus_TIMEOUT,
			corepb.HealthStatus_DEGRADED,
		},
	})
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	var (
		readySCs           []balancer.SubConn
		newSubConnAddrStrs []string
	)
	for i := 0; i < 4; i++ {
		addr := <-cc.NewSubConnAddrsCh
		newSubConnAddrStrs = append(newSubConnAddrStrs, addr[0].Addr)
		sc := <-cc.NewSubConnCh
		edsb.handleSubConnStateChange(sc, connectivity.Connecting)
		edsb.handleSubConnStateChange(sc, connectivity.Ready)
		readySCs = append(readySCs, sc)
	}

	wantNewSubConnAddrStrs := []string{
		testEndpointAddrs[0],
		testEndpointAddrs[2],
		testEndpointAddrs[6],
		testEndpointAddrs[8],
	}
	sortStrTrans := cmp.Transformer("Sort", func(in []string) []string {
		out := append([]string(nil), in...) // Copy input to avoid mutating it.
		sort.Strings(out)
		return out
	})
	if !cmp.Equal(newSubConnAddrStrs, wantNewSubConnAddrStrs, sortStrTrans) {
		t.Fatalf("want newSubConn with address %v, got %v", wantNewSubConnAddrStrs, newSubConnAddrStrs)
	}

	// There should be exactly 4 new SubConns. Check to make sure there's no
	// more subconns being created.
	select {
	case <-cc.NewSubConnCh:
		t.Fatalf("Got unexpected new subconn")
	case <-time.After(time.Microsecond * 100):
	}

	// Test roundrobin with the subconns.
	p1 := <-cc.NewPickerCh
	want := readySCs
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func (s) TestClose(t *testing.T) {
	edsb := newEDSBalancerImpl(nil, balancer.BuildOptions{}, nil, nil, nil)
	// This is what could happen when switching between fallback and eds. This
	// make sure it doesn't panic.
	edsb.close()
}

// TestEDS_EmptyUpdate covers the cases when eds impl receives an empty update.
//
// It should send an error picker with transient failure to the parent.
func (s) TestEDS_EmptyUpdate(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	// The first update is an empty update.
	edsb.handleEDSResponse(xdsclient.EndpointsUpdate{})
	// Pick should fail with transient failure, and all priority removed error.
	perr0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := perr0.Pick(balancer.PickInfo{})
		if !reflect.DeepEqual(err, errAllPrioritiesRemoved) {
			t.Fatalf("picker.Pick, got error %v, want error %v", err, errAllPrioritiesRemoved)
		}
	}

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Pick with only the first backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !reflect.DeepEqual(gotSCSt.SubConn, sc1) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	edsb.handleEDSResponse(xdsclient.EndpointsUpdate{})
	// Pick should fail with transient failure, and all priority removed error.
	perr1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := perr1.Pick(balancer.PickInfo{})
		if !reflect.DeepEqual(err, errAllPrioritiesRemoved) {
			t.Fatalf("picker.Pick, got error %v, want error %v", err, errAllPrioritiesRemoved)
		}
	}

	// Handle another update with priorities and localities.
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)

	// Pick with only the first backend.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{})
		if !reflect.DeepEqual(gotSCSt.SubConn, sc2) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}
}

// Create XDS balancer, and update sub-balancer before handling eds responses.
// Then switch between round-robin and a test stub-balancer after handling first
// eds response.
func (s) TestEDS_UpdateSubBalancerName(t *testing.T) {
	const balancerName = "stubBalancer-TestEDS_UpdateSubBalancerName"

	cc := testutils.NewTestClientConn(t)
	stub.Register(balancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, s balancer.ClientConnState) error {
			if len(s.ResolverState.Addresses) == 0 {
				return nil
			}
			bd.ClientConn.NewSubConn(s.ResolverState.Addresses, balancer.NewSubConnOptions{})
			return nil
		},
		UpdateSubConnState: func(bd *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			bd.ClientConn.UpdateState(balancer.State{
				ConnectivityState: state.ConnectivityState,
				Picker:            &testutils.TestConstPicker{Err: testutils.ErrTestConstPicker},
			})
		},
	})

	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	t.Logf("update sub-balancer to stub-balancer")
	edsb.handleChildPolicy(balancerName, nil)

	// Two localities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab1.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	for i := 0; i < 2; i++ {
		sc := <-cc.NewSubConnCh
		edsb.handleSubConnStateChange(sc, connectivity.Ready)
	}

	p0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := p0.Pick(balancer.PickInfo{})
		if err != testutils.ErrTestConstPicker {
			t.Fatalf("picker.Pick, got err %+v, want err %+v", err, testutils.ErrTestConstPicker)
		}
	}

	t.Logf("update sub-balancer to round-robin")
	edsb.handleChildPolicy(roundrobin.Name, nil)

	for i := 0; i < 2; i++ {
		<-cc.RemoveSubConnCh
	}

	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)
	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	t.Logf("update sub-balancer to stub-balancer")
	edsb.handleChildPolicy(balancerName, nil)

	for i := 0; i < 2; i++ {
		scToRemove := <-cc.RemoveSubConnCh
		if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) &&
			!cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("RemoveSubConn, want (%v or %v), got %v", sc1, sc2, scToRemove)
		}
		edsb.handleSubConnStateChange(scToRemove, connectivity.Shutdown)
	}

	for i := 0; i < 2; i++ {
		sc := <-cc.NewSubConnCh
		edsb.handleSubConnStateChange(sc, connectivity.Ready)
	}

	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := p2.Pick(balancer.PickInfo{})
		if err != testutils.ErrTestConstPicker {
			t.Fatalf("picker.Pick, got err %q, want err %q", err, testutils.ErrTestConstPicker)
		}
	}

	t.Logf("update sub-balancer to round-robin")
	edsb.handleChildPolicy(roundrobin.Name, nil)

	for i := 0; i < 2; i++ {
		<-cc.RemoveSubConnCh
	}

	sc3 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc3, connectivity.Ready)
	sc4 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc4, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc4, connectivity.Ready)

	p3 := <-cc.NewPickerCh
	want = []balancer.SubConn{sc3, sc4}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func (s) TestEDS_CircuitBreaking(t *testing.T) {
	origCircuitBreakingSupport := env.CircuitBreakingSupport
	env.CircuitBreakingSupport = true
	defer func() { env.CircuitBreakingSupport = origCircuitBreakingSupport }()

	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState
	var maxRequests uint32 = 50
	edsb.updateServiceRequestsConfig("test", &maxRequests)

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Picks with drops.
	dones := []func(){}
	p := <-cc.NewPickerCh
	for i := 0; i < 100; i++ {
		pr, err := p.Pick(balancer.PickInfo{})
		if i < 50 && err != nil {
			t.Errorf("The first 50%% picks should be non-drops, got error %v", err)
		} else if i > 50 && err == nil {
			t.Errorf("The second 50%% picks should be drops, got error <nil>")
		}
		dones = append(dones, func() {
			if pr.Done != nil {
				pr.Done(balancer.DoneInfo{})
			}
		})
	}

	for _, done := range dones {
		done()
	}
	dones = []func(){}

	// Pick without drops.
	for i := 0; i < 50; i++ {
		pr, err := p.Pick(balancer.PickInfo{})
		if err != nil {
			t.Errorf("The third 50%% picks should be non-drops, got error %v", err)
		}
		dones = append(dones, func() {
			if pr.Done != nil {
				pr.Done(balancer.DoneInfo{})
			}
		})
	}

	// Without this, future tests with the same service name will fail.
	for _, done := range dones {
		done()
	}

	// Send another update, with only circuit breaking update (and no picker
	// update afterwards). Make sure the new picker uses the new configs.
	var maxRequests2 uint32 = 10
	edsb.updateServiceRequestsConfig("test", &maxRequests2)

	// Picks with drops.
	dones = []func(){}
	p2 := <-cc.NewPickerCh
	for i := 0; i < 100; i++ {
		pr, err := p2.Pick(balancer.PickInfo{})
		if i < 10 && err != nil {
			t.Errorf("The first 10%% picks should be non-drops, got error %v", err)
		} else if i > 10 && err == nil {
			t.Errorf("The next 90%% picks should be drops, got error <nil>")
		}
		dones = append(dones, func() {
			if pr.Done != nil {
				pr.Done(balancer.DoneInfo{})
			}
		})
	}

	for _, done := range dones {
		done()
	}
	dones = []func(){}

	// Pick without drops.
	for i := 0; i < 10; i++ {
		pr, err := p2.Pick(balancer.PickInfo{})
		if err != nil {
			t.Errorf("The next 10%% picks should be non-drops, got error %v", err)
		}
		dones = append(dones, func() {
			if pr.Done != nil {
				pr.Done(balancer.DoneInfo{})
			}
		})
	}

	// Without this, future tests with the same service name will fail.
	for _, done := range dones {
		done()
	}
}

func init() {
	balancer.Register(&testInlineUpdateBalancerBuilder{})
}

// A test balancer that updates balancer.State inline when handling ClientConn
// state.
type testInlineUpdateBalancerBuilder struct{}

func (*testInlineUpdateBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &testInlineUpdateBalancer{cc: cc}
}

func (*testInlineUpdateBalancerBuilder) Name() string {
	return "test-inline-update-balancer"
}

type testInlineUpdateBalancer struct {
	cc balancer.ClientConn
}

func (tb *testInlineUpdateBalancer) ResolverError(error) {
	panic("not implemented")
}

func (tb *testInlineUpdateBalancer) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {
}

var errTestInlineStateUpdate = fmt.Errorf("don't like addresses, empty or not")

func (tb *testInlineUpdateBalancer) UpdateClientConnState(balancer.ClientConnState) error {
	tb.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            &testutils.TestConstPicker{Err: errTestInlineStateUpdate},
	})
	return nil
}

func (*testInlineUpdateBalancer) Close() {
}

// When the child policy update picker inline in a handleClientUpdate call
// (e.g., roundrobin handling empty addresses). There could be deadlock caused
// by acquiring a locked mutex.
func (s) TestEDS_ChildPolicyUpdatePickerInline(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = func(p priorityType, state balancer.State) {
		// For this test, euqueue needs to happen asynchronously (like in the
		// real implementation).
		go edsb.updateState(p, state)
	}

	edsb.handleChildPolicy("test-inline-update-balancer", nil)

	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	p0 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		_, err := p0.Pick(balancer.PickInfo{})
		if err != errTestInlineStateUpdate {
			t.Fatalf("picker.Pick, got err %q, want err %q", err, errTestInlineStateUpdate)
		}
	}
}

func (s) TestDropPicker(t *testing.T) {
	const pickCount = 12
	var constPicker = &testutils.TestConstPicker{
		SC: testutils.TestSubConns[0],
	}

	tests := []struct {
		name  string
		drops []*dropper
	}{
		{
			name:  "no drop",
			drops: nil,
		},
		{
			name: "one drop",
			drops: []*dropper{
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 2}),
			},
		},
		{
			name: "two drops",
			drops: []*dropper{
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 3}),
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 2}),
			},
		},
		{
			name: "three drops",
			drops: []*dropper{
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 3}),
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 4}),
				newDropper(xdsclient.OverloadDropConfig{Numerator: 1, Denominator: 2}),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			p := newDropPicker(constPicker, tt.drops, nil, nil, defaultServiceRequestCountMax)

			// scCount is the number of sc's returned by pick. The opposite of
			// drop-count.
			var (
				scCount   int
				wantCount = pickCount
			)
			for _, dp := range tt.drops {
				wantCount = wantCount * int(dp.c.Denominator-dp.c.Numerator) / int(dp.c.Denominator)
			}

			for i := 0; i < pickCount; i++ {
				_, err := p.Pick(balancer.PickInfo{})
				if err == nil {
					scCount++
				}
			}

			if scCount != (wantCount) {
				t.Errorf("drops: %+v, scCount %v, wantCount %v", tt.drops, scCount, wantCount)
			}
		})
	}
}

func (s) TestEDS_LoadReport(t *testing.T) {
	origCircuitBreakingSupport := env.CircuitBreakingSupport
	env.CircuitBreakingSupport = true
	defer func() { env.CircuitBreakingSupport = origCircuitBreakingSupport }()

	// We create an xdsClientWrapper with a dummy xdsClientInterface which only
	// implements the LoadStore() method to return the underlying load.Store to
	// be used.
	loadStore := load.NewStore()
	lsWrapper := &loadStoreWrapper{}
	lsWrapper.updateServiceName(testClusterNames[0])
	lsWrapper.updateLoadStore(loadStore)

	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, lsWrapper, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	const (
		testServiceName = "test-service"
		cbMaxRequests   = 20
	)
	var maxRequestsTemp uint32 = cbMaxRequests
	edsb.updateServiceRequestsConfig(testServiceName, &maxRequestsTemp)
	defer client.ClearCounterForTesting(testServiceName)

	backendToBalancerID := make(map[balancer.SubConn]xdsinternal.LocalityID)

	const testDropCategory = "test-drop"
	// Two localities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], map[string]uint32{testDropCategory: 50})
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)
	locality1 := xdsinternal.LocalityID{SubZone: testSubZones[0]}
	backendToBalancerID[sc1] = locality1

	// Add the second locality later to make sure sc2 belongs to the second
	// locality. Otherwise the test is flaky because of a map is used in EDS to
	// keep localities.
	clab1.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)
	locality2 := xdsinternal.LocalityID{SubZone: testSubZones[1]}
	backendToBalancerID[sc2] = locality2

	// Test roundrobin with two subconns.
	p1 := <-cc.NewPickerCh
	// We expect the 10 picks to be split between the localities since they are
	// of equal weight. And since we only mark the picks routed to sc2 as done,
	// the picks on sc1 should show up as inProgress.
	locality1JSON, _ := locality1.ToString()
	locality2JSON, _ := locality2.ToString()
	const (
		rpcCount = 100
		// 50% will be dropped with category testDropCategory.
		dropWithCategory = rpcCount / 2
		// In the remaining RPCs, only cbMaxRequests are allowed by circuit
		// breaking. Others will be dropped by CB.
		dropWithCB = rpcCount - dropWithCategory - cbMaxRequests

		rpcInProgress = cbMaxRequests / 2 // 50% of RPCs will be never done.
		rpcSucceeded  = cbMaxRequests / 2 // 50% of RPCs will succeed.
	)
	wantStoreData := []*load.Data{{
		Cluster: testClusterNames[0],
		Service: "",
		LocalityStats: map[string]load.LocalityData{
			locality1JSON: {RequestStats: load.RequestData{InProgress: rpcInProgress}},
			locality2JSON: {RequestStats: load.RequestData{Succeeded: rpcSucceeded}},
		},
		TotalDrops: dropWithCategory + dropWithCB,
		Drops: map[string]uint64{
			testDropCategory: dropWithCategory,
		},
	}}

	var rpcsToBeDone []balancer.PickResult
	// Run the picks, but only pick with sc1 will be done later.
	for i := 0; i < rpcCount; i++ {
		scst, _ := p1.Pick(balancer.PickInfo{})
		if scst.Done != nil && scst.SubConn != sc1 {
			rpcsToBeDone = append(rpcsToBeDone, scst)
		}
	}
	// Call done on those sc1 picks.
	for _, scst := range rpcsToBeDone {
		scst.Done(balancer.DoneInfo{})
	}

	gotStoreData := loadStore.Stats(testClusterNames[0:1])
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(load.Data{}, "ReportInterval")); diff != "" {
		t.Errorf("store.stats() returned unexpected diff (-want +got):\n%s", diff)
	}
}

// TestEDS_LoadReportDisabled covers the case that LRS is disabled. It makes
// sure the EDS implementation isn't broken (doesn't panic).
func (s) TestEDS_LoadReportDisabled(t *testing.T) {
	lsWrapper := &loadStoreWrapper{}
	lsWrapper.updateServiceName(testClusterNames[0])
	// Not calling lsWrapper.updateLoadStore(loadStore) because LRS is disabled.

	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, lsWrapper, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	// One localities, with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))
	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Test roundrobin with two subconns.
	p1 := <-cc.NewPickerCh
	// We call picks to make sure they don't panic.
	for i := 0; i < 10; i++ {
		p1.Pick(balancer.PickInfo{})
	}
}

// TestEDS_ClusterNameInAddressAttributes covers the case that cluster name is
// attached to the subconn address attributes.
func (s) TestEDS_ClusterNameInAddressAttributes(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	edsb := newEDSBalancerImpl(cc, balancer.BuildOptions{}, nil, nil, nil)
	edsb.enqueueChildBalancerStateUpdate = edsb.updateState

	const clusterName1 = "cluster-name-1"
	edsb.updateClusterName(clusterName1)

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab1.Build()))

	addrs1 := <-cc.NewSubConnAddrsCh
	if got, want := addrs1[0].Addr, testEndpointAddrs[0]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	cn, ok := internal.GetXDSHandshakeClusterName(addrs1[0].Attributes)
	if !ok || cn != clusterName1 {
		t.Fatalf("sc is created with addr with cluster name %v, %v, want cluster name %v", cn, ok, clusterName1)
	}

	sc1 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc1, connectivity.Ready)

	// Pick with only the first backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Change cluster name.
	const clusterName2 = "cluster-name-2"
	edsb.updateClusterName(clusterName2)

	// Change backend.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[1:2], nil)
	edsb.handleEDSResponse(parseEDSRespProtoForTesting(clab2.Build()))

	addrs2 := <-cc.NewSubConnAddrsCh
	if got, want := addrs2[0].Addr, testEndpointAddrs[1]; got != want {
		t.Fatalf("sc is created with addr %v, want %v", got, want)
	}
	// New addresses should have the new cluster name.
	cn2, ok := internal.GetXDSHandshakeClusterName(addrs2[0].Attributes)
	if !ok || cn2 != clusterName2 {
		t.Fatalf("sc is created with addr with cluster name %v, %v, want cluster name %v", cn2, ok, clusterName1)
	}

	sc2 := <-cc.NewSubConnCh
	edsb.handleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.handleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc2)
		}
	}
}
