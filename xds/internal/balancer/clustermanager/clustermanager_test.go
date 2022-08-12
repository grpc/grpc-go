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

package clustermanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/balancergroup"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

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
		a.BalancerAttributes = nil
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
	rtBuilder = balancer.Get(balancerName)
	rtParser = rtBuilder.(balancer.ConfigParser)

	balancer.Register(&ignoreAttrsRRBuilder{balancer.Get(roundrobin.Name)})
	balancer.Register(wrappedPickFirstBalancerBuilder{})

	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

func testPick(t *testing.T, p balancer.Picker, info balancer.PickInfo, wantSC balancer.SubConn, wantErr error) {
	t.Helper()
	for i := 0; i < 5; i++ {
		gotSCSt, err := p.Pick(info)
		if fmt.Sprint(err) != fmt.Sprint(wantErr) {
			t.Fatalf("picker.Pick(%+v), got error %v, want %v", info, err, wantErr)
		}
		if !cmp.Equal(gotSCSt.SubConn, wantSC, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick(%+v), got %v, want SubConn=%v", info, gotSCSt, wantSC)
		}
	}
}

func TestClusterPicks(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
}
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
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
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].BalancerAttributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
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
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestConfigUpdateAddCluster covers the cases the balancer receives config
// update with extra clusters.
func TestConfigUpdateAddCluster(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
}
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
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
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].BalancerAttributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
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
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with different routes, and different actions. Expect a
	// new subconn and a picker update.
	configJSON2 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_3":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
}
}`
	config2, err := rtParser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	wantAddrs = append(wantAddrs, resolver.Address{Addr: testBackendAddrStrs[2], BalancerAttributes: nil})
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
		t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].BalancerAttributes)
	}
	sc := <-cc.NewSubConnCh
	// Clear the attributes before adding to map.
	addrs[0].BalancerAttributes = nil
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
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_3"),
			},
			wantSC: m1[wantAddrs[2]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p2, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestRoutingConfigUpdateDeleteAll covers the cases the balancer receives
// config update with no clusters. Pick should fail with details in error.
func TestRoutingConfigUpdateDeleteAll(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"ignore_attrs_round_robin":""}] },
	"cds:cluster_2":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
}
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
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
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].BalancerAttributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
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
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with no clusters.
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

	// Expect two removed subconns.
	for range wantAddrs {
		select {
		case <-time.After(time.Millisecond * 500):
			t.Fatalf("timeout waiting for remove subconn")
		case <-cc.RemoveSubConnCh:
		}
	}

	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, err := p2.Pick(balancer.PickInfo{Ctx: SetPickedCluster(context.Background(), "cds:notacluster")})
		if fmt.Sprint(err) != status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`).Error() {
			t.Fatalf("picker.Pick, got %v, %v, want error %v", gotSCSt, err, `unknown cluster selected for RPC: "cds:notacluster"`)
		}
	}

	// Resend the previous config with clusters
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
			t.Fatalf("NewSubConn with address %+v, attrs %+v, want address with hierarchy cleared", addrs[0], addrs[0].BalancerAttributes)
		}
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
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
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_1"),
			},
			wantSC: m2[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:cluster_2"),
			},
			wantSC: m2[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(context.Background(), "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p3, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

func TestClusterManagerForwardsBalancerBuildOptions(t *testing.T) {
	const (
		balancerName       = "stubBalancer-TestClusterManagerForwardsBalancerBuildOptions"
		userAgent          = "ua"
		defaultTestTimeout = 1 * time.Second
	)

	// Setup the stub balancer such that we can read the build options passed to
	// it in the UpdateClientConnState method.
	ccsCh := testutils.NewChannel()
	bOpts := balancer.BuildOptions{
		DialCreds:        insecure.NewCredentials(),
		ChannelzParentID: channelz.NewIdentifierForTesting(channelz.RefChannel, 1234, nil),
		CustomUserAgent:  userAgent,
	}
	stub.Register(balancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			if !cmp.Equal(bd.BuildOptions, bOpts) {
				err := fmt.Errorf("buildOptions in child balancer: %v, want %v", bd, bOpts)
				ccsCh.Send(err)
				return err
			}
			ccsCh.Send(nil)
			return nil
		},
	})

	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, bOpts)

	configJSON1 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"%s":""}] }
}
}`, balancerName)
	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	if err := rtb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: config1}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	v, err := ccsCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timed out waiting for UpdateClientConnState result: %v", err)
	}
	if v != nil {
		t.Fatal(v)
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
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"test-init-Idle-balancer":""}] }
}
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
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
		rtb.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	}

	if state1 := <-cc.NewStateCh; state1 != connectivity.Idle {
		t.Fatalf("Received aggregated state: %v, want Idle", state1)
	}
}

// TestClusterGracefulSwitch tests the graceful switch functionality for a child
// of the cluster manager. At first, the child is configured as a round robin
// load balancer, and thus should behave accordingly. The test then gracefully
// switches this child to a pick first load balancer. Once that balancer updates
// it's state and completes the graceful switch process the new picker should
// reflect this change.
func TestClusterGracefulSwitch(t *testing.T) {
	cc := testutils.NewTestClientConn(t)
	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"csp:cluster":{ "childPolicy": [{"ignore_attrs_round_robin":""}] }
}
}`
	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"csp:cluster"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	sc1 := <-cc.NewSubConnCh
	rtb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	rtb.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p1 := <-cc.NewPickerCh
	pi := balancer.PickInfo{
		Ctx: SetPickedCluster(context.Background(), "csp:cluster"),
	}
	testPick(t, p1, pi, sc1, nil)

	// Same cluster, different balancer type.
	configJSON2 := `{
"children": {
	"csp:cluster":{ "childPolicy": [{"wrappedPickFirstBalancer":""}] }
}
}`
	config2, err := rtParser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[1], []string{"csp:cluster"}),
		}},
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}
	sc2 := <-cc.NewSubConnCh
	// Update the pick first balancers SubConn as CONNECTING. This will cause
	// the pick first balancer to UpdateState() with CONNECTING, which shouldn't send
	// a Picker update back, as the Graceful Switch process is not complete.
	rtb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-cc.NewPickerCh:
		t.Fatalf("No new picker should have been sent due to the Graceful Switch process not completing")
	case <-ctx.Done():
	}

	// Update the pick first balancers SubConn as READY. This will cause
	// the pick first balancer to UpdateState() with READY, which should send a
	// Picker update back, as the Graceful Switch process is complete. This
	// Picker should always pick the pick first's created SubConn.
	rtb.UpdateSubConnState(sc2, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p2 := <-cc.NewPickerCh
	testPick(t, p2, pi, sc2, nil)
	// The Graceful Switch process completing for the child should cause the
	// SubConns for the balancer being gracefully switched from to get deleted.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("error waiting for RemoveSubConn()")
	case rsc := <-cc.RemoveSubConnCh:
		// The SubConn removed should have been the created SubConn
		// from the child before switching.
		if rsc != sc1 {
			t.Fatalf("RemoveSubConn() got: %v, want %v", rsc, sc1)
		}
	}
}

type wrappedPickFirstBalancerBuilder struct{}

func (wrappedPickFirstBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	builder := balancer.Get(grpc.PickFirstBalancerName)
	wpfb := &wrappedPickFirstBalancer{
		ClientConn: cc,
	}
	pf := builder.Build(wpfb, opts)
	wpfb.Balancer = pf
	return wpfb
}

func (wrappedPickFirstBalancerBuilder) Name() string {
	return "wrappedPickFirstBalancer"
}

type wrappedPickFirstBalancer struct {
	balancer.Balancer
	balancer.ClientConn
}

func (wb *wrappedPickFirstBalancer) UpdateState(state balancer.State) {
	// Eat it if IDLE - allows it to switch over only on a READY SubConn.
	if state.ConnectivityState == connectivity.Idle {
		return
	}
	wb.ClientConn.UpdateState(state)
}

// tcc wraps a testutils.TestClientConn but stores all state transitions in a
// slice.
type tcc struct {
	*testutils.TestClientConn
	states []balancer.State
}

func (t *tcc) UpdateState(bs balancer.State) {
	t.states = append(t.states, bs)
	t.TestClientConn.UpdateState(bs)
}

func (s) TestUpdateStatePauses(t *testing.T) {
	cc := &tcc{TestClientConn: testutils.NewTestClientConn(t)}

	balFuncs := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, s balancer.ClientConnState) error {
			bd.ClientConn.UpdateState(balancer.State{ConnectivityState: connectivity.TransientFailure, Picker: nil})
			bd.ClientConn.UpdateState(balancer.State{ConnectivityState: connectivity.Ready, Picker: nil})
			return nil
		},
	}
	stub.Register("update_state_balancer", balFuncs)

	rtb := rtBuilder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"update_state_balancer":""}] }
}
}`

	config1, err := rtParser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an address with hierarchy path ["cluster_1"].
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
	}
	if err := rtb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			hierarchy.Set(wantAddrs[0], []string{"cds:cluster_1"}),
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Verify that the only state update is the second one called by the child.
	if len(cc.states) != 1 || cc.states[0].ConnectivityState != connectivity.Ready {
		t.Fatalf("cc.states = %v; want [connectivity.Ready]", cc.states)
	}
}
