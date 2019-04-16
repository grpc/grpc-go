// +build go1.12

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
 *
 */

package xds

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/balancer"
	discoverypb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/discovery"
	edspb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/eds"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
)

var lbABuilder *balancerABuilder

func init() {
	lbABuilder = &balancerABuilder{}
	balancer.Register(lbABuilder)
	balancer.Register(&balancerBBuilder{})
}

type s struct{}

func (s) Teardown(t *testing.T) {
	leakcheck.Check(t)
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type lbPolicy string

const (
	fakeBalancerA lbPolicy = "fake_balancer_A"
	fakeBalancerB lbPolicy = "fake_balancer_B"
	fakeBalancerC lbPolicy = "fake_balancer_C"
)

var (
	testBalancerNameFooBar      = "foo.bar"
	testBalancerConfigFooBar, _ = json.Marshal(&testBalancerConfig{
		BalancerName:   testBalancerNameFooBar,
		ChildPolicy:    []lbPolicy{fakeBalancerA},
		FallbackPolicy: []lbPolicy{fakeBalancerA},
	})
	specialAddrForBalancerA = resolver.Address{Addr: "this.is.balancer.A"}
	specialAddrForBalancerB = resolver.Address{Addr: "this.is.balancer.B"}

	// mu protects the access of latestFakeEdsBalancer
	mu                    sync.Mutex
	latestFakeEdsBalancer *fakeEDSBalancer
)

type testBalancerConfig struct {
	BalancerName   string     `json:"balancerName,omitempty"`
	ChildPolicy    []lbPolicy `json:"childPolicy,omitempty"`
	FallbackPolicy []lbPolicy `json:"fallbackPolicy,omitempty"`
}

func (l *lbPolicy) UnmarshalJSON(b []byte) error {
	// no need to implement, not used.
	return nil
}

func (l lbPolicy) MarshalJSON() ([]byte, error) {
	m := make(map[string]struct{})
	m[string(l)] = struct{}{}
	return json.Marshal(m)
}

type balancerABuilder struct {
	mu           sync.Mutex
	lastBalancer *balancerA
}

func (b *balancerABuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b.mu.Lock()
	b.lastBalancer = &balancerA{cc: cc, subconnStateChange: make(chan *scStateChange, 10)}
	b.mu.Unlock()
	return b.lastBalancer
}

func (b *balancerABuilder) Name() string {
	return string(fakeBalancerA)
}

func (b *balancerABuilder) getLastBalancer() *balancerA {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastBalancer
}

func (b *balancerABuilder) clearLastBalancer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastBalancer = nil
}

type balancerBBuilder struct{}

func (b *balancerBBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &balancerB{cc: cc}
}

func (*balancerBBuilder) Name() string {
	return string(fakeBalancerB)
}

type balancerA struct {
	cc                 balancer.ClientConn
	subconnStateChange chan *scStateChange
}

func (b *balancerA) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	b.subconnStateChange <- &scStateChange{sc: sc, state: state}
}

func (b *balancerA) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	_, _ = b.cc.NewSubConn(append(addrs, specialAddrForBalancerA), balancer.NewSubConnOptions{})
}

func (b *balancerA) Close() {}

type balancerB struct {
	cc balancer.ClientConn
}

func (balancerB) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	panic("implement me")
}

func (b *balancerB) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	_, _ = b.cc.NewSubConn(append(addrs, specialAddrForBalancerB), balancer.NewSubConnOptions{})
}

func (balancerB) Close() {}

func newTestClientConn() *testClientConn {
	return &testClientConn{
		newSubConns: make(chan []resolver.Address, 10),
	}
}

type testClientConn struct {
	newSubConns chan []resolver.Address
}

func (t *testClientConn) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	t.newSubConns <- addrs
	return nil, nil
}

func (testClientConn) RemoveSubConn(balancer.SubConn) {
}

func (testClientConn) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
}

func (testClientConn) ResolveNow(resolver.ResolveNowOption) {}

func (testClientConn) Target() string {
	return testServiceName
}

type scStateChange struct {
	sc    balancer.SubConn
	state connectivity.State
}

type fakeEDSBalancer struct {
	cc                 balancer.ClientConn
	edsChan            chan *edspb.ClusterLoadAssignment
	childPolicy        chan *loadBalancingConfig
	fallbackPolicy     chan *loadBalancingConfig
	subconnStateChange chan *scStateChange
}

func (f *fakeEDSBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	f.subconnStateChange <- &scStateChange{sc: sc, state: state}
}

func (f *fakeEDSBalancer) Close() {
	mu.Lock()
	defer mu.Unlock()
	latestFakeEdsBalancer = nil
}

func (f *fakeEDSBalancer) HandleEDSResponse(edsResp *edspb.ClusterLoadAssignment) {
	f.edsChan <- edsResp
}

func (f *fakeEDSBalancer) HandleChildPolicy(name string, config json.RawMessage) {
	f.childPolicy <- &loadBalancingConfig{
		Name:   name,
		Config: config,
	}
}

func newFakeEDSBalancer(cc balancer.ClientConn) edsBalancerInterface {
	lb := &fakeEDSBalancer{
		cc:                 cc,
		edsChan:            make(chan *edspb.ClusterLoadAssignment, 10),
		childPolicy:        make(chan *loadBalancingConfig, 10),
		fallbackPolicy:     make(chan *loadBalancingConfig, 10),
		subconnStateChange: make(chan *scStateChange, 10),
	}
	mu.Lock()
	latestFakeEdsBalancer = lb
	mu.Unlock()
	return lb
}

func getLatestEdsBalancer() *fakeEDSBalancer {
	mu.Lock()
	defer mu.Unlock()
	return latestFakeEdsBalancer
}

type fakeSubConn struct{}

func (*fakeSubConn) UpdateAddresses([]resolver.Address) {
	panic("implement me")
}

func (*fakeSubConn) Connect() {
	panic("implement me")
}

func (s) TestXdsBalanceHandleResolvedAddrs(t *testing.T) {
	startupTimeout = 500 * time.Millisecond
	defer func() { startupTimeout = defaultTimeout }()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()
	if err := lb.HandleBalancerConfig(json.RawMessage(testBalancerConfigFooBar)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(testBalancerConfigFooBar), err)
	}
	addrs := []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, {Addr: "3.3.3.3:10003"}}
	for i := 0; i < 3; i++ {
		lb.HandleResolvedAddrs(addrs, nil)
		select {
		case nsc := <-cc.newSubConns:
			if !reflect.DeepEqual(append(addrs, specialAddrForBalancerA), nsc) {
				t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerA))
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout when geting new subconn result")
		}
		addrs = addrs[:2-i]
	}
}

func (s) TestXdsBalanceHandleBalancerConfigBalancerNameUpdate(t *testing.T) {
	startupTimeout = 500 * time.Millisecond
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		startupTimeout = defaultTimeout
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()
	if err := lb.HandleBalancerConfig(json.RawMessage(testBalancerConfigFooBar)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(testBalancerConfigFooBar), err)
	}
	addrs := []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, {Addr: "3.3.3.3:10003"}}
	lb.HandleResolvedAddrs(addrs, nil)

	// verify fallback takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerA), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerA))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout when geting new subconn result")
	}

	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()
	// In the first iteration, an eds balancer takes over fallback balancer
	// In the second iteration, a new xds client takes over previous one.
	for i := 0; i < 2; i++ {
		addr, td, cleanup := setupServer(t)
		cleanups = append(cleanups, cleanup)
		workingBalancerConfig, _ := json.Marshal(&testBalancerConfig{
			BalancerName:   addr,
			ChildPolicy:    []lbPolicy{fakeBalancerA},
			FallbackPolicy: []lbPolicy{fakeBalancerA},
		})

		if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
			t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
		}
		td.sendResp(&response{resp: testEDSRespWithoutEndpoints})

		var j int
		for j = 0; j < 10; j++ {
			if edsLB := getLatestEdsBalancer(); edsLB != nil { // edsLB won't change between the two iterations
				select {
				case gotEDS := <-edsLB.edsChan:
					if !proto.Equal(gotEDS, testClusterLoadAssignmentWithoutEndpoints) {
						t.Fatalf("edsBalancer got eds: %v, want %v", gotEDS, testClusterLoadAssignmentWithoutEndpoints)
					}
				case <-time.After(time.Second):
					t.Fatal("haven't got EDS update after 1s")
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if j == 10 {
			t.Fatal("edsBalancer instance has not been created or updated after 1s")
		}
	}
}

// switch child policy, lb stays the same
// cds->eds or eds -> cds, restart xdsClient, lb stays the same
func (s) TestXdsBalanceHandleBalancerConfigChildPolicyUpdate(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()

	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()
	for _, test := range []struct {
		cfg                 *testBalancerConfig
		responseToSend      *discoverypb.DiscoveryResponse
		expectedChildPolicy *loadBalancingConfig
	}{
		{
			cfg: &testBalancerConfig{
				ChildPolicy: []lbPolicy{fakeBalancerA},
			},
			responseToSend: testEDSRespWithoutEndpoints,
			expectedChildPolicy: &loadBalancingConfig{
				Name:   string(fakeBalancerA),
				Config: json.RawMessage(`{}`),
			},
		},
		{
			cfg: &testBalancerConfig{
				ChildPolicy: []lbPolicy{fakeBalancerB},
			},
			expectedChildPolicy: &loadBalancingConfig{
				Name:   string(fakeBalancerB),
				Config: json.RawMessage(`{}`),
			},
		},
		{
			cfg:            &testBalancerConfig{},
			responseToSend: testCDSResp,
			expectedChildPolicy: &loadBalancingConfig{
				Name: "ROUND_ROBIN",
			},
		},
	} {
		addr, td, cleanup := setupServer(t)
		cleanups = append(cleanups, cleanup)
		test.cfg.BalancerName = addr
		workingBalancerConfig, _ := json.Marshal(test.cfg)

		if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
			t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
		}
		if test.responseToSend != nil {
			td.sendResp(&response{resp: test.responseToSend})
		}
		var i int
		for i = 0; i < 10; i++ {
			if edsLB := getLatestEdsBalancer(); edsLB != nil {
				select {
				case childPolicy := <-edsLB.childPolicy:
					if !reflect.DeepEqual(childPolicy, test.expectedChildPolicy) {
						t.Fatalf("got childPolicy %v, want %v", childPolicy, test.expectedChildPolicy)
					}
				case <-time.After(time.Second):
					t.Fatal("haven't got policy update after 1s")
				}
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if i == 10 {
			t.Fatal("edsBalancer instance has not been created or updated after 1s")
		}
	}
}

// not in fallback mode, overwrite fallback info.
// in fallback mode, update config or switch balancer.
func (s) TestXdsBalanceHandleBalancerConfigFallbackUpdate(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()

	addr, td, cleanup := setupServer(t)

	cfg := &testBalancerConfig{
		BalancerName:   addr,
		ChildPolicy:    []lbPolicy{fakeBalancerA},
		FallbackPolicy: []lbPolicy{fakeBalancerA},
	}
	workingBalancerConfig, _ := json.Marshal(cfg)

	if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
	}

	cfg.FallbackPolicy = []lbPolicy{fakeBalancerB}
	workingBalancerConfig, _ = json.Marshal(cfg)

	if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
	}

	td.sendResp(&response{resp: testEDSRespWithoutEndpoints})

	var i int
	for i = 0; i < 10; i++ {
		if edsLB := getLatestEdsBalancer(); edsLB != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("edsBalancer instance has not been created and assigned to lb.xdsLB after 1s")
	}

	cleanup()

	addrs := []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, {Addr: "3.3.3.3:10003"}}
	lb.HandleResolvedAddrs(addrs, nil)

	// verify fallback balancer B takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerB), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerB))
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout when geting new subconn result")
	}

	cfg.FallbackPolicy = []lbPolicy{fakeBalancerA}
	workingBalancerConfig, _ = json.Marshal(cfg)
	if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
	}

	// verify fallback balancer A takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerA), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerA))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout when geting new subconn result")
	}
}

func (s) TestXdsBalancerHandlerSubConnStateChange(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()

	addr, td, cleanup := setupServer(t)
	defer cleanup()
	cfg := &testBalancerConfig{
		BalancerName:   addr,
		ChildPolicy:    []lbPolicy{fakeBalancerA},
		FallbackPolicy: []lbPolicy{fakeBalancerA},
	}
	workingBalancerConfig, _ := json.Marshal(cfg)

	if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
	}

	td.sendResp(&response{resp: testEDSRespWithoutEndpoints})

	expectedScStateChange := &scStateChange{
		sc:    &fakeSubConn{},
		state: connectivity.Ready,
	}

	var i int
	for i = 0; i < 10; i++ {
		if edsLB := getLatestEdsBalancer(); edsLB != nil {
			lb.HandleSubConnStateChange(expectedScStateChange.sc, expectedScStateChange.state)
			select {
			case scsc := <-edsLB.subconnStateChange:
				if !reflect.DeepEqual(scsc, expectedScStateChange) {
					t.Fatalf("got subconn state change %v, want %v", scsc, expectedScStateChange)
				}
			case <-time.After(time.Second):
				t.Fatal("haven't got subconn state change after 1s")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("edsBalancer instance has not been created and assigned to lb.xdsLB after 1s")
	}

	// lbAbuilder has a per binary record what's the last balanceA created. We need to clear the record
	// to make sure there's a new one created and get the pointer to it.
	lbABuilder.clearLastBalancer()
	cleanup()

	// switch to fallback
	// fallback balancer A takes over
	for i = 0; i < 10; i++ {
		if fblb := lbABuilder.getLastBalancer(); fblb != nil {
			lb.HandleSubConnStateChange(expectedScStateChange.sc, expectedScStateChange.state)
			select {
			case scsc := <-fblb.subconnStateChange:
				if !reflect.DeepEqual(scsc, expectedScStateChange) {
					t.Fatalf("got subconn state change %v, want %v", scsc, expectedScStateChange)
				}
			case <-time.After(time.Second):
				t.Fatal("haven't got subconn state change after 1s")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("balancerA instance has not been created after 1s")
	}
}

func (s) TestXdsBalancerFallbackSignalFromEdsBalancer(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get("xds")
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{}).(*xdsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *xdsBalancer")
	}
	defer lb.Close()

	addr, td, cleanup := setupServer(t)
	defer cleanup()
	cfg := &testBalancerConfig{
		BalancerName:   addr,
		ChildPolicy:    []lbPolicy{fakeBalancerA},
		FallbackPolicy: []lbPolicy{fakeBalancerA},
	}
	workingBalancerConfig, _ := json.Marshal(cfg)

	if err := lb.HandleBalancerConfig(json.RawMessage(workingBalancerConfig)); err != nil {
		t.Fatalf("failed to HandleBalancerConfig(%v), due to err: %v", string(workingBalancerConfig), err)
	}

	td.sendResp(&response{resp: testEDSRespWithoutEndpoints})

	expectedScStateChange := &scStateChange{
		sc:    &fakeSubConn{},
		state: connectivity.Ready,
	}

	var i int
	for i = 0; i < 10; i++ {
		if edsLB := getLatestEdsBalancer(); edsLB != nil {
			lb.HandleSubConnStateChange(expectedScStateChange.sc, expectedScStateChange.state)
			select {
			case scsc := <-edsLB.subconnStateChange:
				if !reflect.DeepEqual(scsc, expectedScStateChange) {
					t.Fatalf("got subconn state change %v, want %v", scsc, expectedScStateChange)
				}
			case <-time.After(time.Second):
				t.Fatal("haven't got subconn state change after 1s")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("edsBalancer instance has not been created and assigned to lb.xdsLB after 1s")
	}

	// lbAbuilder has a per binary record what's the last balanceA created. We need to clear the record
	// to make sure there's a new one created and get the pointer to it.
	lbABuilder.clearLastBalancer()
	cleanup()

	// switch to fallback
	// fallback balancer A takes over
	for i = 0; i < 10; i++ {
		if fblb := lbABuilder.getLastBalancer(); fblb != nil {
			lb.HandleSubConnStateChange(expectedScStateChange.sc, expectedScStateChange.state)
			select {
			case scsc := <-fblb.subconnStateChange:
				if !reflect.DeepEqual(scsc, expectedScStateChange) {
					t.Fatalf("got subconn state change %v, want %v", scsc, expectedScStateChange)
				}
			case <-time.After(time.Second):
				t.Fatal("haven't got subconn state change after 1s")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if i == 10 {
		t.Fatal("balancerA instance has not been created after 1s")
	}
}

func (s) TestXdsBalancerConfigParsingSelectingLBPolicy(t *testing.T) {
	tesCfg := &testBalancerConfig{
		BalancerName:   "fake.foo.bar",
		ChildPolicy:    []lbPolicy{fakeBalancerC, fakeBalancerA, fakeBalancerB}, // selects fakeBalancerA
		FallbackPolicy: []lbPolicy{fakeBalancerC, fakeBalancerB, fakeBalancerA}, // selects fakeBalancerB
	}
	js, _ := json.Marshal(tesCfg)
	var xdsCfg xdsConfig
	if err := json.Unmarshal(js, &xdsCfg); err != nil {
		t.Fatal("unable to unmarshal balancer config into xds config")
	}
	wantChildPolicy := &loadBalancingConfig{Name: string(fakeBalancerA), Config: json.RawMessage(`{}`)}
	if !reflect.DeepEqual(xdsCfg.ChildPolicy, wantChildPolicy) {
		t.Fatalf("got child policy %v, want %v", xdsCfg.ChildPolicy, wantChildPolicy)
	}
	wantFallbackPolicy := &loadBalancingConfig{Name: string(fakeBalancerB), Config: json.RawMessage(`{}`)}
	if !reflect.DeepEqual(xdsCfg.FallBackPolicy, wantFallbackPolicy) {
		t.Fatalf("got fallback policy %v, want %v", xdsCfg.FallBackPolicy, wantFallbackPolicy)
	}
}
