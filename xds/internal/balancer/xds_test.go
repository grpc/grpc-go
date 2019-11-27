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

package balancer

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/jsonpb"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	scpb "google.golang.org/grpc/internal/proto/grpc_service_config"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/client/fakexds"
)

var lbABuilder = &balancerABuilder{}

func init() {
	balancer.Register(&edsBalancerBuilder{})
	balancer.Register(lbABuilder)
	balancer.Register(&balancerBBuilder{})

	bootstrapConfigNew = func() *bootstrap.Config {
		return &bootstrap.Config{
			BalancerName: "",
			Creds:        grpc.WithInsecure(),
			NodeProto:    &corepb.Node{},
		}
	}
}

type s struct{}

func (s) Teardown(t *testing.T) {
	leakcheck.Check(t)
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	fakeBalancerA = "fake_balancer_A"
	fakeBalancerB = "fake_balancer_B"
)

var (
	testBalancerNameFooBar = "foo.bar"
	testLBConfigFooBar     = &XDSConfig{
		BalancerName:   testBalancerNameFooBar,
		ChildPolicy:    &loadBalancingConfig{Name: fakeBalancerB},
		FallBackPolicy: &loadBalancingConfig{Name: fakeBalancerA},
		EDSServiceName: testEDSClusterName,
	}

	specialAddrForBalancerA = resolver.Address{Addr: "this.is.balancer.A"}
	specialAddrForBalancerB = resolver.Address{Addr: "this.is.balancer.B"}

	// mu protects the access of latestFakeEdsBalancer
	mu                    sync.Mutex
	latestFakeEdsBalancer *fakeEDSBalancer
)

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

// A fake balancer implementation which does two things:
// * Appends a unique address to the list of resolved addresses received before
//   attempting to create a SubConn.
// * Makes the received subConn state changes available through a channel, for
//   the test to inspect.
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

// A fake balancer implementation which appends a unique address to the list of
// resolved addresses received before attempting to create a SubConn.
type balancerB struct {
	cc balancer.ClientConn
}

func (b *balancerB) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	_, _ = b.cc.NewSubConn(append(addrs, specialAddrForBalancerB), balancer.NewSubConnOptions{})
}

func (balancerB) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	panic("implement me")
}
func (balancerB) Close() {}

func newTestClientConn() *testClientConn {
	return &testClientConn{newSubConns: make(chan []resolver.Address, 10)}
}

type testClientConn struct {
	newSubConns chan []resolver.Address
}

func (t *testClientConn) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	t.newSubConns <- addrs
	return nil, nil
}

func (testClientConn) RemoveSubConn(balancer.SubConn)                          {}
func (testClientConn) UpdateBalancerState(connectivity.State, balancer.Picker) {}
func (testClientConn) UpdateState(balancer.State)                              {}
func (testClientConn) ResolveNow(resolver.ResolveNowOptions)                   {}
func (testClientConn) Target() string                                          { return testServiceName }

type scStateChange struct {
	sc    balancer.SubConn
	state connectivity.State
}

type fakeEDSBalancer struct {
	cc                 balancer.ClientConn
	edsChan            chan *xdsclient.EDSUpdate
	childPolicy        chan *loadBalancingConfig
	fallbackPolicy     chan *loadBalancingConfig
	subconnStateChange chan *scStateChange
	loadStore          lrs.Store
}

func (f *fakeEDSBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	f.subconnStateChange <- &scStateChange{sc: sc, state: state}
}

func (f *fakeEDSBalancer) Close() {
	mu.Lock()
	defer mu.Unlock()
	latestFakeEdsBalancer = nil
}

func (f *fakeEDSBalancer) HandleEDSResponse(edsResp *xdsclient.EDSUpdate) {
	f.edsChan <- edsResp
}

func (f *fakeEDSBalancer) HandleChildPolicy(name string, config json.RawMessage) {
	f.childPolicy <- &loadBalancingConfig{
		Name:   name,
		Config: config,
	}
}

func newFakeEDSBalancer(cc balancer.ClientConn, loadStore lrs.Store) edsBalancerInterface {
	lb := &fakeEDSBalancer{
		cc:                 cc,
		edsChan:            make(chan *xdsclient.EDSUpdate, 10),
		childPolicy:        make(chan *loadBalancingConfig, 10),
		fallbackPolicy:     make(chan *loadBalancingConfig, 10),
		subconnStateChange: make(chan *scStateChange, 10),
		loadStore:          loadStore,
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

func (*fakeSubConn) UpdateAddresses([]resolver.Address) { panic("implement me") }
func (*fakeSubConn) Connect()                           { panic("implement me") }

type fakeXDSClient struct {
	edsCbReceived chan struct{} // Will be closed when WatchEDS is called.
	edsCb         func(*xdsclient.EDSUpdate, error)
}

func newFakeXDSClient() *fakeXDSClient {
	return &fakeXDSClient{edsCbReceived: make(chan struct{})}
}

func (c *fakeXDSClient) WatchEDS(clusterName string, edsCb func(*xdsclient.EDSUpdate, error)) (cancel func()) {
	c.edsCb = edsCb
	// WatchEDS is expected to be only called once in the test. If a test needs
	// to call it multiple times, this will panic.
	close(c.edsCbReceived)
	return func() {}
}

func (c *fakeXDSClient) callEDSCallback(u *xdsclient.EDSUpdate, err error) {
	t := time.NewTimer(1 * time.Second)
	select {
	case <-c.edsCbReceived:
		t.Stop()
	case <-t.C:
		panic("EDS callback is not received after 1 second")
	}
	c.edsCb(u, err)
}

func (c *fakeXDSClient) ReportLoad(server string, clusterName string, loadStore lrs.Store) (cancel func()) {
	return func() {}
}

func (c *fakeXDSClient) Close() {}

// TestXdsFallbackResolvedAddrs verifies that the fallback balancer specified
// in the provided lbconfig is initialized, and that it receives the addresses
// pushed by the resolver.
//
// The test does the following:
// * Builds a new xds balancer.
// * Since there is no xDS server to respond to requests from the xds client
//   (created as part of the xds balancer), we expect the fallback policy to
//   kick in.
// * Repeatedly pushes new ClientConnState which specifies the same fallback
//   policy, but a different set of resolved addresses.
// * The fallback policy is implemented by a fake balancer, which appends a
//   unique address to the list of addresses it uses to create the SubConn.
// * We also have a fake ClientConn which verifies that it receives the
//   expected address list.
func (s) TestXdsFallbackResolvedAddrs(t *testing.T) {
	startupTimeout = 500 * time.Millisecond
	defer func() { startupTimeout = defaultTimeout }()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	b := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}})
	lb, ok := b.(*edsBalancer)
	if !ok {
		t.Fatalf("builder.Build() returned a balancer of type %T, want *edsBalancer", b)
	}
	defer lb.Close()

	tests := []struct {
		resolvedAddrs []resolver.Address
		wantAddrs     []resolver.Address
	}{
		{
			resolvedAddrs: []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}},
			wantAddrs:     []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, specialAddrForBalancerA},
		},
		{
			resolvedAddrs: []resolver.Address{{Addr: "1.1.1.1:10001"}},
			wantAddrs:     []resolver.Address{{Addr: "1.1.1.1:10001"}, specialAddrForBalancerA},
		},
	}
	for _, test := range tests {
		lb.UpdateClientConnState(balancer.ClientConnState{
			ResolverState:  resolver.State{Addresses: test.resolvedAddrs},
			BalancerConfig: testLBConfigFooBar,
		})

		select {
		case gotAddrs := <-cc.newSubConns:
			if !reflect.DeepEqual(gotAddrs, test.wantAddrs) {
				t.Fatalf("got new subconn address %v, want %v", gotAddrs, test.wantAddrs)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout when getting new subconn result")
		}
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

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()
	addrs := []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, {Addr: "3.3.3.3:10003"}}
	lb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Addresses: addrs},
		BalancerConfig: testLBConfigFooBar,
	})

	// verify fallback takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerA), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerA))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout when getting new subconn result")
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
		td, cleanup := fakexds.StartServer(t)
		cleanups = append(cleanups, cleanup)
		workingLBConfig := &XDSConfig{
			BalancerName:   td.Address,
			ChildPolicy:    &loadBalancingConfig{Name: fakeBalancerA},
			FallBackPolicy: &loadBalancingConfig{Name: fakeBalancerA},
			EDSServiceName: testEDSClusterName,
		}
		lb.UpdateClientConnState(balancer.ClientConnState{
			ResolverState:  resolver.State{Addresses: addrs},
			BalancerConfig: workingLBConfig,
		})
		td.ResponseChan <- &fakexds.Response{Resp: testEDSResp}

		var j int
		for j = 0; j < 10; j++ {
			if edsLB := getLatestEdsBalancer(); edsLB != nil { // edsLB won't change between the two iterations
				select {
				case gotEDS := <-edsLB.edsChan:
					want, err := xdsclient.ParseEDSRespProto(testClusterLoadAssignment)
					if err != nil {
						t.Fatalf("parsing wanted EDS response failed: %v", err)
					}
					if !cmp.Equal(gotEDS, want) {
						t.Fatalf("edsBalancer got eds: %v, want %v", gotEDS, testClusterLoadAssignment)
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
func (s) TestXdsBalanceHandleBalancerConfigChildPolicyUpdate(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()

	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()
	for _, test := range []struct {
		cfg                 *XDSConfig
		responseToSend      *xdspb.DiscoveryResponse
		expectedChildPolicy *loadBalancingConfig
	}{
		{
			cfg: &XDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   fakeBalancerA,
					Config: json.RawMessage("{}"),
				},
				EDSServiceName: testEDSClusterName,
			},
			responseToSend: testEDSResp,
			expectedChildPolicy: &loadBalancingConfig{
				Name:   string(fakeBalancerA),
				Config: json.RawMessage(`{}`),
			},
		},
		{
			cfg: &XDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   fakeBalancerB,
					Config: json.RawMessage("{}"),
				},
				EDSServiceName: testEDSClusterName,
			},
			expectedChildPolicy: &loadBalancingConfig{
				Name:   string(fakeBalancerB),
				Config: json.RawMessage(`{}`),
			},
		},
	} {
		td, cleanup := fakexds.StartServer(t)
		cleanups = append(cleanups, cleanup)
		test.cfg.BalancerName = td.Address

		lb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: test.cfg})
		if test.responseToSend != nil {
			td.ResponseChan <- &fakexds.Response{Resp: test.responseToSend}
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
func (s) TestXdsBalanceHandleBalancerConfigFallBackUpdate(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	testXDSClient := newFakeXDSClient()
	originalxdsclientNew := xdsclientNew
	xdsclientNew = func(opts xdsclient.Options) (xdsClientInterface, error) {
		return testXDSClient, nil
	}
	defer func() {
		xdsclientNew = originalxdsclientNew
	}()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testEDSClusterName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()

	cfg := XDSConfig{
		BalancerName:   "wrong-balancer-name",
		ChildPolicy:    &loadBalancingConfig{Name: fakeBalancerA},
		FallBackPolicy: &loadBalancingConfig{Name: fakeBalancerA},
	}
	lb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: &cfg})

	addrs := []resolver.Address{{Addr: "1.1.1.1:10001"}, {Addr: "2.2.2.2:10002"}, {Addr: "3.3.3.3:10003"}}
	cfg2 := cfg
	cfg2.FallBackPolicy = &loadBalancingConfig{Name: fakeBalancerB}
	lb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Addresses: addrs},
		BalancerConfig: &cfg2,
	})

	// Callback with an EDS update, the balancer will build a EDS balancer, not
	// a fallback.
	testXDSClient.callEDSCallback(xdsclient.ParseEDSRespProtoForTesting(testClusterLoadAssignment), nil)

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

	// Call loseContact explicitly, error in EDS callback is not handled.
	// Eventually, this should call EDS ballback with an error that indicates
	// "lost contact".
	lb.loseContact()

	// verify fallback balancer B takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerB), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerB))
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout when getting new subconn result")
	}

	cfg3 := cfg
	cfg3.FallBackPolicy = &loadBalancingConfig{Name: fakeBalancerA}
	lb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Addresses: addrs},
		BalancerConfig: &cfg3,
	})

	// verify fallback balancer A takes over
	select {
	case nsc := <-cc.newSubConns:
		if !reflect.DeepEqual(append(addrs, specialAddrForBalancerA), nsc) {
			t.Fatalf("got new subconn address %v, want %v", nsc, append(addrs, specialAddrForBalancerA))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout when getting new subconn result")
	}
}

func (s) TestXdsBalancerHandlerSubConnStateChange(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	testXDSClient := newFakeXDSClient()
	originalxdsclientNew := xdsclientNew
	xdsclientNew = func(opts xdsclient.Options) (xdsClientInterface, error) {
		return testXDSClient, nil
	}
	defer func() {
		xdsclientNew = originalxdsclientNew
	}()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testEDSClusterName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()

	cfg := &XDSConfig{
		BalancerName:   "wrong-balancer-name",
		ChildPolicy:    &loadBalancingConfig{Name: fakeBalancerA},
		FallBackPolicy: &loadBalancingConfig{Name: fakeBalancerA},
		EDSServiceName: testEDSClusterName,
	}
	lb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: cfg})

	// Callback with an EDS update, the balancer will build a EDS balancer, not
	// a fallback.
	testXDSClient.callEDSCallback(xdsclient.ParseEDSRespProtoForTesting(testClusterLoadAssignment), nil)

	expectedScStateChange := &scStateChange{
		sc:    &fakeSubConn{},
		state: connectivity.Ready,
	}

	var i int
	for i = 0; i < 10; i++ {
		if edsLB := getLatestEdsBalancer(); edsLB != nil {
			lb.UpdateSubConnState(expectedScStateChange.sc, balancer.SubConnState{ConnectivityState: expectedScStateChange.state})
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
	// Call loseContact explicitly, error in EDS callback is not handled.
	// Eventually, this should call EDS ballback with an error that indicates
	// "lost contact".
	lb.loseContact()

	// switch to fallback
	// fallback balancer A takes over
	for i = 0; i < 10; i++ {
		if fblb := lbABuilder.getLastBalancer(); fblb != nil {
			lb.UpdateSubConnState(expectedScStateChange.sc, balancer.SubConnState{ConnectivityState: expectedScStateChange.state})
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

func (s) TestXdsBalancerFallBackSignalFromEdsBalancer(t *testing.T) {
	originalNewEDSBalancer := newEDSBalancer
	newEDSBalancer = newFakeEDSBalancer
	defer func() {
		newEDSBalancer = originalNewEDSBalancer
	}()

	testXDSClient := newFakeXDSClient()
	originalxdsclientNew := xdsclientNew
	xdsclientNew = func(opts xdsclient.Options) (xdsClientInterface, error) {
		return testXDSClient, nil
	}
	defer func() {
		xdsclientNew = originalxdsclientNew
	}()

	builder := balancer.Get(edsName)
	cc := newTestClientConn()
	lb, ok := builder.Build(cc, balancer.BuildOptions{Target: resolver.Target{Endpoint: testEDSClusterName}}).(*edsBalancer)
	if !ok {
		t.Fatalf("unable to type assert to *edsBalancer")
	}
	defer lb.Close()

	cfg := &XDSConfig{
		BalancerName:   "wrong-balancer-name",
		ChildPolicy:    &loadBalancingConfig{Name: fakeBalancerA},
		FallBackPolicy: &loadBalancingConfig{Name: fakeBalancerA},
	}
	lb.UpdateClientConnState(balancer.ClientConnState{BalancerConfig: cfg})

	// Callback with an EDS update, the balancer will build a EDS balancer, not
	// a fallback.
	testXDSClient.callEDSCallback(xdsclient.ParseEDSRespProtoForTesting(testClusterLoadAssignment), nil)

	expectedScStateChange := &scStateChange{
		sc:    &fakeSubConn{},
		state: connectivity.Ready,
	}

	var i int
	for i = 0; i < 10; i++ {
		if edsLB := getLatestEdsBalancer(); edsLB != nil {
			lb.UpdateSubConnState(expectedScStateChange.sc, balancer.SubConnState{ConnectivityState: expectedScStateChange.state})
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
	// Call loseContact explicitly, error in EDS callback is not handled.
	// Eventually, this should call EDS ballback with an error that indicates
	// "lost contact".
	lb.loseContact()

	// switch to fallback
	// fallback balancer A takes over
	for i = 0; i < 10; i++ {
		if fblb := lbABuilder.getLastBalancer(); fblb != nil {
			lb.UpdateSubConnState(expectedScStateChange.sc, balancer.SubConnState{ConnectivityState: expectedScStateChange.state})
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

func TestXdsBalancerConfigParsing(t *testing.T) {
	const testEDSName = "eds.service"
	var testLRSName = "lrs.server"
	b := bytes.NewBuffer(nil)
	if err := (&jsonpb.Marshaler{}).Marshal(b, &scpb.XdsConfig{
		ChildPolicy: []*scpb.LoadBalancingConfig{
			{Policy: &scpb.LoadBalancingConfig_Xds{}},
			{Policy: &scpb.LoadBalancingConfig_RoundRobin{
				RoundRobin: &scpb.RoundRobinConfig{},
			}},
		},
		FallbackPolicy: []*scpb.LoadBalancingConfig{
			{Policy: &scpb.LoadBalancingConfig_Xds{}},
			{Policy: &scpb.LoadBalancingConfig_PickFirst{
				PickFirst: &scpb.PickFirstConfig{},
			}},
		},
		EdsServiceName:             testEDSName,
		LrsLoadReportingServerName: &wrapperspb.StringValue{Value: testLRSName},
	}); err != nil {
		t.Fatalf("%v", err)
	}

	tests := []struct {
		name    string
		js      json.RawMessage
		want    serviceconfig.LoadBalancingConfig
		wantErr bool
	}{
		{
			name: "jsonpb-generated",
			js:   b.Bytes(),
			want: &XDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "round_robin",
					Config: json.RawMessage("{}"),
				},
				FallBackPolicy: &loadBalancingConfig{
					Name:   "pick_first",
					Config: json.RawMessage("{}"),
				},
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: &testLRSName,
			},
			wantErr: false,
		},
		{
			// json with random balancers, and the first is not registered.
			name: "manually-generated",
			js: json.RawMessage(`
{
  "balancerName": "fake.foo.bar",
  "childPolicy": [
    {"fake_balancer_C": {}},
    {"fake_balancer_A": {}},
    {"fake_balancer_B": {}}
  ],
  "fallbackPolicy": [
    {"fake_balancer_C": {}},
    {"fake_balancer_B": {}},
    {"fake_balancer_A": {}}
  ],
  "edsServiceName": "eds.service",
  "lrsLoadReportingServerName": "lrs.server"
}`),
			want: &XDSConfig{
				BalancerName: "fake.foo.bar",
				ChildPolicy: &loadBalancingConfig{
					Name:   "fake_balancer_A",
					Config: json.RawMessage("{}"),
				},
				FallBackPolicy: &loadBalancingConfig{
					Name:   "fake_balancer_B",
					Config: json.RawMessage("{}"),
				},
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: &testLRSName,
			},
			wantErr: false,
		},
		{
			// json with no lrs server name, LrsLoadReportingServerName should
			// be nil (not an empty string).
			name: "no-lrs-server-name",
			js: json.RawMessage(`
{
  "balancerName": "fake.foo.bar",
  "edsServiceName": "eds.service"
}`),
			want: &XDSConfig{
				BalancerName:               "fake.foo.bar",
				EDSServiceName:             testEDSName,
				LrsLoadReportingServerName: nil,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &edsBalancerBuilder{}
			got, err := b.ParseConfig(tt.js)
			if (err != nil) != tt.wantErr {
				t.Errorf("edsBalancerBuilder.ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf(cmp.Diff(got, tt.want))
			}
		})
	}
}
func TestLoadbalancingConfigParsing(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want *XDSConfig
	}{
		{
			name: "empty",
			s:    "{}",
			want: &XDSConfig{},
		},
		{
			name: "success1",
			s:    `{"childPolicy":[{"pick_first":{}}]}`,
			want: &XDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "pick_first",
					Config: json.RawMessage(`{}`),
				},
			},
		},
		{
			name: "success2",
			s:    `{"childPolicy":[{"round_robin":{}},{"pick_first":{}}]}`,
			want: &XDSConfig{
				ChildPolicy: &loadBalancingConfig{
					Name:   "round_robin",
					Config: json.RawMessage(`{}`),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg XDSConfig
			if err := json.Unmarshal([]byte(tt.s), &cfg); err != nil || !reflect.DeepEqual(&cfg, tt.want) {
				t.Errorf("test name: %s, parseFullServiceConfig() = %+v, err: %v, want %+v, <nil>", tt.name, cfg, err, tt.want)
			}
		})
	}
}

func TestEqualStringPointers(t *testing.T) {
	var (
		ta1 = "test-a"
		ta2 = "test-a"
		tb  = "test-b"
	)
	tests := []struct {
		name string
		a    *string
		b    *string
		want bool
	}{
		{"both-nil", nil, nil, true},
		{"a-non-nil", &ta1, nil, false},
		{"b-non-nil", nil, &tb, false},
		{"equal", &ta1, &ta2, true},
		{"different", &ta1, &tb, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalStringPointers(tt.a, tt.b); got != tt.want {
				t.Errorf("equalStringPointers() = %v, want %v", got, tt.want)
			}
		})
	}
}
