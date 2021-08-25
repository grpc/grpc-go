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

package clusterresolver

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/balancer/weightedtarget"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
	"google.golang.org/grpc/xds/internal/xdsclient"
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
	clusterimpl.NewRandomWRR = testutils.NewTestWRR
	weightedtarget.NewRandomWRR = testutils.NewTestWRR
	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond * 100
}

func setupTestEDS(t *testing.T, initChild *internalserviceconfig.BalancerConfig) (balancer.Balancer, *testutils.TestClientConn, *fakeclient.Client, func()) {
	xdsC := fakeclient.NewClientWithName(testBalancerNameFooBar)
	cc := testutils.NewTestClientConn(t)
	builder := balancer.Get(Name)
	edsb := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testEDSServcie}})
	if edsb == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := edsb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: xdsclient.SetClient(resolver.State{}, xdsC),
		BalancerConfig: &LBConfig{
			DiscoveryMechanisms: []DiscoveryMechanism{{
				Cluster: testClusterName,
				Type:    DiscoveryMechanismTypeEDS,
			}},
		},
	}); err != nil {
		edsb.Close()
		xdsC.Close()
		t.Fatal(err)
	}
	if _, err := xdsC.WaitForWatchEDS(ctx); err != nil {
		edsb.Close()
		xdsC.Close()
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	return edsb, cc, xdsC, func() {
		edsb.Close()
		xdsC.Close()
	}
}

// One locality
//  - add backend
//  - remove backend
//  - replace backend
//  - change drop rate
func (s) TestEDS_OneLocality(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Pick with only the first backend.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	// The same locality, add one more backend.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)

	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with two subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1, sc2}); err != nil {
		t.Fatal(err)
	}

	// The same locality, delete first backend.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab3.Build()), nil)

	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	// Test pick with only the second subconn.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}

	// The same locality, replace backend.
	clab4 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab4.Build()), nil)

	sc3 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	scToRemove = <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc2, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	// Test pick with only the third subconn.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc3}); err != nil {
		t.Fatal(err)
	}

	// The same locality, different drop rate, dropping 50%.
	clab5 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], map[string]uint32{"test-drop": 50})
	clab5.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab5.Build()), nil)

	// Picks with drops.
	if err := testPickerFromCh(cc.NewPickerCh, func(p balancer.Picker) error {
		for i := 0; i < 100; i++ {
			_, err := p.Pick(balancer.PickInfo{})
			// TODO: the dropping algorithm needs a design. When the dropping algorithm
			// is fixed, this test also needs fix.
			if i%2 == 0 && err == nil {
				return fmt.Errorf("%d - the even number picks should be drops, got error <nil>", i)
			} else if i%2 != 0 && err != nil {
				return fmt.Errorf("%d - the odd number picks should be non-drops, got error %v", i, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The same locality, remove drops.
	clab6 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab6.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab6.Build()), nil)

	// Pick without drops.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc3}); err != nil {
		t.Fatal(err)
	}
}

// 2 locality
//  - start with 2 locality
//  - add locality
//  - remove locality
//  - address change for the <not-the-first> locality
//  - update locality weight
func (s) TestEDS_TwoLocalities(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	// Two localities, each with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Add the second locality later to make sure sc2 belongs to the second
	// locality. Otherwise the test is flaky because of a map is used in EDS to
	// keep localities.
	clab1.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with two subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1, sc2}); err != nil {
		t.Fatal(err)
	}

	// Add another locality, with one backend.
	clab2 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	clab2.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab2.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab2.Build()), nil)

	sc3 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin with three subconns.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1, sc2, sc3}); err != nil {
		t.Fatal(err)
	}

	// Remove first locality.
	clab3 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab3.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:3], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab3.Build()), nil)

	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	// Test pick with two subconns (without the first one).
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2, sc3}); err != nil {
		t.Fatal(err)
	}

	// Add a backend to the last locality.
	clab4 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.AddLocality(testSubZones[1], 1, 0, testEndpointAddrs[1:2], nil)
	clab4.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab4.Build()), nil)

	sc4 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with two subconns (without the first one).
	//
	// Locality-1 will be picked twice, and locality-2 will be picked twice.
	// Locality-1 contains only sc2, locality-2 contains sc3 and sc4. So expect
	// two sc2's and sc3, sc4.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2, sc2, sc3, sc4}); err != nil {
		t.Fatal(err)
	}

	// Change weight of the locality[1].
	clab5 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab5.AddLocality(testSubZones[1], 2, 0, testEndpointAddrs[1:2], nil)
	clab5.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab5.Build()), nil)

	// Test pick with two subconns different locality weight.
	//
	// Locality-1 will be picked four times, and locality-2 will be picked twice
	// (weight 2 and 1). Locality-1 contains only sc2, locality-2 contains sc3 and
	// sc4. So expect four sc2's and sc3, sc4.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2, sc2, sc2, sc2, sc3, sc4}); err != nil {
		t.Fatal(err)
	}

	// Change weight of the locality[1] to 0, it should never be picked.
	clab6 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab6.AddLocality(testSubZones[1], 0, 0, testEndpointAddrs[1:2], nil)
	clab6.AddLocality(testSubZones[2], 1, 0, testEndpointAddrs[2:4], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab6.Build()), nil)

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
	//
	// Locality-1 will be not be picked, and locality-2 will be picked.
	// Locality-2 contains sc3 and sc4. So expect sc3, sc4.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc3, sc4}); err != nil {
		t.Fatal(err)
	}
}

// The EDS balancer gets EDS resp with unhealthy endpoints. Test that only
// healthy ones are used.
func (s) TestEDS_EndpointsHealth(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

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
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	var (
		readySCs           []balancer.SubConn
		newSubConnAddrStrs []string
	)
	for i := 0; i < 4; i++ {
		addr := <-cc.NewSubConnAddrsCh
		newSubConnAddrStrs = append(newSubConnAddrStrs, addr[0].Addr)
		sc := <-cc.NewSubConnCh
		edsb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		edsb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
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
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, readySCs); err != nil {
		t.Fatal(err)
	}
}

// TestEDS_EmptyUpdate covers the cases when eds impl receives an empty update.
//
// It should send an error picker with transient failure to the parent.
func (s) TestEDS_EmptyUpdate(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	const cacheTimeout = 100 * time.Microsecond
	oldCacheTimeout := balancergroup.DefaultSubBalancerCloseTimeout
	balancergroup.DefaultSubBalancerCloseTimeout = cacheTimeout
	defer func() { balancergroup.DefaultSubBalancerCloseTimeout = oldCacheTimeout }()

	// The first update is an empty update.
	xdsC.InvokeWatchEDSCallback("", xdsclient.EndpointsUpdate{}, nil)
	// Pick should fail with transient failure, and all priority removed error.
	if err := testErrPickerFromCh(cc.NewPickerCh, priority.ErrAllPrioritiesRemoved); err != nil {
		t.Fatal(err)
	}

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Pick with only the first backend.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc1}); err != nil {
		t.Fatal(err)
	}

	xdsC.InvokeWatchEDSCallback("", xdsclient.EndpointsUpdate{}, nil)
	// Pick should fail with transient failure, and all priority removed error.
	if err := testErrPickerFromCh(cc.NewPickerCh, priority.ErrAllPrioritiesRemoved); err != nil {
		t.Fatal(err)
	}

	// Wait for the old SubConn to be removed (which happens when the child
	// policy is closed), so a new update would trigger a new SubConn (we need
	// this new SubConn to tell if the next picker is newly created).
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	// Handle another update with priorities and localities.
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)

	sc2 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Pick with only the first backend.
	if err := testRoundRobinPickerFromCh(cc.NewPickerCh, []balancer.SubConn{sc2}); err != nil {
		t.Fatal(err)
	}
}

func (s) TestEDS_CircuitBreaking(t *testing.T) {
	edsb, cc, xdsC, cleanup := setupTestEDS(t, nil)
	defer cleanup()

	var maxRequests uint32 = 50
	if err := edsb.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			DiscoveryMechanisms: []DiscoveryMechanism{{
				Cluster:               testClusterName,
				MaxConcurrentRequests: &maxRequests,
				Type:                  DiscoveryMechanismTypeEDS,
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// One locality with one backend.
	clab1 := testutils.NewClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.AddLocality(testSubZones[0], 1, 0, testEndpointAddrs[:1], nil)
	xdsC.InvokeWatchEDSCallback("", parseEDSRespProtoForTesting(clab1.Build()), nil)
	sc1 := <-cc.NewSubConnCh
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	edsb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

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
	if err := edsb.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &LBConfig{
			DiscoveryMechanisms: []DiscoveryMechanism{{
				Cluster:               testClusterName,
				MaxConcurrentRequests: &maxRequests2,
				Type:                  DiscoveryMechanismTypeEDS,
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

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
