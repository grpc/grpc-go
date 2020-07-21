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

package xdsrouting

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/metadata"
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

var (
	rtBuilder           balancer.Builder
	rtParser            balancer.ConfigParser
	testBackendAddrStrs []string
)

const ignoreAttrsRRName = "ignore_attrs_round_robin"

type ignoreAttrsRRBuilder struct {
	balancer.Builder
}

func (trrb *ignoreAttrsRRBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &ignoreAttrsRRBalancer{trrb.Builder.Build(cc, opts)}
}

func (*ignoreAttrsRRBuilder) Name() string {
	return ignoreAttrsRRName
}

// ignoreAttrsRRBalancer clears attributes from all addresses.
//
// It's necessary in this tests because hierarchy modifies address.Attributes.
// Even if rr gets addresses with empty hierarchy, the attributes fields are
// different. This is a temporary walkaround for the tests to ignore attributes.
// Eventually, we need a way for roundrobin to know that two addresses with
// empty attributes are equal.
//
// TODO: delete this when the issue is resolved:
// https://github.com/grpc/grpc-go/issues/3611.
type ignoreAttrsRRBalancer struct {
	balancer.Balancer
}

func (trrb *ignoreAttrsRRBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	var newAddrs []resolver.Address
	for _, a := range s.ResolverState.Addresses {
		a.Attributes = nil
		newAddrs = append(newAddrs, a)
	}
	s.ResolverState.Addresses = newAddrs
	return trrb.Balancer.UpdateClientConnState(s)
}

const testBackendAddrsCount = 12

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
	rtBuilder = balancer.Get(xdsRoutingName)
	rtParser = rtBuilder.(balancer.ConfigParser)

	balancer.Register(&ignoreAttrsRRBuilder{balancer.Get(roundrobin.Name)})

	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

func testPick(t *testing.T, p balancer.Picker, info balancer.PickInfo, wantSC balancer.SubConn, wantErr error) {
	t.Helper()
	for i := 0; i < 5; i++ {
		gotSCSt, err := p.Pick(info)
		if err != wantErr {
			t.Fatalf("picker.Pick(%+v), got error %v, want %v", info, err, wantErr)
		}
		if !cmp.Equal(gotSCSt.SubConn, wantSC, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick(%+v), got %v, want SubConn=%v", info, gotSCSt, wantSC)
		}
	}
}

func TestRouting(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"prefix":"", "headers":[{"name":"header-1", "exactMatch":"value-1"}], "action":"cds:cluster_2"}
]
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], Attributes: nil},
		{Addr: testBackendAddrStrs[1], Attributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		if len(hierarchy.Get(addrs[0])) != 0 {
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].Attributes = nil
		m1[addrs[0]] = sc
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/z/y",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("header-1", "value-1")),
			},
			wantSC:  m1[wantAddrs[1]],
			wantErr: nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/z/y",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("h", "v")),
			},
			wantSC:  nil,
			wantErr: errNoMatchedRouteFound,
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestRoutingConfigUpdateAddRoute covers the cases the routing balancer
// receives config update with extra route, but the same actions.
func TestRoutingConfigUpdateAddRoute(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"path":"/z/y", "action":"cds:cluster_2"}
]
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], Attributes: nil},
		{Addr: testBackendAddrStrs[1], Attributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		if len(hierarchy.Get(addrs[0])) != 0 {
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].Attributes = nil
		m1[addrs[0]] = sc
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m1[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/c/d"},
			wantSC:   nil,
			wantErr:  errNoMatchedRouteFound,
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with different routes, but the same actions. Expect a
	// picker update, but no subconn changes.
	configJSON2 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"", "headers":[{"name":"header-1", "presentMatch":true}], "action":"cds:cluster_2"},
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"path":"/z/y", "action":"cds:cluster_2"}
]
}`
	config2, err := rtParser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	// Send update with the same addresses.
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// New change to actions, expect no newSubConn.
	select {
	case <-time.After(time.Millisecond * 500):
	case <-cc.NewSubConnCh:
		addrs := <-cc.NewSubConnAddrsCh
		t.Fatalf("unexpected NewSubConn with address %v", addrs)
	}

	p2 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m1[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/a/z",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("header-1", "value-1")),
			},
			wantSC:  m1[wantAddrs[1]],
			wantErr: nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/c/d",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("h", "v")),
			},
			wantSC:  nil,
			wantErr: errNoMatchedRouteFound,
		},
	} {
		testPick(t, p2, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestRoutingConfigUpdateAddRouteAndAction covers the cases the routing
// balancer receives config update with extra route and actions.
func TestRoutingConfigUpdateAddRouteAndAction(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"path":"/z/y", "action":"cds:cluster_2"}
]
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], Attributes: nil},
		{Addr: testBackendAddrStrs[1], Attributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		if len(hierarchy.Get(addrs[0])) != 0 {
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].Attributes = nil
		m1[addrs[0]] = sc
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m1[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/c/d"},
			wantSC:   nil,
			wantErr:  errNoMatchedRouteFound,
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with different routes, and different actions. Expect a
	// new subconn and a picker update.
	configJSON2 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_3":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"", "headers":[{"name":"header-1", "presentMatch":false, "invertMatch":true}], "action":"cds:cluster_3"},
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"path":"/z/y", "action":"cds:cluster_2"}
]
}`
	config2, err := rtParser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	wantAddrs = append(wantAddrs, resolver.Address{Addr: testBackendAddrStrs[2], Attributes: nil})
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
			hierarchy.Set(wantAddrs[2], []string{"cds:cluster_3"}),
		}},
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Expect exactly one new subconn.
	addrs := <-cc.NewSubConnAddrsCh
	if len(hierarchy.Get(addrs[0])) != 0 {
		t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
	}
	sc := <-cc.NewSubConnCh
	// Clear the attributes before adding to map.
	addrs[0].Attributes = nil
	m1[addrs[0]] = sc
	rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Should have no more newSubConn.
	select {
	case <-time.After(time.Millisecond * 500):
	case <-cc.NewSubConnCh:
		addrs := <-cc.NewSubConnAddrsCh
		t.Fatalf("unexpected NewSubConn with address %v", addrs)
	}

	p2 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m1[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/a/z",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("header-1", "value-1")),
			},
			wantSC:  m1[wantAddrs[2]],
			wantErr: nil,
		},
		{
			pickInfo: balancer.PickInfo{
				FullMethodName: "/c/d",
				Ctx:            metadata.NewOutgoingContext(context.Background(), metadata.Pairs("h", "v")),
			},
			wantSC:  nil,
			wantErr: errNoMatchedRouteFound,
		},
	} {
		testPick(t, p2, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestRoutingConfigUpdateDeleteAll covers the cases the routing balancer receives config
// update with no routes. Pick should fail with details in error.
func TestRoutingConfigUpdateDeleteAll(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"Action": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
},
"Route": [
	{"prefix":"/a/", "action":"cds:cluster_1"},
	{"path":"/z/y", "action":"cds:cluster_2"}
]
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], Attributes: nil},
		{Addr: testBackendAddrStrs[1], Attributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		if len(hierarchy.Get(addrs[0])) != 0 {
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].Attributes = nil
		m1[addrs[0]] = sc
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m1[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m1[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/c/d"},
			wantSC:   nil,
			wantErr:  errNoMatchedRouteFound,
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with no routes.
	configJSON2 := `{}`
	config2, err := rtParser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Expect two remove subconn.
	for range wantAddrs {
		select {
		case <-time.After(time.Millisecond * 500):
			t.Fatalf("timeout waiting for remove subconn")
		case <-cc.RemoveSubConnCh:
		}
	}

	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, err := p2.Pick(balancer.PickInfo{})
		if err != errNoMatchedRouteFound {
			t.Fatalf("picker.Pick, got %v, %v, want error %v", gotSCSt, err, errNoMatchedRouteFound)
		}
	}

	// Resend the previous config with routes and actions.
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
			hierarchy.Set(wantAddrs[1], []string{"cds:cluster_2"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m2 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address, and the hierarchy path
	// in the address is cleared.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		if len(hierarchy.Get(addrs[0])) != 0 {
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].Attributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].Attributes = nil
		m2[addrs[0]] = sc
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p3 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/0"},
			wantSC:   m2[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/a/1"},
			wantSC:   m2[wantAddrs[0]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/z/y"},
			wantSC:   m2[wantAddrs[1]],
			wantErr:  nil,
		},
		{
			pickInfo: balancer.PickInfo{FullMethodName: "/c/d"},
			wantSC:   nil,
			wantErr:  errNoMatchedRouteFound,
		},
	} {
		testPick(t, p3, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}
