/*
 *
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
 *
 */

package weightedtarget

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
	"google.golang.org/grpc/xds/internal/testutils"
)

type testConfigBalancerBuilder struct {
	balancer.Builder
}

func newTestConfigBalancerBuilder() *testConfigBalancerBuilder {
	return &testConfigBalancerBuilder{
		Builder: balancer.Get(roundrobin.Name),
	}
}

func (t *testConfigBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	rr := t.Builder.Build(cc, opts)
	return &testConfigBalancer{
		Balancer: rr,
	}
}

const testConfigBalancerName = "test_config_balancer"

func (t *testConfigBalancerBuilder) Name() string {
	return testConfigBalancerName
}

type stringBalancerConfig struct {
	serviceconfig.LoadBalancingConfig
	s string
}

func (t *testConfigBalancerBuilder) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	// Return string without quotes.
	return stringBalancerConfig{s: string(c[1 : len(c)-1])}, nil
}

// testConfigBalancer is a roundrobin balancer, but it takes the balancer config
// string and append it to the backend addresses.
type testConfigBalancer struct {
	balancer.Balancer
}

func (b *testConfigBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	c, ok := s.BalancerConfig.(stringBalancerConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type %T", s.BalancerConfig)
	}
	oneMoreAddr := resolver.Address{Addr: c.s}
	s.BalancerConfig = nil
	s.ResolverState.Addresses = append(s.ResolverState.Addresses, oneMoreAddr)
	return b.Balancer.UpdateClientConnState(s)
}

func (b *testConfigBalancer) Close() {
	b.Balancer.Close()
}

var (
	wtbBuilder          balancer.Builder
	wtbParser           balancer.ConfigParser
	testBackendAddrStrs []string
)

const testBackendAddrsCount = 12

func init() {
	balancer.Register(newTestConfigBalancerBuilder())
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
	wtbBuilder = balancer.Get(Name)
	wtbParser = wtbBuilder.(balancer.ConfigParser)

	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

// TestWeightedTarget covers the cases that a sub-balancer is added and a
// sub-balancer is removed. It verifies that the addresses and balancer configs
// are forwarded to the right sub-balancer.
//
// This test is intended to test the glue code in weighted_target. Most of the
// functionality tests are covered by the balancer group tests.
func TestWeightedTarget(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	wtb := wtbBuilder.Build(cc, balancer.BuildOptions{})

	// Start with "cluster_1: round_robin".
	config1, err := wtbParser.ParseConfig([]byte(`{"targets":{"cluster_1":{"weight":1,"childPolicy":[{"round_robin":""}]}}}`))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddr1 := resolver.Address{Addr: testBackendAddrStrs[0], Attributes: nil}
	if err := wtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddr1, []string{"cluster_1"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	addr1 := <-cc.NewSubConnAddrsCh
	if want := []resolver.Address{
		hierarchy.Set(wantAddr1, []string{}),
	}; !cmp.Equal(addr1, want, cmp.AllowUnexported(attributes.Attributes{})) {
		t.Fatalf("got unexpected new subconn addrs: %v", cmp.Diff(addr1, want, cmp.AllowUnexported(attributes.Attributes{})))
	}

	// Send subconn state change.
	sc1 := <-cc.NewSubConnCh
	wtb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	wtb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Remove cluster_1, and add "cluster_2: test_config_balancer".
	wantAddr3Str := testBackendAddrStrs[2]
	config2, err := wtbParser.ParseConfig([]byte(
		fmt.Sprintf(`{"targets":{"cluster_2":{"weight":1,"childPolicy":[{%q:%q}]}}}`, testConfigBalancerName, wantAddr3Str),
	))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and one address with hierarchy path "cluster_2".
	wantAddr2 := resolver.Address{Addr: testBackendAddrStrs[1], Attributes: nil}
	if err := wtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddr2, []string{"cluster_2"}),
		}},
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Expect the address sent in the address list. The hierarchy path should be
	// cleared.
	addr2 := <-cc.NewSubConnAddrsCh
	if want := []resolver.Address{
		hierarchy.Set(wantAddr2, []string{}),
	}; !cmp.Equal(addr2, want, cmp.AllowUnexported(attributes.Attributes{})) {
		t.Fatalf("got unexpected new subconn addrs: %v", cmp.Diff(addr2, want, cmp.AllowUnexported(attributes.Attributes{})))
	}
	// Expect the other address sent as balancer config. This address doesn't
	// have hierarchy path.
	wantAddr3 := resolver.Address{Addr: wantAddr3Str, Attributes: nil}
	addr3 := <-cc.NewSubConnAddrsCh
	if want := []resolver.Address{wantAddr3}; !cmp.Equal(addr3, want, cmp.AllowUnexported(attributes.Attributes{})) {
		t.Fatalf("got unexpected new subconn addrs: %v", cmp.Diff(addr3, want, cmp.AllowUnexported(attributes.Attributes{})))
	}

	// The subconn for cluster_1 should be removed.
	scToRemove := <-cc.RemoveSubConnCh
	if !cmp.Equal(scToRemove, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
		t.Fatalf("RemoveSubConn, want %v, got %v", sc1, scToRemove)
	}
	wtb.UpdateSubConnState(scToRemove, balancer.SubConnState{ConnectivityState: connectivity.Shutdown})

	sc2 := <-cc.NewSubConnCh
	wtb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	wtb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	sc3 := <-cc.NewSubConnCh
	wtb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	wtb.UpdateSubConnState(sc3, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test roundrobin pick with backends in cluster_2.
	p2 := <-cc.NewPickerCh
	want := []balancer.SubConn{sc2, sc3}
	if err := testutils.IsRoundRobin(want, subConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	// Replace child policy of "cluster_1" to "round_robin".
	config3, err := wtbParser.ParseConfig([]byte(`{"targets":{"cluster_2":{"weight":1,"childPolicy":[{"round_robin":""}]}}}`))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddr4 := resolver.Address{Addr: testBackendAddrStrs[0], Attributes: nil}
	if err := wtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddr4, []string{"cluster_2"}),
		}},
		BalancerConfig: config3,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	addr4 := <-cc.NewSubConnAddrsCh
	if want := []resolver.Address{
		hierarchy.Set(wantAddr4, []string{}),
	}; !cmp.Equal(addr4, want, cmp.AllowUnexported(attributes.Attributes{})) {
		t.Fatalf("got unexpected new subconn addrs: %v", cmp.Diff(addr4, want, cmp.AllowUnexported(attributes.Attributes{})))
	}

	// Send subconn state change.
	sc4 := <-cc.NewSubConnCh
	wtb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	wtb.UpdateSubConnState(sc4, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with one backend.
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p3.Pick(balancer.PickInfo{})
		if !cmp.Equal(gotSCSt.SubConn, sc4, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc4)
		}
	}
}

func subConnFromPicker(p balancer.Picker) func() balancer.SubConn {
	return func() balancer.SubConn {
		scst, _ := p.Pick(balancer.PickInfo{})
		return scst.SubConn
	}
}

const initIdleBalancerName = "test-init-Idle-balancer"

var errTestInitIdle = fmt.Errorf("init Idle balancer error 0")

func init() {
	stub.Register(initIdleBalancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, opts balancer.ClientConnState) error {
			bd.ClientConn.NewSubConn(opts.ResolverState.Addresses, balancer.NewSubConnOptions{})
			return nil
		},
		UpdateSubConnState: func(bd *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			err := fmt.Errorf("wrong picker error")
			if state.ConnectivityState == connectivity.Idle {
				err = errTestInitIdle
			}
			bd.ClientConn.UpdateState(balancer.State{
				ConnectivityState: state.ConnectivityState,
				Picker:            &testutils.TestConstPicker{Err: err},
			})
		},
	})
}

// TestInitialIdle covers the case that if the child reports Idle, the overall
// state will be Idle.
func TestInitialIdle(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	wtb := wtbBuilder.Build(cc, balancer.BuildOptions{})

	// Start with "cluster_1: round_robin".
	config1, err := wtbParser.ParseConfig([]byte(`{"targets":{"cluster_1":{"weight":1,"childPolicy":[{"test-init-Idle-balancer":""}]}}}`))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], Attributes: nil},
	}
	if err := wtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		sc := <-cc.NewSubConnCh
		wtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	}

	if state1 := <-cc.NewStateCh; state1 != connectivity.Idle {
		t.Fatalf("Received aggregated state: %v, want Idle", state1)
	}
}
