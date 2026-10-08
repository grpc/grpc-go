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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
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
	testBackendAddrsCount   = 12
	testChildPolicyName     = "cluster_manager_test_child"
)

var testBackendAddrStrs []string

// testChildConfig is the LB config of the test child policy.
type testChildConfig struct {
	serviceconfig.LoadBalancingConfig
	Backend string `json:"backend"`
}

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}

	// The test child policy delegates to pick_first with the single backend
	// specified in its LB config, and ignores the endpoints received from the
	// cluster_manager. This mirrors the real children of the cluster_manager,
	// which get their endpoints from the xDS config instead of the resolver
	// state.
	stub.Register(testChildPolicyName, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = balancer.Get(pickfirst.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		ParseConfig: func(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			cfg := &testChildConfig{}
			if err := json.Unmarshal(c, cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			cfg, ok := ccs.BalancerConfig.(*testChildConfig)
			if !ok {
				return fmt.Errorf("unexpected balancer config with type: %T", ccs.BalancerConfig)
			}
			return bd.ChildBalancer.UpdateClientConnState(balancer.ClientConnState{
				ResolverState: resolver.State{
					Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: cfg.Backend}}}},
				},
			})
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.ChildBalancer.ExitIdle()
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	})
}

// testChildPolicyJSON returns the JSON config of a test child policy that
// connects to the given backend.
func testChildPolicyJSON(backend string) string {
	return fmt.Sprintf(`{ "childPolicy": [{%q: {"backend": %q}}] }`, testChildPolicyName, backend)
}

func testPick(t *testing.T, p balancer.Picker, info balancer.PickInfo, wantSC balancer.SubConn, wantErr error) {
	t.Helper()
	for i := 0; i < 5; i++ {
		gotSCSt, err := p.Pick(info)
		if fmt.Sprint(err) != fmt.Sprint(wantErr) {
			t.Fatalf("picker.Pick(%+v), got error %v, want %v", info, err, wantErr)
		}
		if gotSCSt.SubConn != wantSC {
			t.Fatalf("picker.Pick(%+v), got %v, want SubConn=%v", info, gotSCSt, wantSC)
		}
	}
}

func (s) TestClusterPicks(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})

	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
	}
	configJSON1 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1": %s,
	"cds:cluster_2": %s
}
}`, testChildPolicyJSON(wantAddrs[0].Addr), testChildPolicyJSON(wantAddrs[1].Addr))
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config. Each child connects to the backend in its config.
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
		m1[addrs[0]] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	p1 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestConfigUpdateAddCluster covers the cases the balancer receives config
// update with extra clusters.
func (s) TestConfigUpdateAddCluster(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})

	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
	}
	configJSON1 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1": %s,
	"cds:cluster_2": %s
}
}`, testChildPolicyJSON(wantAddrs[0].Addr), testChildPolicyJSON(wantAddrs[1].Addr))
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config. Each child connects to the backend in its config.
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
		m1[addrs[0]] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with different routes, and different actions. Expect a
	// new subconn and a picker update.
	wantAddrs = append(wantAddrs, resolver.Address{Addr: testBackendAddrStrs[2], BalancerAttributes: nil})
	configJSON2 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1": %s,
	"cds:cluster_2": %s,
	"cds:cluster_3": %s
}
}`, testChildPolicyJSON(wantAddrs[0].Addr), testChildPolicyJSON(wantAddrs[1].Addr), testChildPolicyJSON(wantAddrs[2].Addr))
	config2, err := parser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Expect exactly one new subconn.
	addrs := <-cc.NewSubConnAddrsCh
	sc := <-cc.NewSubConnCh
	// Clear the attributes before adding to map.
	addrs[0].BalancerAttributes = nil
	m1[addrs[0]] = sc
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

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
				Ctx: SetPickedCluster(ctx, "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_3"),
			},
			wantSC: m1[wantAddrs[2]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p2, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

// TestRoutingConfigUpdateDeleteAll covers the cases the balancer receives
// config update with no clusters. Pick should fail with details in error.
func (s) TestRoutingConfigUpdateDeleteAll(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})

	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
		{Addr: testBackendAddrStrs[1], BalancerAttributes: nil},
	}
	configJSON1 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1": %s,
	"cds:cluster_2": %s
}
}`, testChildPolicyJSON(wantAddrs[0].Addr), testChildPolicyJSON(wantAddrs[1].Addr))
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config. Each child connects to the backend in its config.
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m1 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
		m1[addrs[0]] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_1"),
			},
			wantSC: m1[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_2"),
			},
			wantSC: m1[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p1, tt.pickInfo, tt.wantSC, tt.wantErr)
	}

	// A config update with no clusters.
	configJSON2 := `{}`
	config2, err := parser.ParseConfig([]byte(configJSON2))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config2,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Expect two removed subconns.
	for range wantAddrs {
		select {
		case <-time.After(time.Millisecond * 500):
			t.Fatalf("timeout waiting for remove subconn")
		case <-cc.ShutdownSubConnCh:
		}
	}

	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, err := p2.Pick(balancer.PickInfo{Ctx: SetPickedCluster(ctx, "cds:notacluster")})
		if fmt.Sprint(err) != status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`).Error() {
			t.Fatalf("picker.Pick, got %v, %v, want error %v", gotSCSt, err, `unknown cluster selected for RPC: "cds:notacluster"`)
		}
	}

	// Resend the previous config with clusters
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	m2 := make(map[resolver.Address]balancer.SubConn)
	// Verify that a subconn is created with the address.
	for range wantAddrs {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		// Clear the attributes before adding to map.
		addrs[0].BalancerAttributes = nil
		m2[addrs[0]] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p3 := <-cc.NewPickerCh
	for _, tt := range []struct {
		pickInfo balancer.PickInfo
		wantSC   balancer.SubConn
		wantErr  error
	}{
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_1"),
			},
			wantSC: m2[wantAddrs[0]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:cluster_2"),
			},
			wantSC: m2[wantAddrs[1]],
		},
		{
			pickInfo: balancer.PickInfo{
				Ctx: SetPickedCluster(ctx, "cds:notacluster"),
			},
			wantErr: status.Errorf(codes.Unavailable, `unknown cluster selected for RPC: "cds:notacluster"`),
		},
	} {
		testPick(t, p3, tt.pickInfo, tt.wantSC, tt.wantErr)
	}
}

func (s) TestClusterManagerForwardsBalancerBuildOptions(t *testing.T) {
	const (
		userAgent          = "ua"
		defaultTestTimeout = 1 * time.Second
	)

	// Setup the stub balancer such that we can read the build options passed to
	// it in the UpdateClientConnState method.
	ccsCh := testutils.NewChannel()
	bOpts := balancer.BuildOptions{
		DialCreds:       insecure.NewCredentials(),
		CustomUserAgent: userAgent,
	}
	stub.Register(t.Name(), stub.BalancerFuncs{
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

	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, bOpts)

	configJSON1 := fmt.Sprintf(`{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"%s":""}] }
}
}`, t.Name())
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	if err := bal.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: config1}); err != nil {
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

// TestClusterManagerForwardsResolverState verifies that the cluster_manager
// forwards the resolver state it receives, unchanged, to all of its children.
func (s) TestClusterManagerForwardsResolverState(t *testing.T) {
	const numChildren = 2
	ccsCh := make(chan balancer.ClientConnState, numChildren)
	stub.Register(t.Name(), stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccsCh <- ccs
			return nil
		},
	})

	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})
	defer bal.Close()

	configJSON := fmt.Sprintf(`{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"%[1]s":""}] },
	"cds:cluster_2":{ "childPolicy": [{"%[1]s":""}] }
}
}`, t.Name())
	config, err := parser.ParseConfig([]byte(configJSON))
	if err != nil {
		t.Fatalf("Failed to parse balancer config: %v", err)
	}

	type attrKey struct{}
	wantState := resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}},
			{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[1]}}},
		},
		Attributes: attributes.New(attrKey{}, "value"),
	}
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  wantState,
		BalancerConfig: config,
	}); err != nil {
		t.Fatalf("Failed to update ClientConn state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < numChildren; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for child %d to receive an update", i)
		case ccs := <-ccsCh:
			if diff := cmp.Diff(wantState.Endpoints, ccs.ResolverState.Endpoints); diff != "" {
				t.Errorf("Child received unexpected endpoints (-want +got):\n%s", diff)
			}
			if !ccs.ResolverState.Attributes.Equal(wantState.Attributes) {
				t.Errorf("Child received attributes %v, want %v", ccs.ResolverState.Attributes, wantState.Attributes)
			}
		}
	}
}

const initIdleBalancerName = "test-init-idle-balancer"

var errTestInitIdle = fmt.Errorf("init Idle balancer error 0")

func init() {
	stub.Register(initIdleBalancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, opts balancer.ClientConnState) error {
			sc, err := bd.ClientConn.NewSubConn(opts.ResolverState.Addresses, balancer.NewSubConnOptions{
				StateListener: func(state balancer.SubConnState) {
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
			if err != nil {
				return err
			}
			sc.Connect()
			return nil
		},
	})
}

// TestInitialIdle covers the case that if the child reports Idle, the overall
// state will be Idle.
func (s) TestInitialIdle(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"test-init-idle-balancer":""}] }
}
}`
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an endpoint.
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
	}
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{wantAddrs[0]}},
		}},
		BalancerConfig: config1,
	}); err != nil {
		t.Fatalf("failed to update ClientConn state: %v", err)
	}

	// Verify that a subconn is created with the address.
	for range wantAddrs {
		sc := <-cc.NewSubConnCh
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	}

	if state1 := <-cc.NewStateCh; state1 != connectivity.Idle {
		t.Fatalf("Received aggregated state: %v, want Idle", state1)
	}
}

// TestClusterGracefulSwitch tests the graceful switch functionality for a child
// of the cluster_manager. The test performs the following steps:
//  1. Configures a single child for the cluster_manager with a round_robin LB
//     policy. Ensures the child sends a picker update when the subchannel moves
//     to READY.
//  2. Updates the exiting child with a different LB policy (pick_first), and a
//     different endpoint. This should trigger the graceful switch process, and
//     cluster_manager should not send a picker update until the child has
//     completed the graceful switch process.
//  3. The child should complete the graceful switch process once it receives an
//     update that moves its subchannel to READY. At this point, the cluster_manager
//     should send a picker update reflecting the new pick_first balancer's picker.
func (s) TestClusterGracefulSwitch(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})
	defer bal.Close()

	// Push round_robin config with one endpoint to the cluster_manager.
	rrConfigJSON := `
	{
		"children": {
			"csp:cluster":{ "childPolicy": [{"round_robin": {}}] }
		}
	}`
	rrConfig, err := parser.ParseConfig([]byte(rrConfigJSON))
	if err != nil {
		t.Fatalf("Failed to parse round_robin config: %v", err)
	}
	rrEndpoint := resolver.Endpoint{Addresses: []resolver.Address{{Addr: "rr-backend"}}}
	ccs := balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: []resolver.Endpoint{rrEndpoint}},
		BalancerConfig: rrConfig,
	}
	if err := bal.UpdateClientConnState(ccs); err != nil {
		t.Fatalf("Failed to send round_robin config update to cluster_manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Wait for a subchannel to be created.
	var sc1 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for subchannel to be created by the round_robin LB policy")
	case sc1 = <-cc.NewSubConnCh:
	}

	// Update the subchannel's state to READY, and ensure we get a picker update.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	var p1 balancer.Picker
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for picker update from cluster_manager LB policy")
	case p1 = <-cc.NewPickerCh:
	}

	// Verify the picker picks the expected subchannel.
	pi := balancer.PickInfo{Ctx: SetPickedCluster(ctx, "csp:cluster")}
	testPick(t, p1, pi, sc1, nil)

	// Update the config to use pick_first, and a different endpoint.
	pfConfigJSON := `
	{
		"children": {
			"csp:cluster":{ "childPolicy": [{"pick_first": {}}] }
		}
	}`
	pfConfig, err := parser.ParseConfig([]byte(pfConfigJSON))
	if err != nil {
		t.Fatalf("Failed to parse pick_first config: %v", err)
	}
	pfEndpoint := resolver.Endpoint{Addresses: []resolver.Address{{Addr: "pf-backend"}}}
	ccs = balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: []resolver.Endpoint{pfEndpoint}},
		BalancerConfig: pfConfig,
	}
	if err := bal.UpdateClientConnState(ccs); err != nil {
		t.Fatalf("Failed to send pick_first config update to cluster_manager: %v", err)
	}

	// Wait for a subchannel to be created.
	var sc2 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for subchannel to be created by the pick_first LB policy")
	case sc2 = <-cc.NewSubConnCh:
	}

	// Move the pick_first LB policy's subchannel to CONNECTING. This will cause
	// the pick_first LB policy to send a connecting picker. But this should not
	// result in the cluster_manager sending a picker update to the channel as
	// the graceful switch process is not yet complete.
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-cc.NewPickerCh:
		t.Fatalf("Received picker from cluster_manager when none expected during graceful switch process")
	case <-sCtx.Done():
	}

	// Move the pick_first LB policy's subchannel to READY. This will cause the
	// pick_first LB policy to send a ready picker and complete the graceful
	// switch process.
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	var p2 balancer.Picker
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for picker update from cluster_manager LB policy")
	case p2 = <-cc.NewPickerCh:
	}

	// Verify the picker picks the expected subchannel.
	testPick(t, p2, pi, sc2, nil)

	// Completion of the graceful switch process should result in the old
	// subchannel being removed.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for subchannels belonging to old child to be deleted")
	case rsc := <-cc.ShutdownSubConnCh:
		if rsc != sc1 {
			t.Fatalf("Unexpected subchannel %v being shutdown, want %v", rsc, sc1)
		}
	}
}

// tcc wraps a testutils.TestClientConn but stores all state transitions in a
// slice.
type tcc struct {
	*testutils.BalancerClientConn
	states []balancer.State
}

func (t *tcc) UpdateState(bs balancer.State) {
	t.states = append(t.states, bs)
	t.BalancerClientConn.UpdateState(bs)
}

func (s) TestUpdateStatePauses(t *testing.T) {
	cc := &tcc{BalancerClientConn: testutils.NewBalancerClientConn(t)}

	balFuncs := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			bd.ClientConn.UpdateState(balancer.State{ConnectivityState: connectivity.TransientFailure, Picker: nil})
			bd.ClientConn.UpdateState(balancer.State{ConnectivityState: connectivity.Ready, Picker: nil})
			return nil
		},
	}
	stub.Register("update_state_balancer", balFuncs)

	builder := balancer.Get(balancerName)
	parser := builder.(balancer.ConfigParser)
	bal := builder.Build(cc, balancer.BuildOptions{})
	defer bal.Close()

	configJSON1 := `{
"children": {
	"cds:cluster_1":{ "childPolicy": [{"update_state_balancer":""}] }
}
}`
	config1, err := parser.ParseConfig([]byte(configJSON1))
	if err != nil {
		t.Fatalf("failed to parse balancer config: %v", err)
	}

	// Send the config, and an endpoint.
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0], BalancerAttributes: nil},
	}
	if err := bal.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{wantAddrs[0]}},
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
