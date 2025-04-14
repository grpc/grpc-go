/*
 *
 * Copyright 2021 gRPC authors.
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

package ringhash

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/grpctest"
	iringhash "google.golang.org/grpc/internal/ringhash"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond

	testBackendAddrsCount = 12
)

var (
	testBackendAddrStrs []string
	testConfig          = &iringhash.LBConfig{MinRingSize: 1, MaxRingSize: 10}
)

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
}

// setupTest creates the balancer, and does an initial sanity check.
func setupTest(t *testing.T, endpoints []resolver.Endpoint) (*testutils.BalancerClientConn, balancer.Balancer, balancer.Picker) {
	t.Helper()
	cc := testutils.NewBalancerClientConn(t)
	builder := balancer.Get(Name)
	b := builder.Build(cc, balancer.BuildOptions{})
	if b == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", Name)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: endpoints},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}

	// The leaf pickfirst are created lazily, only when their endpoint is picked
	// or other endpoints are in TF. No SubConns should be created immediately.
	select {
	case sc := <-cc.NewSubConnCh:
		t.Errorf("unexpected SubConn creation: %v", sc)
	case <-time.After(defaultTestShortTimeout):
	}

	// Should also have a picker, with all endpoints in Idle.
	p1 := <-cc.NewPickerCh

	ringHashPicker := p1.(*picker)
	if got, want := len(ringHashPicker.endpointStates), len(endpoints); got != want {
		t.Errorf("Number of child balancers = %d, want = %d", got, want)
	}
	for firstAddr, bs := range ringHashPicker.endpointStates {
		if got, want := bs.state.ConnectivityState, connectivity.Idle; got != want {
			t.Errorf("Child balancer connectivity state for address %q = %v, want = %v", firstAddr, got, want)
		}
	}
	return cc, b, p1
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestUpdateClientConnState_NewRingSize tests the scenario where the ringhash
// LB policy receives new configuration which specifies new values for the ring
// min and max sizes. The test verifies that a new ring is created and a new
// picker is sent to the ClientConn.
func (s) TestUpdateClientConnState_NewRingSize(t *testing.T) {
	origMinRingSize, origMaxRingSize := 1, 10 // Configured from `testConfig` in `setupTest`
	newMinRingSize, newMaxRingSize := 20, 100

	endpoints := []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}}}
	cc, b, p1 := setupTest(t, endpoints)
	ring1 := p1.(*picker).ring
	if ringSize := len(ring1.items); ringSize < origMinRingSize || ringSize > origMaxRingSize {
		t.Fatalf("Ring created with size %d, want between [%d, %d]", ringSize, origMinRingSize, origMaxRingSize)
	}

	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Endpoints: endpoints},
		BalancerConfig: &iringhash.LBConfig{
			MinRingSize: uint64(newMinRingSize),
			MaxRingSize: uint64(newMaxRingSize),
		},
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}

	var ring2 *ring
	select {
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for a picker update after a configuration update")
	case p2 := <-cc.NewPickerCh:
		ring2 = p2.(*picker).ring
	}
	if ringSize := len(ring2.items); ringSize < newMinRingSize || ringSize > newMaxRingSize {
		t.Fatalf("Ring created with size %d, want between [%d, %d]", ringSize, newMinRingSize, newMaxRingSize)
	}
}

func (s) TestOneEndpoint(t *testing.T) {
	wantAddr1 := resolver.Address{Addr: testBackendAddrStrs[0]}
	cc, _, p0 := setupTest(t, []resolver.Endpoint{{Addresses: []resolver.Address{wantAddr1}}})
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// firstHash-1 will pick the first (and only) SubConn from the ring.
	testHash := firstHash - 1
	// The first pick should be queued, and should trigger a connection to the
	// only Endpoint which has a single address.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := p0.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	var sc0 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc0 = <-cc.NewSubConnCh:
	}
	if got, want := sc0.Addresses[0].Addr, wantAddr1.Addr; got != want {
		t.Fatalf("SubConn.Addresses = %v, want = %v", got, want)
	}
	select {
	case <-sc0.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Send state updates to Ready.
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatal(err)
	}

	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != sc0 {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// TestThreeBackendsAffinity covers that there are 3 SubConns, RPCs with the
// same hash always pick the same SubConn. When the one picked is down, another
// one will be picked.
func (s) TestThreeSubConnsAffinity(t *testing.T) {
	endpoints := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[1]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[2]}}},
	}
	remainingAddrs := map[string]bool{
		testBackendAddrStrs[0]: true,
		testBackendAddrStrs[1]: true,
		testBackendAddrStrs[2]: true,
	}
	cc, _, p0 := setupTest(t, endpoints)
	// This test doesn't update addresses, so this ring will be used by all the
	// pickers.
	ring := p0.(*picker).ring

	firstHash := ring.items[0].hash
	// firstHash+1 will pick the second endpoint from the ring.
	testHash := firstHash + 1
	// The first pick should be queued, and should trigger Connect() on the only
	// SubConn.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := p0.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}

	// The picked endpoint should be the second in the ring.
	var subConns [3]*testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case subConns[1] = <-cc.NewSubConnCh:
	}
	if got, want := subConns[1].Addresses[0].Addr, ring.items[1].hashKey; got != want {
		t.Fatalf("SubConn.Address = %v, want = %v", got, want)
	}
	select {
	case <-subConns[1].ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", subConns[1])
	}
	delete(remainingAddrs, ring.items[1].hashKey)

	// Turn down the subConn in use.
	subConns[1].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	subConns[1].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// This should trigger a connection to a new endpoint.
	<-cc.NewPickerCh
	var sc *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc = <-cc.NewSubConnCh:
	}
	scAddr := sc.Addresses[0].Addr
	if _, ok := remainingAddrs[scAddr]; !ok {
		t.Fatalf("New SubConn created with previously used address: %q", scAddr)
	}
	delete(remainingAddrs, scAddr)
	select {
	case <-sc.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", subConns[1])
	}
	if scAddr == ring.items[0].hashKey {
		subConns[0] = sc
	} else if scAddr == ring.items[2].hashKey {
		subConns[2] = sc
	}

	// Turning down the SubConn should cause creation of a connection to the
	// final endpoint.
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc = <-cc.NewSubConnCh:
	}
	scAddr = sc.Addresses[0].Addr
	if _, ok := remainingAddrs[scAddr]; !ok {
		t.Fatalf("New SubConn created with previously used address: %q", scAddr)
	}
	delete(remainingAddrs, scAddr)
	select {
	case <-sc.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", subConns[1])
	}
	if scAddr == ring.items[0].hashKey {
		subConns[0] = sc
	} else if scAddr == ring.items[2].hashKey {
		subConns[2] = sc
	}
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	// All endpoints are in TransientFailure. Make the first endpoint in the
	// ring report Ready. All picks should go to this endpoint which is two
	// indexes away from the endpoint with the chosen hash.
	subConns[0].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	subConns[0].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	subConns[0].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("Context timed out while waiting for channel to report Ready.")
	}
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != subConns[0] {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, subConns[0])
		}
	}

	// Make the last endpoint in the ring report Ready. All picks should go to
	// this endpoint since it is one index away from the chosen hash.
	subConns[2].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	subConns[2].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	subConns[2].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != subConns[2] {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, subConns[2])
		}
	}

	// Make the second endpoint in the ring report Ready. All picks should go to
	// this endpoint as it is the one with the chosen hash.
	subConns[1].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	subConns[1].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	subConns[1].UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p3.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != subConns[1] {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, subConns[1])
		}
	}
}

// TestThreeBackendsAffinity covers that there are 3 SubConns, RPCs with the
// same hash always pick the same SubConn. Then try different hash to pick
// another backend, and verify the first hash still picks the first backend.
func (s) TestThreeBackendsAffinityMultiple(t *testing.T) {
	wantEndpoints := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[1]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[2]}}},
	}
	cc, _, p0 := setupTest(t, wantEndpoints)
	// This test doesn't update addresses, so this ring will be used by all the
	// pickers.
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// firstHash+1 will pick the second SubConn from the ring.
	testHash := firstHash + 1
	// The first pick should be queued, and should trigger Connect() on the only
	// SubConn.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := p0.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	// The picked SubConn should be the second in the ring.
	var sc0 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc0 = <-cc.NewSubConnCh:
	}
	if got, want := sc0.Addresses[0].Addr, ring0.items[1].hashKey; got != want {
		t.Fatalf("SubConn.Address = %v, want = %v", got, want)
	}
	select {
	case <-sc0.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Send state updates to Ready.
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatal(err)
	}

	// First hash should always pick sc0.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != sc0 {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}

	secondHash := ring0.items[1].hash
	// secondHash+1 will pick the third SubConn from the ring.
	testHash2 := secondHash + 1
	if _, err := p0.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash2)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	var sc1 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc1 = <-cc.NewSubConnCh:
	}
	if got, want := sc1.Addresses[0].Addr, ring0.items[2].hashKey; got != want {
		t.Fatalf("SubConn.Address = %v, want = %v", got, want)
	}
	select {
	case <-sc1.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc1)
	}
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// With the new generated picker, hash2 always picks sc1.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash2)})
		if gotSCSt.SubConn != sc1 {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}
	// But the first hash still picks sc0.
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, testHash)})
		if gotSCSt.SubConn != sc0 {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// TestAddrWeightChange covers the following scenarios after setting up the
// balancer with 3 addresses [A, B, C]:
//   - updates balancer with [A, B, C], a new Picker should not be sent.
//   - updates balancer with [A, B] (C removed), a new Picker is sent and the
//     ring is updated.
//   - updates balancer with [A, B], but B has a weight of 2, a new Picker is
//     sent.  And the new ring should contain the correct number of entries
//     and weights.
func (s) TestAddrWeightChange(t *testing.T) {
	endpoints := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[1]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[2]}}},
	}
	cc, b, p0 := setupTest(t, endpoints)
	ring0 := p0.(*picker).ring

	// Update with the same addresses, it will result in a new picker, but with
	// the same ring.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: endpoints},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	var p1 balancer.Picker
	select {
	case p1 = <-cc.NewPickerCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for picker after UpdateClientConn with same addresses")
	}
	ring1 := p1.(*picker).ring
	if ring1 != ring0 {
		t.Fatalf("new picker with same address has a different ring than before, want same")
	}

	// Delete an address, should send a new Picker.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: endpoints[:2]},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	var p2 balancer.Picker
	select {
	case p2 = <-cc.NewPickerCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for picker after UpdateClientConn with different addresses")
	}
	ring2 := p2.(*picker).ring
	if ring2 == ring0 {
		t.Fatalf("new picker after removing address has the same ring as before, want different")
	}

	// Another update with the same addresses, but different weight.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Endpoints: []resolver.Endpoint{
			endpoints[0],
			weight.Set(endpoints[1], weight.EndpointInfo{Weight: 2}),
		}},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	var p3 balancer.Picker
	select {
	case p3 = <-cc.NewPickerCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for picker after UpdateClientConn with different addresses")
	}
	if p3.(*picker).ring == ring2 {
		t.Fatalf("new picker after changing address weight has the same ring as before, want different")
	}
	// With the new update, the ring must look like this:
	//   [
	//     {idx:0 endpoint: {addr: testBackendAddrStrs[0], weight: 1}},
	//     {idx:1 endpoint: {addr: testBackendAddrStrs[1], weight: 2}},
	//     {idx:2 endpoint: {addr: testBackendAddrStrs[2], weight: 1}},
	//   ].
	if len(p3.(*picker).ring.items) != 3 {
		t.Fatalf("new picker after changing address weight has %d entries, want 3", len(p3.(*picker).ring.items))
	}
	for _, i := range p3.(*picker).ring.items {
		if i.hashKey == testBackendAddrStrs[0] {
			if i.weight != 1 {
				t.Fatalf("new picker after changing address weight has weight %d for %v, want 1", i.weight, i.hashKey)
			}
		}
		if i.hashKey == testBackendAddrStrs[1] {
			if i.weight != 2 {
				t.Fatalf("new picker after changing address weight has weight %d for %v, want 2", i.weight, i.hashKey)
			}
		}
	}
}

// TestAutoConnectEndpointOnTransientFailure covers the situation when an
// endpoint fails. It verifies that a new endpoint is automatically tried
// (without a pick) when there is no endpoint already in Connecting state.
func (s) TestAutoConnectEndpointOnTransientFailure(t *testing.T) {
	wantEndpoints := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[0]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[1]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[2]}}},
		{Addresses: []resolver.Address{{Addr: testBackendAddrStrs[3]}}},
	}
	cc, _, p0 := setupTest(t, wantEndpoints)

	// ringhash won't tell SCs to connect until there is an RPC, so simulate
	// one now.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	ctx = iringhash.SetXDSRequestHash(ctx, 0)
	defer cancel()
	p0.Pick(balancer.PickInfo{Ctx: ctx})

	// The picked SubConn should be the second in the ring.
	var sc0 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc0 = <-cc.NewSubConnCh:
	}
	select {
	case <-sc0.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Turn the first subconn to transient failure. This should set the overall
	// connectivity state to CONNECTING.
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	cc.WaitForConnectivityState(ctx, connectivity.Connecting)

	// It will trigger the second subconn to connect since there is only one
	// endpoint, which is in TF.
	var sc1 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc1 = <-cc.NewSubConnCh:
	}
	select {
	case <-sc1.ConnectCh:
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("timeout waiting for Connect() from SubConn %v", sc1)
	}

	// Turn the second subconn to TF. This will set the overall state to TF.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	cc.WaitForConnectivityState(ctx, connectivity.TransientFailure)

	// It will trigger the third subconn to connect.
	var sc2 *testutils.TestSubConn
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case sc2 = <-cc.NewSubConnCh:
	}
	select {
	case <-sc2.ConnectCh:
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("timeout waiting for Connect() from SubConn %v", sc2)
	}

	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// Send the first SubConn into CONNECTING. To do this, first make it READY,
	// then CONNECTING.
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	cc.WaitForConnectivityState(ctx, connectivity.Ready)
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	// Since one endpoint is in TF and one in CONNECTING, the aggregated state
	// will be CONNECTING.
	cc.WaitForConnectivityState(ctx, connectivity.Connecting)
	p1 := <-cc.NewPickerCh
	p1.Pick(balancer.PickInfo{Ctx: ctx})
	select {
	case <-sc0.ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}
	sc0.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})

	// This will not trigger any new SubCOnns to be created, because sc0 is
	// still attempting to connect, and we only need one SubConn to connect.
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})

	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("unexpected SubConn creation: %v", sc)
	case <-sc0.ConnectCh:
		t.Fatalf("unexpected Connect() from SubConn %v", sc0)
	case <-sc1.ConnectCh:
		t.Fatalf("unexpected Connect() from SubConn %v", sc1)
	case <-sc2.ConnectCh:
		t.Fatalf("unexpected Connect() from SubConn %v", sc2)
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestAggregatedConnectivityState(t *testing.T) {
	tests := []struct {
		name           string
		endpointStates []connectivity.State
		want           connectivity.State
	}{
		{
			name:           "one ready",
			endpointStates: []connectivity.State{connectivity.Ready},
			want:           connectivity.Ready,
		},
		{
			name:           "one connecting",
			endpointStates: []connectivity.State{connectivity.Connecting},
			want:           connectivity.Connecting,
		},
		{
			name:           "one ready one transient failure",
			endpointStates: []connectivity.State{connectivity.Ready, connectivity.TransientFailure},
			want:           connectivity.Ready,
		},
		{
			name:           "one connecting one transient failure",
			endpointStates: []connectivity.State{connectivity.Connecting, connectivity.TransientFailure},
			want:           connectivity.Connecting,
		},
		{
			name:           "one connecting two transient failure",
			endpointStates: []connectivity.State{connectivity.Connecting, connectivity.TransientFailure, connectivity.TransientFailure},
			want:           connectivity.TransientFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bal := &ringhashBalancer{endpointStates: resolver.NewEndpointMap[*endpointState]()}
			for i, cs := range tt.endpointStates {
				es := &endpointState{
					state: balancer.State{ConnectivityState: cs},
				}
				ep := resolver.Endpoint{Addresses: []resolver.Address{{Addr: fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i)}}}
				bal.endpointStates.Set(ep, es)
			}
			if got := bal.aggregatedStateLocked(); got != tt.want {
				t.Errorf("recordTransition() = %v, want %v", got, tt.want)
			}
		})
	}
}

type testKeyType string

const testKey testKeyType = "grpc.lb.ringhash.testKey"

type testAttribute struct {
	content string
}

func setTestAttrAddr(addr resolver.Address, content string) resolver.Address {
	addr.BalancerAttributes = addr.BalancerAttributes.WithValue(testKey, testAttribute{content})
	return addr
}

func setTestAttrEndpoint(endpoint resolver.Endpoint, content string) resolver.Endpoint {
	endpoint.Attributes = endpoint.Attributes.WithValue(testKey, testAttribute{content})
	return endpoint
}

// TestAddrBalancerAttributesChange tests the case where the ringhash balancer
// receives a ClientConnUpdate with the same config and addresses as received in
// the previous update. Although the `BalancerAttributes` and endpoint
// attributes contents are the same, the pointers are different. This test
// verifies that subConns are not recreated in this scenario.
func (s) TestAddrBalancerAttributesChange(t *testing.T) {
	content := "test"
	addrs1 := []resolver.Address{setTestAttrAddr(resolver.Address{Addr: testBackendAddrStrs[0]}, content)}
	wantEndpoints1 := []resolver.Endpoint{
		setTestAttrEndpoint(resolver.Endpoint{Addresses: addrs1}, "content"),
	}
	cc, b, p0 := setupTest(t, wantEndpoints1)
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// The first pick should be queued, and should trigger a connection to the
	// only Endpoint which has a single address.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := p0.Pick(balancer.PickInfo{Ctx: iringhash.SetXDSRequestHash(ctx, firstHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn creation.")
	case <-cc.NewSubConnCh:
	}

	addrs2 := []resolver.Address{setTestAttrAddr(resolver.Address{Addr: testBackendAddrStrs[0]}, content)}
	wantEndpoints2 := []resolver.Endpoint{setTestAttrEndpoint(resolver.Endpoint{Addresses: addrs2}, content)}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Endpoints: wantEndpoints2},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	select {
	case <-cc.NewSubConnCh:
		t.Fatal("new subConn created for an update with the same addresses")
	case <-time.After(defaultTestShortTimeout):
	}
}
