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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/testutils"
)

var (
	cmpOpts = cmp.Options{
		cmp.AllowUnexported(testutils.TestSubConn{}, ringEntry{}, subConn{}),
		cmpopts.IgnoreFields(subConn{}, "mu"),
	}
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond

	testBackendAddrsCount = 12
)

var (
	testBackendAddrStrs []string
	testConfig          = &LBConfig{MinRingSize: 1, MaxRingSize: 10}
)

func init() {
	for i := 0; i < testBackendAddrsCount; i++ {
		testBackendAddrStrs = append(testBackendAddrStrs, fmt.Sprintf("%d.%d.%d.%d:%d", i, i, i, i, i))
	}
}

func ctxWithHash(h uint64) context.Context {
	return SetRequestHash(context.Background(), h)
}

// setupTest creates the balancer, and does an initial sanity check.
func setupTest(t *testing.T, addrs []resolver.Address) (*testutils.TestClientConn, balancer.Balancer, balancer.Picker) {
	t.Helper()
	cc := testutils.NewTestClientConn(t)
	builder := balancer.Get(Name)
	b := builder.Build(cc, balancer.BuildOptions{})
	if b == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", Name)
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Addresses: addrs},
		BalancerConfig: testConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}

	for _, addr := range addrs {
		addr1 := <-cc.NewSubConnAddrsCh
		if want := []resolver.Address{addr}; !cmp.Equal(addr1, want, cmp.AllowUnexported(attributes.Attributes{})) {
			t.Fatalf("got unexpected new subconn addrs: %v", cmp.Diff(addr1, want, cmp.AllowUnexported(attributes.Attributes{})))
		}
		sc1 := <-cc.NewSubConnCh
		// All the SubConns start in Idle, and should not Connect().
		select {
		case <-sc1.(*testutils.TestSubConn).ConnectCh:
			t.Errorf("unexpected Connect() from SubConn %v", sc1)
		case <-time.After(defaultTestShortTimeout):
		}
	}

	// Should also have a picker, with all SubConns in Idle.
	p1 := <-cc.NewPickerCh
	return cc, b, p1
}

func TestOneSubConn(t *testing.T) {
	wantAddr1 := resolver.Address{Addr: testBackendAddrStrs[0]}
	cc, b, p0 := setupTest(t, []resolver.Address{wantAddr1})
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// firstHash-1 will pick the first (and only) SubConn from the ring.
	testHash := firstHash - 1
	// The first pick should be queued, and should trigger Connect() on the only
	// SubConn.
	if _, err := p0.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc0 := ring0.items[0].sc.sc
	select {
	case <-sc0.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Send state updates to Ready.
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Test pick with one backend.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// TestThreeBackendsAffinity covers that there are 3 SubConns, RPCs with the
// same hash always pick the same SubConn. When the one picked is down, another
// one will be picked.
func TestThreeSubConnsAffinity(t *testing.T) {
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0]},
		{Addr: testBackendAddrStrs[1]},
		{Addr: testBackendAddrStrs[2]},
	}
	cc, b, p0 := setupTest(t, wantAddrs)
	// This test doesn't update addresses, so this ring will be used by all the
	// pickers.
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// firstHash+1 will pick the second SubConn from the ring.
	testHash := firstHash + 1
	// The first pick should be queued, and should trigger Connect() on the only
	// SubConn.
	if _, err := p0.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	// The picked SubConn should be the second in the ring.
	sc0 := ring0.items[1].sc.sc
	select {
	case <-sc0.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Send state updates to Ready.
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}

	// Turn down the subConn in use.
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	p2 := <-cc.NewPickerCh
	// Pick with the same hash should be queued, because the SubConn after the
	// first picked is Idle.
	if _, err := p2.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}

	// The third SubConn in the ring should connect.
	sc1 := ring0.items[2].sc.sc
	select {
	case <-sc1.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc1)
	}

	// Send state updates to Ready.
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	// New picks should all return this SubConn.
	p3 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p3.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}

	// Now, after backoff, the first picked SubConn will turn Idle.
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	// The picks above should have queued Connect() for the first picked
	// SubConn, so this Idle state change will trigger a Connect().
	select {
	case <-sc0.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// After the first picked SubConn turn Ready, new picks should return it
	// again (even though the second picked SubConn is also Ready).
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})
	p4 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p4.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

// TestThreeBackendsAffinity covers that there are 3 SubConns, RPCs with the
// same hash always pick the same SubConn. Then try different hash to pick
// another backend, and verify the first hash still picks the first backend.
func TestThreeSubConnsAffinityMultiple(t *testing.T) {
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0]},
		{Addr: testBackendAddrStrs[1]},
		{Addr: testBackendAddrStrs[2]},
	}
	cc, b, p0 := setupTest(t, wantAddrs)
	// This test doesn't update addresses, so this ring will be used by all the
	// pickers.
	ring0 := p0.(*picker).ring

	firstHash := ring0.items[0].hash
	// firstHash+1 will pick the second SubConn from the ring.
	testHash := firstHash + 1
	// The first pick should be queued, and should trigger Connect() on the only
	// SubConn.
	if _, err := p0.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc0 := ring0.items[1].sc.sc
	select {
	case <-sc0.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// Send state updates to Ready.
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// First hash should always pick sc0.
	p1 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p1.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}

	secondHash := ring0.items[1].hash
	// secondHash+1 will pick the third SubConn from the ring.
	testHash2 := secondHash + 1
	if _, err := p0.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash2)}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("first pick returned err %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc1 := ring0.items[2].sc.sc
	select {
	case <-sc1.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc1)
	}
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// With the new generated picker, hash2 always picks sc1.
	p2 := <-cc.NewPickerCh
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash2)})
		if !cmp.Equal(gotSCSt.SubConn, sc1, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc1)
		}
	}
	// But the first hash still picks sc0.
	for i := 0; i < 5; i++ {
		gotSCSt, _ := p2.Pick(balancer.PickInfo{Ctx: ctxWithHash(testHash)})
		if !cmp.Equal(gotSCSt.SubConn, sc0, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("picker.Pick, got %v, want SubConn=%v", gotSCSt, sc0)
		}
	}
}

func TestAddrWeightChange(t *testing.T) {
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0]},
		{Addr: testBackendAddrStrs[1]},
		{Addr: testBackendAddrStrs[2]},
	}
	cc, b, p0 := setupTest(t, wantAddrs)
	ring0 := p0.(*picker).ring

	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolver.State{Addresses: wantAddrs},
		BalancerConfig: nil,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	select {
	case <-cc.NewPickerCh:
		t.Fatalf("unexpected picker after UpdateClientConn with the same addresses")
	case <-time.After(defaultTestShortTimeout):
	}

	// Delete an address, should send a new Picker.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			{Addr: testBackendAddrStrs[0]},
			{Addr: testBackendAddrStrs[1]},
		}},
		BalancerConfig: nil,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	var p1 balancer.Picker
	select {
	case p1 = <-cc.NewPickerCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for picker after UpdateClientConn with different addresses")
	}
	ring1 := p1.(*picker).ring
	if ring1 == ring0 {
		t.Fatalf("new picker after removing address has the same ring as before, want different")
	}

	// Another update with the same addresses, but different weight.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{
			{Addr: testBackendAddrStrs[0]},
			weightedroundrobin.SetAddrInfo(
				resolver.Address{Addr: testBackendAddrStrs[1]},
				weightedroundrobin.AddrInfo{Weight: 2}),
		}},
		BalancerConfig: nil,
	}); err != nil {
		t.Fatalf("UpdateClientConnState returned err: %v", err)
	}
	var p2 balancer.Picker
	select {
	case p2 = <-cc.NewPickerCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timeout waiting for picker after UpdateClientConn with different addresses")
	}
	if p2.(*picker).ring == ring1 {
		t.Fatalf("new picker after changing address weight has the same ring as before, want different")
	}
}

// TestSubConnToConnectWhenOverallTransientFailure covers the situation when the
// overall state is TransientFailure, the SubConns turning Idle will be
// triggered to Connect(). But not when the overall state is not
// TransientFailure.
func TestSubConnToConnectWhenOverallTransientFailure(t *testing.T) {
	wantAddrs := []resolver.Address{
		{Addr: testBackendAddrStrs[0]},
		{Addr: testBackendAddrStrs[1]},
		{Addr: testBackendAddrStrs[2]},
	}
	_, b, p0 := setupTest(t, wantAddrs)
	ring0 := p0.(*picker).ring

	// Turn all SubConns to TransientFailure.
	for _, it := range ring0.items {
		b.UpdateSubConnState(it.sc.sc, balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	}

	// The next one turning Idle should Connect().
	sc0 := ring0.items[0].sc.sc
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	select {
	case <-sc0.(*testutils.TestSubConn).ConnectCh:
	case <-time.After(defaultTestTimeout):
		t.Errorf("timeout waiting for Connect() from SubConn %v", sc0)
	}

	// If this SubConn is ready. Other SubConns turning Idle will not Connect().
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	b.UpdateSubConnState(sc0, balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// The third SubConn in the ring should connect.
	sc1 := ring0.items[1].sc.sc
	b.UpdateSubConnState(sc1, balancer.SubConnState{ConnectivityState: connectivity.Idle})
	select {
	case <-sc1.(*testutils.TestSubConn).ConnectCh:
		t.Errorf("unexpected Connect() from SubConn %v", sc1)
	case <-time.After(defaultTestShortTimeout):
	}
}

func TestConnectivityStateEvaluatorRecordTransition(t *testing.T) {
	tests := []struct {
		name     string
		from, to []connectivity.State
		want     connectivity.State
	}{
		{
			name: "one ready",
			from: []connectivity.State{connectivity.Idle},
			to:   []connectivity.State{connectivity.Ready},
			want: connectivity.Ready,
		},
		{
			name: "one connecting",
			from: []connectivity.State{connectivity.Idle},
			to:   []connectivity.State{connectivity.Connecting},
			want: connectivity.Connecting,
		},
		{
			name: "one ready one transient failure",
			from: []connectivity.State{connectivity.Idle, connectivity.Idle},
			to:   []connectivity.State{connectivity.Ready, connectivity.TransientFailure},
			want: connectivity.Ready,
		},
		{
			name: "one connecting one transient failure",
			from: []connectivity.State{connectivity.Idle, connectivity.Idle},
			to:   []connectivity.State{connectivity.Connecting, connectivity.TransientFailure},
			want: connectivity.Connecting,
		},
		{
			name: "one connecting two transient failure",
			from: []connectivity.State{connectivity.Idle, connectivity.Idle, connectivity.Idle},
			to:   []connectivity.State{connectivity.Connecting, connectivity.TransientFailure, connectivity.TransientFailure},
			want: connectivity.TransientFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cse := &connectivityStateEvaluator{}
			var got connectivity.State
			for i, fff := range tt.from {
				ttt := tt.to[i]
				got = cse.recordTransition(fff, ttt)
			}
			if got != tt.want {
				t.Errorf("recordTransition() = %v, want %v", got, tt.want)
			}
		})
	}
}
