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
	"context"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	typepb "github.com/envoyproxy/go-control-plane/envoy/type"
	typespb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal"
)

var (
	testClusterNames  = []string{"test-cluster-1", "test-cluster-2"}
	testSubZones      = []string{"I", "II", "III", "IV"}
	testEndpointAddrs []string
)

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testEndpointAddrs = append(testEndpointAddrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
}

type clusterLoadAssignmentBuilder struct {
	v *xdspb.ClusterLoadAssignment
}

func newClusterLoadAssignmentBuilder(clusterName string, dropPercents []uint32) *clusterLoadAssignmentBuilder {
	var drops []*xdspb.ClusterLoadAssignment_Policy_DropOverload
	for i, d := range dropPercents {
		drops = append(drops, &xdspb.ClusterLoadAssignment_Policy_DropOverload{
			Category: fmt.Sprintf("test-drop-%d", i),
			DropPercentage: &typepb.FractionalPercent{
				Numerator:   d,
				Denominator: typepb.FractionalPercent_HUNDRED,
			},
		})
	}

	return &clusterLoadAssignmentBuilder{
		v: &xdspb.ClusterLoadAssignment{
			ClusterName: clusterName,
			Policy: &xdspb.ClusterLoadAssignment_Policy{
				DropOverloads: drops,
			},
		},
	}
}

type addLocalityOptions struct {
	health []corepb.HealthStatus
}

func (clab *clusterLoadAssignmentBuilder) addLocality(subzone string, weight uint32, addrsWithPort []string, opts *addLocalityOptions) {
	var lbEndPoints []*endpointpb.LbEndpoint
	for i, a := range addrsWithPort {
		host, portStr, err := net.SplitHostPort(a)
		if err != nil {
			panic("failed to split " + a)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			panic("failed to atoi " + portStr)
		}

		lbe := &endpointpb.LbEndpoint{
			HostIdentifier: &endpointpb.LbEndpoint_Endpoint{
				Endpoint: &endpointpb.Endpoint{
					Address: &corepb.Address{
						Address: &corepb.Address_SocketAddress{
							SocketAddress: &corepb.SocketAddress{
								Protocol: corepb.SocketAddress_TCP,
								Address:  host,
								PortSpecifier: &corepb.SocketAddress_PortValue{
									PortValue: uint32(port)}}}}}},
		}
		if opts != nil && i < len(opts.health) {
			lbe.HealthStatus = opts.health[i]
		}
		lbEndPoints = append(lbEndPoints, lbe)
	}

	clab.v.Endpoints = append(clab.v.Endpoints, &endpointpb.LocalityLbEndpoints{
		Locality: &corepb.Locality{
			Region:  "",
			Zone:    "",
			SubZone: subzone,
		},
		LbEndpoints:         lbEndPoints,
		LoadBalancingWeight: &typespb.UInt32Value{Value: weight},
	})
}

func (clab *clusterLoadAssignmentBuilder) build() *xdspb.ClusterLoadAssignment {
	return clab.v
}

// One locality
//  - add backend
//  - remove backend
//  - replace backend
//  - change drop rate
func TestEDS_OneLocality(t *testing.T) {
	cc := newTestClientConn(t)
	edsb := NewXDSBalancer(cc, nil)

	// One locality with one backend.
	clab1 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.addLocality(testSubZones[0], 1, testEndpointAddrs[:1], nil)
	edsb.HandleEDSResponse(clab1.build())

	sc1 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc1, connectivity.Ready)

	// Pick with only the first backend.
	p1 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc1) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc1)
		}
	}

	// The same locality, add one more backend.
	clab2 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.addLocality(testSubZones[0], 1, testEndpointAddrs[:2], nil)
	edsb.HandleEDSResponse(clab2.build())

	sc2 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p2 := <-cc.newPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p2.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// The same locality, delete first backend.
	clab3 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.addLocality(testSubZones[0], 1, testEndpointAddrs[1:2], nil)
	edsb.HandleEDSResponse(clab3.build())

	scToRemove := <-cc.removeSubConnCh
	if !reflect.DeepEqual(scToRemove, sc1) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.HandleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with only the second subconn.
	p3 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p3.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc2) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc2)
		}
	}

	// The same locality, replace backend.
	clab4 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.addLocality(testSubZones[0], 1, testEndpointAddrs[2:3], nil)
	edsb.HandleEDSResponse(clab4.build())

	sc3 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc3, connectivity.Ready)
	scToRemove = <-cc.removeSubConnCh
	if !reflect.DeepEqual(scToRemove, sc2) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove)
	}
	edsb.HandleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with only the third subconn.
	p4 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		gotSC, _, _ := p4.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(gotSC, sc3) {
			t.Fatalf("picker.Pick, got %v, want %v", gotSC, sc3)
		}
	}

	// The same locality, different drop rate, dropping 50%.
	clab5 := newClusterLoadAssignmentBuilder(testClusterNames[0], []uint32{50})
	clab5.addLocality(testSubZones[0], 1, testEndpointAddrs[2:3], nil)
	edsb.HandleEDSResponse(clab5.build())

	// Picks with drops.
	p5 := <-cc.newPickerCh
	for i := 0; i < 100; i++ {
		_, _, err := p5.Pick(context.Background(), balancer.PickOptions{})
		// TODO: the dropping algorithm needs a design. When the dropping algorithm
		// is fixed, this test also needs fix.
		if i < 50 && err == nil {
			t.Errorf("The first 50%% picks should be drops, got error <nil>")
		} else if i > 50 && err != nil {
			t.Errorf("The second 50%% picks should be non-drops, got error %v", err)
		}
	}
}

// 2 locality
//  - start with 2 locality
//  - add locality
//  - remove locality
//  - address change for the <not-the-first> locality
//  - update locality weight
func TestEDS_TwoLocalities(t *testing.T) {
	cc := newTestClientConn(t)
	edsb := NewXDSBalancer(cc, nil)

	// Two localities, each with one backend.
	clab1 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.addLocality(testSubZones[0], 1, testEndpointAddrs[:1], nil)
	clab1.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	edsb.HandleEDSResponse(clab1.build())

	sc1 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc1, connectivity.Ready)
	sc2 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p1 := <-cc.newPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Add another locality, with one backend.
	clab2 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab2.addLocality(testSubZones[0], 1, testEndpointAddrs[:1], nil)
	clab2.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	clab2.addLocality(testSubZones[2], 1, testEndpointAddrs[2:3], nil)
	edsb.HandleEDSResponse(clab2.build())

	sc3 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc3, connectivity.Ready)

	// Test roundrobin with three subconns.
	p2 := <-cc.newPickerCh
	want = []balancer.SubConn{sc1, sc2, sc3}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p2.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Remove first locality.
	clab3 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab3.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	clab3.addLocality(testSubZones[2], 1, testEndpointAddrs[2:3], nil)
	edsb.HandleEDSResponse(clab3.build())

	scToRemove := <-cc.removeSubConnCh
	if !reflect.DeepEqual(scToRemove, sc1) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	edsb.HandleSubConnStateChange(scToRemove, connectivity.Shutdown)

	// Test pick with two subconns (without the first one).
	p3 := <-cc.newPickerCh
	want = []balancer.SubConn{sc2, sc3}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p3.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Add a backend to the last locality.
	clab4 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab4.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	clab4.addLocality(testSubZones[2], 1, testEndpointAddrs[2:4], nil)
	edsb.HandleEDSResponse(clab4.build())

	sc4 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc4, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc4, connectivity.Ready)

	// Test pick with two subconns (without the first one).
	p4 := <-cc.newPickerCh
	// Locality-1 will be picked twice, and locality-2 will be picked twice.
	// Locality-1 contains only sc2, locality-2 contains sc3 and sc4. So expect
	// two sc2's and sc3, sc4.
	want = []balancer.SubConn{sc2, sc2, sc3, sc4}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p4.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Change weight of the locality[1].
	clab5 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab5.addLocality(testSubZones[1], 2, testEndpointAddrs[1:2], nil)
	clab5.addLocality(testSubZones[2], 1, testEndpointAddrs[2:4], nil)
	edsb.HandleEDSResponse(clab5.build())

	// Test pick with two subconns different locality weight.
	p5 := <-cc.newPickerCh
	// Locality-1 will be picked four times, and locality-2 will be picked twice
	// (weight 2 and 1). Locality-1 contains only sc2, locality-2 contains sc3 and
	// sc4. So expect four sc2's and sc3, sc4.
	want = []balancer.SubConn{sc2, sc2, sc2, sc2, sc3, sc4}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p5.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Change weight of the locality[1] to 0, it should never be picked.
	clab6 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab6.addLocality(testSubZones[1], 0, testEndpointAddrs[1:2], nil)
	clab6.addLocality(testSubZones[2], 1, testEndpointAddrs[2:4], nil)
	edsb.HandleEDSResponse(clab6.build())

	// Changing weight of locality[1] to 0 caused it to be removed. It's subconn
	// should also be removed.
	//
	// NOTE: this is because we handle locality with weight 0 same as the
	// locality doesn't exist. If this changes in the future, this removeSubConn
	// behavior will also change.
	scToRemove2 := <-cc.removeSubConnCh
	if !reflect.DeepEqual(scToRemove2, sc2) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc2, scToRemove2)
	}

	// Test pick with two subconns different locality weight.
	p6 := <-cc.newPickerCh
	// Locality-1 will be not be picked, and locality-2 will be picked.
	// Locality-2 contains sc3 and sc4. So expect sc3, sc4.
	want = []balancer.SubConn{sc3, sc4}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p6.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// The EDS balancer gets EDS resp with unhealthy endpoints. Test that only
// healthy ones are used.
func TestEDS_EndpointsHealth(t *testing.T) {
	cc := newTestClientConn(t)
	edsb := NewXDSBalancer(cc, nil)

	// Two localities, each 3 backend, one Healthy, one Unhealthy, one Unknown.
	clab1 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.addLocality(testSubZones[0], 1, testEndpointAddrs[:6], &addLocalityOptions{
		health: []corepb.HealthStatus{
			corepb.HealthStatus_HEALTHY,
			corepb.HealthStatus_UNHEALTHY,
			corepb.HealthStatus_UNKNOWN,
			corepb.HealthStatus_DRAINING,
			corepb.HealthStatus_TIMEOUT,
			corepb.HealthStatus_DEGRADED,
		},
	})
	clab1.addLocality(testSubZones[1], 1, testEndpointAddrs[6:12], &addLocalityOptions{
		health: []corepb.HealthStatus{
			corepb.HealthStatus_HEALTHY,
			corepb.HealthStatus_UNHEALTHY,
			corepb.HealthStatus_UNKNOWN,
			corepb.HealthStatus_DRAINING,
			corepb.HealthStatus_TIMEOUT,
			corepb.HealthStatus_DEGRADED,
		},
	})
	edsb.HandleEDSResponse(clab1.build())

	var (
		readySCs           []balancer.SubConn
		newSubConnAddrStrs []string
	)
	for i := 0; i < 4; i++ {
		addr := <-cc.newSubConnAddrsCh
		newSubConnAddrStrs = append(newSubConnAddrStrs, addr[0].Addr)
		sc := <-cc.newSubConnCh
		edsb.HandleSubConnStateChange(sc, connectivity.Connecting)
		edsb.HandleSubConnStateChange(sc, connectivity.Ready)
		readySCs = append(readySCs, sc)
	}

	wantNewSubConnAddrStrs := []string{
		testEndpointAddrs[0],
		testEndpointAddrs[2],
		testEndpointAddrs[6],
		testEndpointAddrs[8],
	}
	sortStrTrans := cmp.Transformer("Sort", func(in []string) []string {
		sort.Strings(in)
		return in
	})
	if !cmp.Equal(newSubConnAddrStrs, wantNewSubConnAddrStrs, sortStrTrans) {
		t.Fatalf("want newSubConn with address %v, got %v", wantNewSubConnAddrStrs, newSubConnAddrStrs)
	}

	// There should be exactly 4 new SubConns. Check to make sure there's no
	// more subconns being created.
	select {
	case <-cc.newSubConnCh:
		t.Fatalf("Got unexpected new subconn")
	case <-time.After(time.Microsecond * 100):
	}

	// Test roundrobin with the subconns.
	p1 := <-cc.newPickerCh
	want := readySCs
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func TestClose(t *testing.T) {
	edsb := NewXDSBalancer(nil, nil)
	// This is what could happen when switching between fallback and eds. This
	// make sure it doesn't panic.
	edsb.Close()
}

func init() {
	balancer.Register(&testConstBalancerBuilder{})
}

var errTestConstPicker = fmt.Errorf("const picker error")

type testConstBalancerBuilder struct{}

func (*testConstBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &testConstBalancer{cc: cc}
}

func (*testConstBalancerBuilder) Name() string {
	return "test-const-balancer"
}

type testConstBalancer struct {
	cc balancer.ClientConn
}

func (tb *testConstBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	tb.cc.UpdateBalancerState(connectivity.Ready, &testConstPicker{err: errTestConstPicker})
}

func (tb *testConstBalancer) HandleResolvedAddrs([]resolver.Address, error) {
	tb.cc.UpdateBalancerState(connectivity.Ready, &testConstPicker{err: errTestConstPicker})
}

func (*testConstBalancer) Close() {
}

type testConstPicker struct {
	err error
	sc  balancer.SubConn
}

func (tcp *testConstPicker) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	if tcp.err != nil {
		return nil, nil, tcp.err
	}
	return tcp.sc, nil, nil
}

// Create XDS balancer, and update sub-balancer before handling eds responses.
// Then switch between round-robin and test-const-balancer after handling first
// eds response.
func TestEDS_UpdateSubBalancerName(t *testing.T) {
	cc := newTestClientConn(t)
	edsb := NewXDSBalancer(cc, nil)

	t.Logf("update sub-balancer to test-const-balancer")
	edsb.HandleChildPolicy("test-const-balancer", nil)

	// Two localities, each with one backend.
	clab1 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.addLocality(testSubZones[0], 1, testEndpointAddrs[:1], nil)
	clab1.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	edsb.HandleEDSResponse(clab1.build())

	p0 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		_, _, err := p0.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(err, errTestConstPicker) {
			t.Fatalf("picker.Pick, got err %q, want err %q", err, errTestConstPicker)
		}
	}

	t.Logf("update sub-balancer to round-robin")
	edsb.HandleChildPolicy(roundrobin.Name, nil)

	sc1 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc1, connectivity.Ready)
	sc2 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc2, connectivity.Ready)

	// Test roundrobin with two subconns.
	p1 := <-cc.newPickerCh
	want := []balancer.SubConn{sc1, sc2}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	t.Logf("update sub-balancer to test-const-balancer")
	edsb.HandleChildPolicy("test-const-balancer", nil)

	for i := 0; i < 2; i++ {
		scToRemove := <-cc.removeSubConnCh
		if !reflect.DeepEqual(scToRemove, sc1) && !reflect.DeepEqual(scToRemove, sc2) {
			t.Fatalf("RemoveSubConn, want (%v or %v), got %v", sc1, sc2, scToRemove)
		}
		edsb.HandleSubConnStateChange(scToRemove, connectivity.Shutdown)
	}

	p2 := <-cc.newPickerCh
	for i := 0; i < 5; i++ {
		_, _, err := p2.Pick(context.Background(), balancer.PickOptions{})
		if !reflect.DeepEqual(err, errTestConstPicker) {
			t.Fatalf("picker.Pick, got err %q, want err %q", err, errTestConstPicker)
		}
	}

	t.Logf("update sub-balancer to round-robin")
	edsb.HandleChildPolicy(roundrobin.Name, nil)

	sc3 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc3, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc3, connectivity.Ready)
	sc4 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc4, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc4, connectivity.Ready)

	p3 := <-cc.newPickerCh
	want = []balancer.SubConn{sc3, sc4}
	if err := isRoundRobin(want, func() balancer.SubConn {
		sc, _, _ := p3.Pick(context.Background(), balancer.PickOptions{})
		return sc
	}); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

func TestDropPicker(t *testing.T) {
	const pickCount = 12
	var constPicker = &testConstPicker{
		sc: testSubConns[0],
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
				newDropper(1, 2, ""),
			},
		},
		{
			name: "two drops",
			drops: []*dropper{
				newDropper(1, 3, ""),
				newDropper(1, 2, ""),
			},
		},
		{
			name: "three drops",
			drops: []*dropper{
				newDropper(1, 3, ""),
				newDropper(1, 4, ""),
				newDropper(1, 2, ""),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			p := newDropPicker(constPicker, tt.drops, nil)

			// scCount is the number of sc's returned by pick. The opposite of
			// drop-count.
			var (
				scCount   int
				wantCount = pickCount
			)
			for _, dp := range tt.drops {
				wantCount = wantCount * int(dp.denominator-dp.numerator) / int(dp.denominator)
			}

			for i := 0; i < pickCount; i++ {
				_, _, err := p.Pick(context.Background(), balancer.PickOptions{})
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

func TestEDS_LoadReport(t *testing.T) {
	testLoadStore := newTestLoadStore()

	cc := newTestClientConn(t)
	edsb := NewXDSBalancer(cc, testLoadStore)

	backendToBalancerID := make(map[balancer.SubConn]internal.Locality)

	// Two localities, each with one backend.
	clab1 := newClusterLoadAssignmentBuilder(testClusterNames[0], nil)
	clab1.addLocality(testSubZones[0], 1, testEndpointAddrs[:1], nil)
	clab1.addLocality(testSubZones[1], 1, testEndpointAddrs[1:2], nil)
	edsb.HandleEDSResponse(clab1.build())

	sc1 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc1, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc1, connectivity.Ready)
	backendToBalancerID[sc1] = internal.Locality{
		SubZone: testSubZones[0],
	}
	sc2 := <-cc.newSubConnCh
	edsb.HandleSubConnStateChange(sc2, connectivity.Connecting)
	edsb.HandleSubConnStateChange(sc2, connectivity.Ready)
	backendToBalancerID[sc2] = internal.Locality{
		SubZone: testSubZones[1],
	}

	// Test roundrobin with two subconns.
	p1 := <-cc.newPickerCh
	var (
		wantStart []internal.Locality
		wantEnd   []internal.Locality
	)

	for i := 0; i < 10; i++ {
		sc, done, _ := p1.Pick(context.Background(), balancer.PickOptions{})
		locality := backendToBalancerID[sc]
		wantStart = append(wantStart, locality)
		if done != nil && sc != sc1 {
			done(balancer.DoneInfo{})
			wantEnd = append(wantEnd, backendToBalancerID[sc])
		}
	}

	if !reflect.DeepEqual(testLoadStore.callsStarted, wantStart) {
		t.Fatalf("want started: %v, got: %v", testLoadStore.callsStarted, wantStart)
	}
	if !reflect.DeepEqual(testLoadStore.callsEnded, wantEnd) {
		t.Fatalf("want ended: %v, got: %v", testLoadStore.callsEnded, wantEnd)
	}
}
