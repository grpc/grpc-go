/*
 *
 * Copyright 2022 gRPC authors.
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

package gracefulswitch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func setup(t *testing.T) (*testutils.TestClientConn, *gracefulSwitchBalancer) {
	tcc := testutils.NewTestClientConn(t)
	return tcc, newGracefulSwitchBalancer(tcc, balancer.BuildOptions{})
}

// Basic test of first Update constructing something for current
func (s) TestFirstUpdate(t *testing.T) {
	tests := []struct {
		name              string
		childType         string
		ccs               balancer.ClientConnState
		wantSwitchToErr   error
		wantClientConnErr error
		wantCCS           balancer.ClientConnState
	}{
		{
			name:      "successful-first-update",
			childType: balancerName1,
			ccs: balancer.ClientConnState{
				// ResolverState: /*Any interesting logic here?*/,
				BalancerConfig: mockBalancer1Config{},
			},
			wantCCS: balancer.ClientConnState{
				// ResolverState: /*Any interesting logic here?*/,
				BalancerConfig: mockBalancer1Config{},
			},
		},

		// Things that trigger error condition (I feel like none of these should happen in practice):
		// Balancer has already been closed - maybe cover this in another test?
		// Wrong type inside the config
		/*{
			name:            "wrong-lb-config-type",
			childType:       "non-existent-balancer",
			wantSwitchToErr: balancer.ErrBadResolverState, // This will no longer cause an error
		},*/
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gsb := setup(t)

			builder := balancer.Get(test.childType)
			if builder == nil {
				t.Fatalf("builder.Get(%v) returned nil", test.childType)
			}

			if err := gsb.SwitchTo(builder); err != test.wantSwitchToErr { // Now you switch directly to builder
				t.Fatalf("gracefulSwitchBalancer.SwitchTo failed with error: %v", err)
			}
			if test.wantSwitchToErr != nil {
				return
			}
			if gsb.balancerCurrent == nil {
				t.Fatal("balancerCurrent was not built out when a correct SwitchTo() call should've triggered the balancer to build")
			}

			// Updating ClientConnState should forward the update exactly as is
			// to the current balancer.
			if err := gsb.UpdateClientConnState(test.ccs); err != test.wantClientConnErr {
				t.Fatalf("gracefulSwitchBalancer.UpdateClientConnState(%v) failed with error: %v", test.ccs, err)
			}
			if test.wantClientConnErr != nil { // Only error that can arise is errBalancerClosed if this gets called after balancer close
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
			defer cancel()
			if err := gsb.balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, test.ccs); err != nil {
				t.Fatalf("error in ClientConnState update: %v", err)
			}
		})
	}

}

// TestTwoBalancersSameType tests the scenario where there is a graceful switch
// load balancer setup with a current and pending Load Balancer of the same
// type. Any ClientConn update should be forwarded to the current if no pending,
// and the only the pending if the graceful switch balancer contains both a
// current and pending. The pending Load Balancer should also swap into current
// whenever it updates with a connectivity state other than CONNECTING.
func (s) TestTwoBalancersSameType(t *testing.T) {
	tcc, gsb := setup(t)
	// This will be used to update the Graceful Switch Balancer. This update
	// should simply be forwarded down to either the Current or Pending Load
	// Balancing policy.
	ccs := balancer.ClientConnState{
		BalancerConfig: mockBalancer1Config{},
	}

	builder := balancer.Get(balancerName1)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName1)
	}
	gsb.SwitchTo(builder)
	gsb.UpdateClientConnState(ccs)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, ccs); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	}

	// The current balancer reporting READY should cause this state
	// to be forwarded to the Client Conn.
	gsb.balancerCurrent.(*mockBalancer1).updateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		// Picker?
	})

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case state := <-tcc.NewStateCh:
		if state != connectivity.Ready {
			t.Fatal("wanted connectivity state ready")
		}
	}

	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		// Validate picker somehow? Add more to picker?
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}

	// An explicit call to switchTo, even if the same type, should cause the
	// balancer to build a new balancer for pending.
	builder = balancer.Get(balancerName1)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName1)
	}
	gsb.SwitchTo(builder)
	if gsb.balancerPending == nil {
		t.Fatal("balancerPending was not built out when another SwitchTo() call should've triggered the pending balancer to build")
	}

	// A Client Conn update received should be forwarded to the new pending LB
	// policy, and not the current one.
	gsb.UpdateClientConnState(ccs)
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if err := gsb.balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, ccs); err == nil {
		t.Fatal("balancerCurrent should not have received a client Conn update if there is a pending LB policy")
	}
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.balancerPending.(*mockBalancer1).waitForClientConnUpdate(ctx, ccs); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	}

	// If the pending LB reports that is CONNECTING, no update should be sent to
	// the Client Conn.
	gsb.balancerPending.(*mockBalancer1).updateState(balancer.State{
		ConnectivityState: connectivity.Connecting,
		// Picker?
	})
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	select {
	case <-tcc.NewStateCh:
		t.Fatal("Pending LB reporting CONNECTING should not forward up to the ClientConn")
	case <-ctx.Done():
	}

	// If the pending LB reports a state other than CONNECTING, the pending LB
	// is logically warmed up, and the ClientConn should be updated with the
	// State and Picker to start using the new policy. The pending LB policy
	// should also be switched into the current LB.
	gsb.balancerPending.(*mockBalancer1).updateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		// Picker?
	})

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case state := <-tcc.NewStateCh:
		if state != connectivity.Ready {
			t.Fatal("wanted connectivity state ready")
		}
		// Sends both connectivity state and picker
		// Validate state somehow
	}

	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		// Validate picker somehow - this picker should be the recent one sent
		// from UpdateState() - not the old one. This is important for later
		// ones with cached pickers for the balancer pending.
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}

	deadline := time.Now().Add(defaultTestTimeout)
	// Poll to see if pending was deleted (happens in forked goroutine).
	for {
		if gsb.balancerPending != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("balancerPending was not deleted as the pending LB reported a state other than READY, which should switch pending to current") // TODO: flakiness here
		}
		time.Sleep(time.Millisecond)
	}
}

// Test that tests Update with 1 + Update with 2 = two balancers

// Current isn't ready, Pending sending any Update should cause it to switch from current to pending ) i.e. UpdateState() call

// This is same as previous, except you don't update current with READY connectivity state...see if you want to pull out into common functionality
// Is this really a thing? I remember this idea, but where did this come from?
// func (s) TestPending

// Test that tests Update with 1 + Update with 2 = two balancers

// Current leaving READY should cause it to switch current to pending (i.e. UpdateState())

// Same as two ago, except you don't update pending at the end, you update current to a state that isn't READY

// TestCurrentLeavingReady tests the scenario where there is a current and
// pending Load Balancer setup in the Graceful Switch Load Balancer, with the
// current Load Balancer being in the state READY, and the current Load Balancer
// then transitions into a state other than READY. This should cause the Pending
// Load Balancer to swap into the current load balancer, and the Client Conn to
// be updated with the cached Pending Load Balancing state.
func (s) TestCurrentLeavingReady(t *testing.T) {
	tcc, gsb := setup(t)
	builder := balancer.Get(balancerName1)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName1)
	}
	gsb.SwitchTo(builder)

	gsb.balancerCurrent.(*mockBalancer1).updateState(balancer.State{
		ConnectivityState: connectivity.Ready,
	})

	builder = balancer.Get(balancerName2)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName2)
	}
	gsb.SwitchTo(builder)
	// Sends CONNECTING, shouldn't make it's way to ClientConn.
	gsb.balancerPending.(*mockBalancer2).updateState(balancer.State{
		ConnectivityState: connectivity.Connecting,
		// Picker here that can verify
	})

	// The current balancer leaving READY should cause the pending balancer to
	// swap to the current balancer. This swap from current to pending should
	// also update the ClientConn with the pending balancers cached state and
	// picker.
	gsb.balancerCurrent.(*mockBalancer1).updateState(balancer.State{
		ConnectivityState: connectivity.Idle,
	})

	// Sends CACHED state and picker (i.e. CONNECTING STATE + a picker you define yourself?)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case state := <-tcc.NewStateCh:
		if state != connectivity.Connecting {
			t.Fatal("wanted connectivity state CONNECTING")
		}
	}

	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for a part UpdateState call on the ClientConn")
	case picker := <-tcc.NewPickerCh:
		// Validate picker somehow - this picker should be the one sent from the
		// balancer pending earlier.
		// HOW TO VALIDATE SOMETHING UNIQUE/INTERESTING ON THE PICKER?
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}

	deadline := time.Now().Add(defaultTestTimeout)
	// Poll to see if pending was deleted (happens in forked goroutine).
	for {
		if gsb.balancerPending == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("balancerPending was not deleted as the pending LB reported a state other than READY, which should switch pending to current")
		}
		time.Sleep(time.Millisecond)
	}

	deadline = time.Now().Add(defaultTestTimeout)
	// Poll to see if current is of right type as it just got replaced by
	// MockBalancer2 (happens in forked goroutine).
	for {
		if _, ok := gsb.balancerCurrent.(*mockBalancer2); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gsb balancerCurrent should be replaced by the balancer of mockBalancer2")
		}
		time.Sleep(time.Millisecond)
	}

	// Make sure current is of right type as it just got replaced by MockBalancer2
	if _, ok := gsb.balancerCurrent.(*mockBalancer2); !ok {
		t.Fatal("gsb balancerCurrent should be replaced by the balancer of mockBalancer2")
	}

}

// Afterward...permutations of API calls

// Including stuff to do with NewSubconns and different types of Subconns - Add/Remove Subconns

// Def need to test whether Subconns updates end up at the correct balancer (i.e. scToSubBalancer works)

// What is flow of how balancers create Subconns?

// TestBalancerSubconns tests the SubConn functionality of the Graceful Switch
// Load Balancer. This tests the SubConn update flow in both directions, and
// make sure updates end up at the correct component. Also, it tests that on an
// UpdateSubConnState() call from the ClientConn, the Graceful Switch Load
// Balancer forwards it to the correct child balancer.
func (s) TestBalancerSubconns(t *testing.T) {
	tcc, gsb := setup(t)
	builder := balancer.Get(balancerName1)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName1)
	}
	gsb.SwitchTo(builder)

	builder = balancer.Get(balancerName2)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName2)
	}
	gsb.SwitchTo(builder)

	// A child balancer creating a new SubConn should eventually be forwarded to
	// the ClientConn held by the graceful switch load balancer.
	sc1, err := gsb.balancerCurrent.(*mockBalancer1).newSubConn([]resolver.Address{}, balancer.NewSubConnOptions{}) // Will eventually get back a SubConn with an identifying property id 1
	if err != nil {
		t.Fatalf("error constructing newSubConn in gsb: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an NewSubConn call on the ClientConn")
	case sc := <-tcc.NewSubConnCh:
		if !cmp.Equal(sc1, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("NewSubConn, want %v, got %v", sc1, sc)
		}
	}

	// The other child balancer creating a new SubConn should also be eventually
	// be forwarded to the ClientConn held by the graceful switch load balancer.
	sc2, err := gsb.balancerPending.(*mockBalancer2).newSubConn([]resolver.Address{}, balancer.NewSubConnOptions{}) // Will eventually get back a SubConn with an identifying property id 2
	if err != nil {
		t.Fatalf("error constructing newSubConn in gsb: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an NewSubConn call on the ClientConn")
	case sc := <-tcc.NewSubConnCh:
		if !cmp.Equal(sc2, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("NewSubConn, want %v, got %v", sc2, sc)
		}
	}
	scState := balancer.SubConnState{ConnectivityState: connectivity.Ready}
	// Updating the SubConnState for sc1 should cause the graceful switch
	// balancer to forward the Update to balancerCurrent for sc1, as that is the
	// balancer that created this SubConn.
	gsb.UpdateSubConnState(sc1, scState)

	// This update should get forwarded to balancerCurrent, as that is the LB
	// that created this SubConn.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.balancerCurrent.(*mockBalancer1).waitForSubConnUpdate(ctx, subConnWithState{sc: sc1, state: scState}); err != nil {
		t.Fatalf("error in subConn update: %v", err)
	}
	// This update should not get forwarded to balancerPending, as that is not
	// the LB that created this SubConn.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if err := gsb.balancerPending.(*mockBalancer2).waitForSubConnUpdate(ctx, subConnWithState{sc: sc1, state: scState}); err == nil {
		t.Fatalf("balancerPending should not have received a subconn update for sc1")
	}

	// Updating the SubConnState for sc2 should cause the graceful switch
	// balancer to forward the Update to balancerPending for sc2, as that is the
	// balancer that created this SubConn.
	gsb.UpdateSubConnState(sc2, scState)

	// This update should get forwarded to balancerPending, as that is the LB
	// that created this SubConn.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.balancerPending.(*mockBalancer2).waitForSubConnUpdate(ctx, subConnWithState{sc: sc2, state: scState}); err != nil {
		t.Fatalf("error in subConn update: %v", err)
	}

	// This update should not get forwarded to balancerCurrent, as that is not
	// the LB that created this SubConn.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if err := gsb.balancerCurrent.(*mockBalancer1).waitForSubConnUpdate(ctx, subConnWithState{sc: sc2, state: scState}); err == nil {
		t.Fatalf("balancerCurrent should not have received a subconn update for sc2")
	}

	// Updating the addresses for both SubConns and removing both SubConns
	// should get forwarded to the ClientConn.

	// Updating the addresses for sc1 should get forwarded to the ClientConn.
	gsb.balancerCurrent.(*mockBalancer1).updateAddresses(sc1, []resolver.Address{})

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case <-tcc.UpdateAddressesAddrsCh:
	}

	// Updating the addresses for sc2 should also get forwarded to the ClientConn.
	gsb.balancerPending.(*mockBalancer2).updateAddresses(sc2, []resolver.Address{})
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case <-tcc.UpdateAddressesAddrsCh:
	}

	// Current Balancer removing sc1 should get forwarded to the ClientConn.
	gsb.balancerCurrent.(*mockBalancer1).removeSubConn(sc1)
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case sc := <-tcc.RemoveSubConnCh:
		if !cmp.Equal(sc1, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("RemoveSubConn, want %v, got %v", sc1, sc)
		}
	}
	// Pending Balancer removing sc2 should get forwarded to the ClientConn.
	gsb.balancerPending.(*mockBalancer2).removeSubConn(sc2)
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case sc := <-tcc.RemoveSubConnCh:
		if !cmp.Equal(sc2, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			t.Fatalf("RemoveSubConn, want %v, got %v", sc2, sc)
		}
	}
}

// Test that tests Close(), downstream effects of closing SubConns, and also guarding everything else

// TestBalancerClose tests the Graceful Switch Balancer's Close() functionality.
// From the Close() call, the Graceful Switch Balancer should remove any created Subconns and
// Close() the Current and Pending Load Balancers. This Close() call should also cause any other
// events (calls to entrance functions) to be no-ops.
func (s) TestBalancerClose(t *testing.T) {
	// Setup gsb balancer with current, pending, and one created SubConn on each
	tcc, gsb := setup(t)
	builder := balancer.Get(balancerName1)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName1)
	}
	gsb.SwitchTo(builder)

	builder = balancer.Get(balancerName2)
	if builder == nil {
		t.Fatalf("builder.Get(%v) returned nil", balancerName2)
	}
	gsb.SwitchTo(builder)

	sc1, err := gsb.balancerCurrent.(*mockBalancer1).newSubConn([]resolver.Address{}, balancer.NewSubConnOptions{}) // Will eventually get back a SubConn with an identifying property id 1
	if err != nil {
		t.Fatalf("error constructing newSubConn in gsb: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an NewSubConn call on the ClientConn")
	case <-tcc.NewSubConnCh:
	}

	sc2, err := gsb.balancerPending.(*mockBalancer2).newSubConn([]resolver.Address{}, balancer.NewSubConnOptions{}) // Will eventually get back a SubConn with an identifying property id 2
	if err != nil {
		t.Fatalf("error constructing newSubConn in gsb: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an NewSubConn call on the ClientConn")
	case <-tcc.NewSubConnCh:
	}

	currBal := gsb.balancerCurrent.(*mockBalancer1)
	pendBal := gsb.balancerPending.(*mockBalancer2)

	// Closing the Graceful Switch Load Balancer should lead to removing any
	// created SubConns, and closing both the current and pending Load Balancer.
	gsb.Close()

	// Downstream effects of Close()
	// Remove() any created subconns (Have 1 subconn one ach) (mock subconns and mock pickers?)

	// ASSERT that the Client Conn received a closed call for

	// The order of Subconns the Graceful Switch Balancer tells the Client Conn
	// to remove is non deterministic, as it is stored in a map. However, the
	// first SubConn removed should be either sc1 or sc2.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case sc := <-tcc.RemoveSubConnCh:
		if !cmp.Equal(sc1, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			if !cmp.Equal(sc2, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
				t.Fatalf("RemoveSubConn, want either %v or %v, got %v", sc1, sc2, sc)
			}
		}
	}

	// The Graceful Switch Balancer should then tell the Client Conn to remove
	// the other SubConn.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout while waiting for an UpdateAddresses call on the ClientConn")
	case sc := <-tcc.RemoveSubConnCh:
		if !cmp.Equal(sc1, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
			if !cmp.Equal(sc2, sc, cmp.AllowUnexported(testutils.TestSubConn{})) {
				t.Fatalf("RemoveSubConn, want either %v or %v, got %v", sc1, sc2, sc)
			}
		}
	}

	// The current balancer should get closed as a result of the graceful switch balancer being closed.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := currBal.waitForClose(ctx); err != nil {
		t.Fatalf("error in close: %v", err)
	}
	// The pending balancer should also get closed as a result of the graceful switch balancer being closed.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pendBal.waitForClose(ctx); err != nil {
		t.Fatalf("error in close: %v", err)
	}

	// Also, once this event happens, trying to do anything else on both codepaths
	// should be a no-op.

	// ASSERT SwitchTo() returns an error

	// ASSERT UpdateClientConnState() returns err bal closed

	// ASSERT ResolverError() doesn't send down an error to a balancer (this can
	// be done because balancer's close just sends on channel so still exists),
	// if not both cleared (set to nil) one would receive the resolver error.

}

func (s) TestResolverError(t *testing.T) {
	// Get test to current but no pending

	// If just current send to current

	// If both filed send to pending
}

// Is there a way to test race conditions (i.e. concurrently call UpdateState() and UpdateClientConnState())?

// When current switches to a state other than READY, the pending state sent up to the ClientConn needs to be of the new type

// Also ASSERT that the ClientConn received closeSubConn calls

// Works normally in current system

// Mock balancer.Balancer here (the current or pending balancer)

// register it, also have an unexported function that allows it to ping up to balancer.ClientConn (updateState() and newSubConn() eventually)
const balancerName1 = "mock_balancer_1"
const balancerName2 = "mock_balancer_2"

func init() {
	balancer.Register(bb1{})
	balancer.Register(bb2{})
}

type bb1 struct{}

func (bb1) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &mockBalancer1{
		ccsCh:         testutils.NewChannel(),
		scStateCh:     testutils.NewChannel(),
		resolverErrCh: testutils.NewChannel(),
		closeCh:       testutils.NewChannel(),
		cc:            cc,
	}
}

func (bb1) Name() string {
	return balancerName1
}

type nonExistentConfig struct {
	serviceconfig.LoadBalancingConfig
}

type mockBalancer1Config struct {
	serviceconfig.LoadBalancingConfig
	/*Anything interesting we want to verify here?*/
}

// mockBalancer is a fake balancer used to verify different actions from
// the gracefulswitch. It contains a bunch of channels to signal different events
// to the test.
type mockBalancer1 struct {
	// ccsCh is a channel used to signal the receipt of a ClientConn update.
	ccsCh *testutils.Channel
	// scStateCh is a channel used to signal the receipt of a SubConn update.
	scStateCh *testutils.Channel
	// resolverErrCh is a channel used to signal a resolver error.
	resolverErrCh *testutils.Channel
	// closeCh is a channel used to signal the closing of this balancer.
	closeCh *testutils.Channel
	// Hold onto Client Conn wrapper to communicate with it
	cc balancer.ClientConn
}

type subConnWithState struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func (mb1 *mockBalancer1) UpdateClientConnState(ccs balancer.ClientConnState) error {
	// Need to verify this call...use a channel?...all of these will need verification
	mb1.ccsCh.Send(ccs)
	return nil
}

func (mb1 *mockBalancer1) ResolverError(err error) {
	mb1.resolverErrCh.Send(err)
}

func (mb1 *mockBalancer1) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	mb1.scStateCh.Send(subConnWithState{sc: sc, state: state})
}

func (mb1 *mockBalancer1) Close() {
	mb1.closeCh.Send(struct{}{})
}

// waitForClientConnUpdate verifies if the mockBalancer1 receives the
// provided ClientConnState within a reasonable amount of time.
func (mb1 *mockBalancer1) waitForClientConnUpdate(ctx context.Context, wantCCS balancer.ClientConnState) error {
	ccs, err := mb1.ccsCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotCCS := ccs.(balancer.ClientConnState)
	/*if xdsclient.FromResolverState(gotCCS.ResolverState) == nil { // Do we need this? It feels like it should be agnostic
		return fmt.Errorf("want resolver state with XDSClient attached, got one without")
	}*/
	if diff := cmp.Diff(gotCCS, wantCCS, cmpopts.IgnoreFields(resolver.State{}, "Attributes")); diff != "" {
		return fmt.Errorf("received unexpected ClientConnState, diff (-got +want): %v", diff)
	}
	return nil
}

// waitForSubConnUpdate verifies if the mockBalancer1 receives the provided
// SubConn update before the context expires.
func (mb1 *mockBalancer1) waitForSubConnUpdate(ctx context.Context, wantSCS subConnWithState) error {
	scs, err := mb1.scStateCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotSCS := scs.(subConnWithState)
	if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{}, testutils.TestSubConn{})) {
		return fmt.Errorf("received SubConnState: %+v, want %+v", gotSCS, wantSCS)
	}
	return nil
}

// waitForResolverError verifies if the mockBalancer1 receives the provided
// resolver error before the context expires.
func (mb1 *mockBalancer1) waitForResolverError(ctx context.Context, wantErr error) error {
	gotErr, err := mb1.resolverErrCh.Receive(ctx)
	if err != nil {
		return err
	}
	if gotErr != wantErr {
		return fmt.Errorf("received resolver error: %v, want %v", gotErr, wantErr)
	}
	return nil
}

// waitForClose verifies that the mockBalancer1 is closed before the context
// expires.
func (mb1 *mockBalancer1) waitForClose(ctx context.Context) error {
	if _, err := mb1.closeCh.Receive(ctx); err != nil {
		return err
	}
	return nil
}

// Needs some way of calling Client Conn UpdateState() upward and also what specific picker
// the picker came from (i.e. mockBalancer1 or mockBalancer2)

// method to call to updateState()

func (mb1 *mockBalancer1) updateState(state balancer.State) {
	mb1.cc.UpdateState(state)
}

func (mb1 *mockBalancer1) newSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return mb1.cc.NewSubConn(addrs, opts) // Mock sub conn
}

func (mb1 *mockBalancer1) updateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	mb1.cc.UpdateAddresses(sc, addrs)
}

func (mb1 *mockBalancer1) removeSubConn(sc balancer.SubConn) {
	mb1.cc.RemoveSubConn(sc)
}

// it's determined by config type, so need a second balancer here
type bb2 struct{}

func (bb2) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &mockBalancer2{
		ccsCh:         testutils.NewChannel(),
		scStateCh:     testutils.NewChannel(),
		resolverErrCh: testutils.NewChannel(),
		closeCh:       testutils.NewChannel(),
		cc:            cc,
	}
}

func (bb2) Name() string {
	return balancerName2
}

type mockBalancer2Config struct {
	serviceconfig.LoadBalancingConfig
	/*Anything interesting we want to verify here?*/
}

// mockBalancer is a fake balancer used to verify different actions from
// the gracefulswitch. It contains a bunch of channels to signal different events
// to the test.
type mockBalancer2 struct {
	// ccsCh is a channel used to signal the receipt of a ClientConn update.
	ccsCh *testutils.Channel
	// scStateCh is a channel used to signal the receipt of a SubConn update.
	scStateCh *testutils.Channel
	// resolverErrCh is a channel used to signal a resolver error.
	resolverErrCh *testutils.Channel
	// closeCh is a channel used to signal the closing of this balancer.
	closeCh *testutils.Channel
	// Hold onto Client Conn wrapper to communicate with it
	cc balancer.ClientConn
}

func (mb2 *mockBalancer2) UpdateClientConnState(ccs balancer.ClientConnState) error {
	// Need to verify this call...use a channel?...all of these will need verification
	mb2.ccsCh.Send(ccs)
	return nil
}

func (mb2 *mockBalancer2) ResolverError(err error) {
	mb2.resolverErrCh.Send(err)
}

func (mb2 *mockBalancer2) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	mb2.scStateCh.Send(subConnWithState{sc: sc, state: state})
}

func (mb2 *mockBalancer2) Close() {
	mb2.closeCh.Send(struct{}{})
}

// waitForClientConnUpdate verifies if the mockBalancer1 receives the
// provided ClientConnState within a reasonable amount of time.
func (mb2 *mockBalancer2) waitForClientConnUpdate(ctx context.Context, wantCCS balancer.ClientConnState) error {
	ccs, err := mb2.ccsCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotCCS := ccs.(balancer.ClientConnState)
	/*if xdsclient.FromResolverState(gotCCS.ResolverState) == nil { // Do we need this? It feels like it should be agnostic
		return fmt.Errorf("want resolver state with XDSClient attached, got one without")
	}*/
	if diff := cmp.Diff(gotCCS, wantCCS, cmpopts.IgnoreFields(resolver.State{}, "Attributes")); diff != "" {
		return fmt.Errorf("received unexpected ClientConnState, diff (-got +want): %v", diff)
	}
	return nil
}

// waitForSubConnUpdate verifies if the mockBalancer1 receives the provided
// SubConn update before the context expires.
func (mb2 *mockBalancer2) waitForSubConnUpdate(ctx context.Context, wantSCS subConnWithState) error {
	scs, err := mb2.scStateCh.Receive(ctx)
	if err != nil {
		return err
	}
	gotSCS := scs.(subConnWithState)
	if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{}, testutils.TestSubConn{})) {
		return fmt.Errorf("received SubConnState: %+v, want %+v", gotSCS, wantSCS)
	}
	return nil
}

// waitForResolverError verifies if the mockBalancer1 receives the provided
// resolver error before the context expires.
func (mb2 *mockBalancer2) waitForResolverError(ctx context.Context, wantErr error) error {
	gotErr, err := mb2.resolverErrCh.Receive(ctx)
	if err != nil {
		return err
	}
	if gotErr != wantErr {
		return fmt.Errorf("received resolver error: %v, want %v", gotErr, wantErr)
	}
	return nil
}

// waitForClose verifies that the mockBalancer1 is closed before the context
// expires.
func (mb2 *mockBalancer2) waitForClose(ctx context.Context) error {
	if _, err := mb2.closeCh.Receive(ctx); err != nil {
		return err
	}
	return nil
}

// Needs some way of calling Client Conn UpdateState() upward and also what specific picker
// the picker came from (i.e. mockBalancer1 or mockBalancer2)

// method to call to updateState()

func (mb2 *mockBalancer2) updateState(state balancer.State) {
	mb2.cc.UpdateState(state)
}

func (mb2 *mockBalancer2) newSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return mb2.cc.NewSubConn(addrs, opts) // Mock sub conn
}

func (mb2 *mockBalancer2) updateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	mb2.cc.UpdateAddresses(sc, addrs)
}

func (mb2 *mockBalancer2) removeSubConn(sc balancer.SubConn) {
	mb2.cc.RemoveSubConn(sc)
}

// Order of what to do:

// 1. Fix Menghan's PR comments wrt implementation, get three tests to continue to pass
// 2. Add the tests that are give mes and get them to run, maybe factor the code out into reusable segments - setup() function
// 3. Add Subconn tests/Close tests
// 4. Think of any more tests you need to add that are special cases?

// Menghan's pass order of how to tackle it:
// 1. Simple, no op comments

// Any dependencies for the three big pieces of functionality below?
// CCW checking if from pending or current (how to read balancers without race condition, will this induce deadlock if it reads current or pending and also updates within mutex?)
// check for current and pending balancer - helper function, and also error message (balancer name + pointer)

// What is default initialization of pending LB State (Easwar gave a suggestion I agreed with here)...also this needs to not swap if no pending (look more into this)
// Easwar: A lot of nits in SwitchTo (pending LB as well) (orthogonal to Menghan's comments) merge this

// Race condition (seems most complicated - probably do this at the end as well)

// TEST CASE: (Menghan's comment specifically) - Current, Pending...new pending that replaces this pending

// Clear pending state...remove subconns

// Need a test case for that race condition...make sure it won't update a closed balancer in UpdateSubConnState() - for this one,
// the balancer needs to call back on an UpdateSubConnState call back into update state which causes itself to get closed/cleared.

// Also need a test where you test an outdated balancer that tries to Update...is there even a way to induce this?
