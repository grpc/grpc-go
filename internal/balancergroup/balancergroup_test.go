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

package balancergroup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/balancer/weightedtarget/weightedaggregator"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

var (
	rrBuilder            = balancer.Get(roundrobin.Name)
	testBalancerIDs      = []string{"b1", "b2", "b3"}
	testBackendAddrs     []resolver.Address
	testBackendEndpoints []resolver.Endpoint
)

const testBackendAddrsCount = 12

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		addr := resolver.Address{Addr: fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i)}
		testBackendAddrs = append(testBackendAddrs, addr)
		testBackendEndpoints = append(testBackendEndpoints, resolver.Endpoint{Addresses: []resolver.Address{addr}})
	}
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Create a new balancer group, add balancer and backends.
// - b1, weight 2, backends [0,1]
// - b2, weight 1, backends [2,3]
// Start the balancer group and check behavior.
//
// Close the balancer group.
func (s) TestBalancerGroup_start_close(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(Options{
		CC:                      cc,
		BuildOpts:               balancer.BuildOptions{},
		StateAggregator:         gator,
		Logger:                  nil,
		SubBalancerCloseTimeout: time.Duration(0),
	})

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[2:4]}})

	m1 := make(map[string]balancer.SubConn)
	for i := 0; i < 4; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m1[addrs[0].Addr] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		m1[testBackendAddrs[0].Addr], m1[testBackendAddrs[0].Addr],
		m1[testBackendAddrs[1].Addr], m1[testBackendAddrs[1].Addr],
		m1[testBackendAddrs[2].Addr], m1[testBackendAddrs[3].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	gator.Stop()
	bg.Close()
	for i := 0; i < 4; i++ {
		(<-cc.ShutdownSubConnCh).UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Shutdown})
	}
}

// Test that balancer group start() doesn't deadlock if the balancer calls back
// into balancer group inline when it gets an update.
//
// The potential deadlock can happen if we
//   - hold a lock and send updates to balancer (e.g. update resolved addresses)
//   - the balancer calls back (NewSubConn or update picker) in line
//
// The callback will try to hold the same lock again, which will cause a
// deadlock.
//
// This test starts the balancer group with a test balancer, will update picker
// whenever it gets an address update. It's expected that start() doesn't block
// because of deadlock.
func (s) TestBalancerGroup_start_close_deadlock(t *testing.T) {
	const balancerName = "stub-TestBalancerGroup_start_close_deadlock"
	stub.Register(balancerName, stub.BalancerFuncs{})
	builder := balancer.Get(balancerName)

	cc := testutils.NewBalancerClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(Options{
		CC:                      cc,
		BuildOpts:               balancer.BuildOptions{},
		StateAggregator:         gator,
		Logger:                  nil,
		SubBalancerCloseTimeout: time.Duration(0),
	})

	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], builder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], builder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Addresses: testBackendAddrs[2:4]}})
}

// initBalancerGroupForCachingTest creates a balancer group, and initialize it
// to be ready for caching tests.
//
// Two rr balancers are added to bg, each with 2 ready subConns. A sub-balancer
// is removed later, so the balancer group returned has one sub-balancer in its
// own map, and one sub-balancer in cache.
func initBalancerGroupForCachingTest(t *testing.T, idleCacheTimeout time.Duration) (*weightedaggregator.Aggregator, *BalancerGroup, *testutils.BalancerClientConn, map[string]*testutils.TestSubConn) {
	cc := testutils.NewBalancerClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(Options{
		CC:                      cc,
		BuildOpts:               balancer.BuildOptions{},
		StateAggregator:         gator,
		Logger:                  nil,
		SubBalancerCloseTimeout: idleCacheTimeout,
	})

	// Add two balancers to group and send two resolved addresses to both
	// balancers.
	gator.Add(testBalancerIDs[0], 2)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[0:2]}})
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[2:4]}})

	m1 := make(map[string]*testutils.TestSubConn)
	for i := 0; i < 4; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m1[addrs[0].Addr] = sc
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	// Test roundrobin on the last picker.
	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		m1[testBackendAddrs[0].Addr], m1[testBackendAddrs[0].Addr],
		m1[testBackendAddrs[1].Addr], m1[testBackendAddrs[1].Addr],
		m1[testBackendAddrs[2].Addr], m1[testBackendAddrs[3].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p1)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	gator.Remove(testBalancerIDs[1])
	bg.Remove(testBalancerIDs[1])
	// Don't wait for SubConns to be removed after close, because they are only
	// removed after close timeout.
	for i := 0; i < 10; i++ {
		select {
		case sc := <-cc.ShutdownSubConnCh:
			t.Fatalf("Got request to shut down subconn %v, want no shut down subconn (because subconns were still in cache)", sc)
		default:
		}
		time.Sleep(time.Millisecond)
	}
	// Test roundrobin on the with only sub-balancer0.
	p2 := <-cc.NewPickerCh
	want = []balancer.SubConn{
		m1[testBackendAddrs[0].Addr], m1[testBackendAddrs[1].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p2)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	return gator, bg, cc, m1
}

// Test that if a sub-balancer is removed, and re-added within close timeout,
// the subConns won't be re-created.
func (s) TestBalancerGroup_locality_caching(t *testing.T) {
	gator, bg, cc, addrToSC := initBalancerGroupForCachingTest(t, defaultTestTimeout)

	// Turn down subconn for addr2, shouldn't get picker update because
	// sub-balancer1 was removed.
	addrToSC[testBackendAddrs[2].Addr].UpdateState(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   errors.New("test error"),
	})
	for i := 0; i < 10; i++ {
		select {
		case <-cc.NewPickerCh:
			t.Fatalf("Got new picker, want no new picker (because the sub-balancer was removed)")
		default:
		}
		time.Sleep(defaultTestShortTimeout)
	}

	// Re-add sub-balancer-1, because subconns were in cache, no new subconns
	// should be created. But a new picker will still be generated, with subconn
	// states update to date.
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], rrBuilder)

	p3 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		addrToSC[testBackendAddrs[0].Addr], addrToSC[testBackendAddrs[0].Addr],
		addrToSC[testBackendAddrs[1].Addr], addrToSC[testBackendAddrs[1].Addr],
		// addr2 is down, b2 only has addr3 in READY state.
		addrToSC[testBackendAddrs[3].Addr], addrToSC[testBackendAddrs[3].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}

	for i := 0; i < 10; i++ {
		select {
		case <-cc.NewSubConnAddrsCh:
			t.Fatalf("Got new subconn, want no new subconn (because subconns were still in cache)")
		default:
		}
		time.Sleep(defaultTestShortTimeout)
	}
}

// Sub-balancers are put in cache when they are shut down. If balancer group is
// closed within close timeout, all subconns should still be removed
// immediately.
func (s) TestBalancerGroup_locality_caching_close_group(t *testing.T) {
	_, bg, cc, addrToSC := initBalancerGroupForCachingTest(t, defaultTestTimeout)

	bg.Close()
	// The balancer group is closed. The subconns should be shutdown immediately.
	shutdownTimeout := time.After(time.Millisecond * 500)
	scToShutdown := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[0].Addr]: 1,
		addrToSC[testBackendAddrs[1].Addr]: 1,
		addrToSC[testBackendAddrs[2].Addr]: 1,
		addrToSC[testBackendAddrs[3].Addr]: 1,
	}
	for i := 0; i < len(scToShutdown); i++ {
		select {
		case sc := <-cc.ShutdownSubConnCh:
			c := scToShutdown[sc]
			if c == 0 {
				t.Fatalf("Got Shutdown for %v when there's %d shutdown expected", sc, c)
			}
			scToShutdown[sc] = c - 1
		case <-shutdownTimeout:
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be shut down")
		}
	}
}

// Sub-balancers in cache will be closed if not re-added within timeout, and
// subConns will be shut down.
func (s) TestBalancerGroup_locality_caching_not_read_within_timeout(t *testing.T) {
	_, _, cc, addrToSC := initBalancerGroupForCachingTest(t, time.Second)

	// The sub-balancer is not re-added within timeout. The subconns should be
	// shut down.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	scToShutdown := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[2].Addr]: 1,
		addrToSC[testBackendAddrs[3].Addr]: 1,
	}
	for i := 0; i < len(scToShutdown); i++ {
		select {
		case sc := <-cc.ShutdownSubConnCh:
			c := scToShutdown[sc]
			if c == 0 {
				t.Fatalf("Got Shutdown for %v when there's %d shutdown expected", sc, c)
			}
			scToShutdown[sc] = c - 1
		case <-ctx.Done():
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be shut down")
		}
	}
}

// Wrap the rr builder, so it behaves the same, but has a different name.
type noopBalancerBuilderWrapper struct {
	balancer.Builder
}

func init() {
	balancer.Register(&noopBalancerBuilderWrapper{Builder: rrBuilder})
}

func (*noopBalancerBuilderWrapper) Name() string {
	return "noopBalancerBuilderWrapper"
}

// After removing a sub-balancer, re-add with same ID, but different balancer
// builder. Old subconns should be shut down, and new subconns should be created.
func (s) TestBalancerGroup_locality_caching_read_with_different_builder(t *testing.T) {
	gator, bg, cc, addrToSC := initBalancerGroupForCachingTest(t, defaultTestTimeout)

	// Re-add sub-balancer-1, but with a different balancer builder. The
	// sub-balancer was still in cache, but can't be reused. This should cause
	// old sub-balancer's subconns to be shut down immediately, and new
	// subconns to be created.
	gator.Add(testBalancerIDs[1], 1)
	bg.Add(testBalancerIDs[1], &noopBalancerBuilderWrapper{rrBuilder})

	// The cached sub-balancer should be closed, and the subconns should be
	// shut down immediately.
	shutdownTimeout := time.After(time.Millisecond * 500)
	scToShutdown := map[balancer.SubConn]int{
		addrToSC[testBackendAddrs[2].Addr]: 1,
		addrToSC[testBackendAddrs[3].Addr]: 1,
	}
	for i := 0; i < len(scToShutdown); i++ {
		select {
		case sc := <-cc.ShutdownSubConnCh:
			c := scToShutdown[sc]
			if c == 0 {
				t.Fatalf("Got Shutdown for %v when there's %d shutdown expected", sc, c)
			}
			scToShutdown[sc] = c - 1
		case <-shutdownTimeout:
			t.Fatalf("timeout waiting for subConns (from balancer in cache) to be shut down")
		}
	}

	bg.UpdateClientConnState(testBalancerIDs[1], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[4:6]}})

	newSCTimeout := time.After(time.Millisecond * 500)
	scToAdd := map[string]int{
		testBackendAddrs[4].Addr: 1,
		testBackendAddrs[5].Addr: 1,
	}
	for i := 0; i < len(scToAdd); i++ {
		select {
		case addr := <-cc.NewSubConnAddrsCh:
			c := scToAdd[addr[0].Addr]
			if c == 0 {
				t.Fatalf("Got newSubConn for %v when there's %d new expected", addr, c)
			}
			scToAdd[addr[0].Addr] = c - 1
			sc := <-cc.NewSubConnCh
			addrToSC[addr[0].Addr] = sc
			sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
			sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
		case <-newSCTimeout:
			t.Fatalf("timeout waiting for subConns (from new sub-balancer) to be newed")
		}
	}

	// Test roundrobin on the new picker.
	p3 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		addrToSC[testBackendAddrs[0].Addr], addrToSC[testBackendAddrs[0].Addr],
		addrToSC[testBackendAddrs[1].Addr], addrToSC[testBackendAddrs[1].Addr],
		addrToSC[testBackendAddrs[4].Addr], addrToSC[testBackendAddrs[5].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p3)); err != nil {
		t.Fatalf("want %v, got %v", want, err)
	}
}

// After removing a sub-balancer, it will be kept in cache. Make sure that this
// sub-balancer's Close is called when the balancer group is closed.
func (s) TestBalancerGroup_CloseStopsBalancerInCache(t *testing.T) {
	const balancerName = "stub-TestBalancerGroup_check_close"
	closed := make(chan struct{})
	stub.Register(balancerName, stub.BalancerFuncs{Close: func(_ *stub.BalancerData) {
		close(closed)
	}})
	builder := balancer.Get(balancerName)

	gator, bg, _, _ := initBalancerGroupForCachingTest(t, time.Second)

	// Add balancer, and remove
	gator.Add(testBalancerIDs[2], 1)
	bg.Add(testBalancerIDs[2], builder)
	gator.Remove(testBalancerIDs[2])
	bg.Remove(testBalancerIDs[2])

	// Immediately close balancergroup, before the cache timeout.
	bg.Close()

	// Make sure the removed child balancer is closed eventually.
	select {
	case <-closed:
	case <-time.After(time.Second * 2):
		t.Fatalf("timeout waiting for the child balancer in cache to be closed")
	}
}

// TestBalancerGroupBuildOptions verifies that the balancer.BuildOptions passed
// to the balancergroup at creation time is passed to child policies.
func (s) TestBalancerGroupBuildOptions(t *testing.T) {
	const (
		balancerName = "stubBalancer-TestBalancerGroupBuildOptions"
		userAgent    = "ua"
	)

	// Setup the stub balancer such that we can read the build options passed to
	// it in the UpdateClientConnState method.
	bOpts := balancer.BuildOptions{
		DialCreds:       insecure.NewCredentials(),
		ChannelzParent:  channelz.RegisterChannel(nil, "test channel"),
		CustomUserAgent: userAgent,
	}
	stub.Register(balancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			if bd.BuildOptions.DialCreds != bOpts.DialCreds || bd.BuildOptions.ChannelzParent != bOpts.ChannelzParent || bd.BuildOptions.CustomUserAgent != bOpts.CustomUserAgent {
				return fmt.Errorf("buildOptions in child balancer: %v, want %v", bd, bOpts)
			}
			return nil
		},
	})
	cc := testutils.NewBalancerClientConn(t)
	bg := New(Options{
		CC:              cc,
		BuildOpts:       bOpts,
		StateAggregator: nil,
		Logger:          nil,
	})

	// Add the stub balancer build above as a child policy.
	balancerBuilder := balancer.Get(balancerName)
	bg.Add(testBalancerIDs[0], balancerBuilder)

	// Send an empty clientConn state change. This should trigger the
	// verification of the buildOptions being passed to the child policy.
	if err := bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{}); err != nil {
		t.Fatal(err)
	}
}

func (s) TestBalancerGroup_UpdateClientConnState_AfterClose(t *testing.T) {
	balancerName := t.Name()
	clientConnStateCh := make(chan struct{}, 1)

	stub.Register(balancerName, stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, _ balancer.ClientConnState) error {
			clientConnStateCh <- struct{}{}
			return nil
		},
	})

	bg := New(Options{
		CC:              testutils.NewBalancerClientConn(t),
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})

	bg.Add(testBalancerIDs[0], balancer.Get(balancerName))
	bg.Close()

	if err := bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{}); err != nil {
		t.Fatalf("Expected nil error, got %v", err)
	}

	select {
	case <-clientConnStateCh:
		t.Fatalf("UpdateClientConnState was called after BalancerGroup was closed")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestBalancerGroup_ResolverError_AfterClose(t *testing.T) {
	balancerName := t.Name()
	resolveErrorCh := make(chan struct{}, 1)

	stub.Register(balancerName, stub.BalancerFuncs{
		ResolverError: func(_ *stub.BalancerData, _ error) {
			resolveErrorCh <- struct{}{}
		},
	})

	bg := New(Options{
		CC:              testutils.NewBalancerClientConn(t),
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})

	bg.Add(testBalancerIDs[0], balancer.Get(balancerName))
	bg.Close()

	bg.ResolverError(errors.New("test error"))

	select {
	case <-resolveErrorCh:
		t.Fatalf("ResolverError was called on sub-balancer after BalancerGroup was closed")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestBalancerExitIdleOne(t *testing.T) {
	const balancerName = "stub-balancer-test-balancergroup-exit-idle-one"
	exitIdleCh := make(chan struct{}, 1)
	stub.Register(balancerName, stub.BalancerFuncs{
		ExitIdle: func(*stub.BalancerData) {
			exitIdleCh <- struct{}{}
		},
	})
	cc := testutils.NewBalancerClientConn(t)
	bg := New(Options{
		CC:              cc,
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})
	defer bg.Close()

	// Add the stub balancer build above as a child policy.
	builder := balancer.Get(balancerName)
	bg.Add(testBalancerIDs[0], builder)

	// Call ExitIdleOne on the child policy.
	bg.ExitIdleOne(testBalancerIDs[0])
	select {
	case <-time.After(time.Second):
		t.Fatal("Timeout when waiting for ExitIdle to be invoked on child policy")
	case <-exitIdleCh:
	}
}

func (s) TestBalancerGroup_ExitIdleOne_AfterClose(t *testing.T) {
	balancerName := t.Name()
	exitIdleCh := make(chan struct{})

	stub.Register(balancerName, stub.BalancerFuncs{
		ExitIdle: func(_ *stub.BalancerData) {
			close(exitIdleCh)
		},
	})

	bg := New(Options{
		CC:              testutils.NewBalancerClientConn(t),
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})

	bg.Add(testBalancerIDs[0], balancer.Get(balancerName))
	bg.Close()
	bg.ExitIdleOne(testBalancerIDs[0])

	select {
	case <-time.After(defaultTestShortTimeout):
	case <-exitIdleCh:
		t.Fatalf("ExitIdleOne called ExitIdle on sub-balancer after BalancerGroup was closed")
	}
}

func (s) TestBalancerGroup_ExitIdleOne_NonExistentID(t *testing.T) {
	balancerName := t.Name()
	exitIdleCh := make(chan struct{}, 1)

	stub.Register(balancerName, stub.BalancerFuncs{
		ExitIdle: func(_ *stub.BalancerData) {
			exitIdleCh <- struct{}{}
		},
	})

	bg := New(Options{
		CC:              testutils.NewBalancerClientConn(t),
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})
	defer bg.Close()

	bg.Add(testBalancerIDs[0], balancer.Get(balancerName))
	bg.ExitIdleOne("non-existent-id")

	select {
	case <-time.After(defaultTestShortTimeout):
	case <-exitIdleCh:
		t.Fatalf("ExitIdleOne called ExitIdle on wrong sub-balancer ID")
	}
}

// TestBalancerGracefulSwitch tests the graceful switch functionality for a
// child of the balancer group. At first, the child is configured as a round
// robin load balancer, and thus should behave accordingly. The test then
// gracefully switches this child to a custom type which only creates a SubConn
// for the second passed in address and also only picks that created SubConn.
// The new aggregated picker should reflect this change for the child.
func (s) TestBalancerGracefulSwitch(t *testing.T) {
	cc := testutils.NewBalancerClientConn(t)
	gator := weightedaggregator.New(cc, nil, testutils.NewTestWRR)
	gator.Start()
	bg := New(Options{
		CC:              cc,
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: gator,
		Logger:          nil,
	})
	gator.Add(testBalancerIDs[0], 1)
	bg.Add(testBalancerIDs[0], rrBuilder)
	bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{ResolverState: resolver.State{Endpoints: testBackendEndpoints[0:2]}})

	defer bg.Close()

	m1 := make(map[string]balancer.SubConn)
	scs := make(map[balancer.SubConn]bool)
	for i := 0; i < 2; i++ {
		addrs := <-cc.NewSubConnAddrsCh
		sc := <-cc.NewSubConnCh
		m1[addrs[0].Addr] = sc
		scs[sc] = true
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
		sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	}

	p1 := <-cc.NewPickerCh
	want := []balancer.SubConn{
		m1[testBackendAddrs[0].Addr], m1[testBackendAddrs[1].Addr],
	}
	if err := testutils.IsRoundRobin(want, testutils.SubConnFromPicker(p1)); err != nil {
		t.Fatal(err)
	}

	// The balancer type for testBalancersIDs[0] is currently Round Robin. Now,
	// change it to a balancer that has separate behavior logically (creating
	// SubConn for second address in address list and always picking that
	// SubConn), and see if the downstream behavior reflects that change.
	childPolicyName := t.Name()
	stub.Register(childPolicyName, stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = balancer.Get(pickfirst.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.ResolverState.Endpoints = ccs.ResolverState.Endpoints[1:]
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
	})
	cfgJSON := json.RawMessage(fmt.Sprintf(`[{%q: {}}]`, t.Name()))
	lbCfg, err := ParseConfig(cfgJSON)
	if err != nil {
		t.Fatalf("ParseConfig(%s) failed: %v", string(cfgJSON), err)
	}
	if err := bg.UpdateClientConnState(testBalancerIDs[0], balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: testBackendEndpoints[2:4]},
		BalancerConfig: lbCfg,
	}); err != nil {
		t.Fatalf("error updating ClientConn state: %v", err)
	}

	addrs := <-cc.NewSubConnAddrsCh
	if addrs[0].Addr != testBackendAddrs[3].Addr {
		// Verifies forwarded to new created balancer, as the wrapped pick first
		// balancer will delete first address.
		t.Fatalf("newSubConn called with wrong address, want: %v, got : %v", testBackendAddrs[3].Addr, addrs[0].Addr)
	}
	sc := <-cc.NewSubConnCh

	// Update the pick first balancers SubConn as CONNECTING. This will cause
	// the pick first balancer to UpdateState() with CONNECTING, which shouldn't send
	// a Picker update back, as the Graceful Switch process is not complete.
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
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
	// Picker should always pick the pick first's created SubConn which
	// corresponds to address 3.
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p2 := <-cc.NewPickerCh
	pr, err := p2.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("error picking: %v", err)
	}
	if pr.SubConn != sc {
		t.Fatalf("picker.Pick(), want %v, got %v", sc, pr.SubConn)
	}

	// The Graceful Switch process completing for the child should cause the
	// SubConns for the balancer being gracefully switched from to get deleted.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("error waiting for Shutdown()")
		case sc := <-cc.ShutdownSubConnCh:
			// The SubConn shut down should have been one of the two created
			// SubConns, and both should be deleted.
			if ok := scs[sc]; ok {
				delete(scs, sc)
				continue
			} else {
				t.Fatalf("Shutdown called for wrong SubConn %v, want in %v", sc, scs)
			}
		}
	}
}

func (s) TestBalancerExitIdle_All(t *testing.T) {
	balancer1 := t.Name() + "-1"
	balancer2 := t.Name() + "-2"

	testID1, testID2 := testBalancerIDs[0], testBalancerIDs[1]

	exitIdleCh1, exitIdleCh2 := make(chan struct{}, 1), make(chan struct{}, 1)

	stub.Register(balancer1, stub.BalancerFuncs{
		ExitIdle: func(_ *stub.BalancerData) {
			exitIdleCh1 <- struct{}{}
		},
	})

	stub.Register(balancer2, stub.BalancerFuncs{
		ExitIdle: func(_ *stub.BalancerData) {
			exitIdleCh2 <- struct{}{}
		},
	})

	cc := testutils.NewBalancerClientConn(t)
	bg := New(Options{
		CC:              cc,
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})
	defer bg.Close()

	bg.Add(testID1, balancer.Get(balancer1))
	bg.Add(testID2, balancer.Get(balancer2))

	bg.ExitIdle()

	errCh := make(chan error, 2)

	go func() {
		select {
		case <-exitIdleCh1:
			errCh <- nil
		case <-time.After(defaultTestTimeout):
			errCh <- fmt.Errorf("timeout waiting for ExitIdle on balancer1")
		}
	}()

	go func() {
		select {
		case <-exitIdleCh2:
			errCh <- nil
		case <-time.After(defaultTestTimeout):
			errCh <- fmt.Errorf("timeout waiting for ExitIdle on balancer2")
		}
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func (s) TestBalancerGroup_ExitIdle_AfterClose(t *testing.T) {
	balancerName := t.Name()
	exitIdleCh := make(chan struct{}, 1)

	stub.Register(balancerName, stub.BalancerFuncs{
		ExitIdle: func(_ *stub.BalancerData) {
			exitIdleCh <- struct{}{}
		},
	})

	bg := New(Options{
		CC:              testutils.NewBalancerClientConn(t),
		BuildOpts:       balancer.BuildOptions{},
		StateAggregator: nil,
		Logger:          nil,
	})

	bg.Add(testBalancerIDs[0], balancer.Get(balancerName))
	bg.Close()
	bg.ExitIdle()

	select {
	case <-exitIdleCh:
		t.Fatalf("ExitIdle was called on sub-balancer even after BalancerGroup was closed")
	case <-time.After(defaultTestShortTimeout):
	}
}
