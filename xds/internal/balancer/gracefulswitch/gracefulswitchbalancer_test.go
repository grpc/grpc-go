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
	"google.golang.org/grpc/connectivity"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer"
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


// cdsbalancer, balancergroup, rlsBalancer for examples

// What is main functionality?

// state: (what will I need to verify at certain situations?)

/*
type gracefulSwitchBalancer struct {
	bOpts          balancer.BuildOptions
	cc balancer.ClientConn

	outgoingMu sync.Mutex
	recentConfig *lbConfig
	balancerCurrent balancer.Balancer
	balancerPending balancer.Balancer

	incomingMu sync.Mutex
	scToSubBalancer map[balancer.SubConn]balancer.Balancer
	pendingState balancer.State

	closed *grpcsync.Event
}
*/

// Entrance functions into gracefulSwitchBalancer:

// UpdateClientConnState(state balancer.ClientConnState) error

// ResolverError(err error)

// UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState)

// Close()


// Causes balancerCurrent and balancerPending to cycle in and out of permutations

// Will communicate (ping) balancerCurrent/balancerPending and also ClientConn






// Basic test of first Update constructing something for current
func (s) TestFirstUpdate(t *testing.T) {
	tests := []struct {
		name string
		ccs balancer.ClientConnState
		wantErr error
		wantCCS balancer.ClientConnState
	}{
		{
			name: "successful-first-update",
			ccs: balancer.ClientConnState{
				// ResolverState: /*Any interesting logic heree?*/,
				BalancerConfig: &lbConfig{ChildBalancerType: balancerName1, Config: mockBalancer1Config{}},
			},
			// Causes to build
			// And receive ClientConnState
			wantCCS: balancer.ClientConnState{
				// ResolverState: /*Any interesting logic heree?*/, // Same resolver state as previous
				BalancerConfig: mockBalancer1Config{},
			},
		},

		// Things that trigger error condition (I feel like none of these should happen in practice):
		// Balancer has already been closed - maybe cover this in another test?
		// Wrong config itself
		{
			name: "wrong-lb-config",
			ccs: balancer.ClientConnState{BalancerConfig: nil/*!lbConfig - whether nil or a different config...will this cause a nil dereference?*/},
			wantErr: balancer.ErrBadResolverState,
		},
		// Wrong type inside the config
		{
			name: "wrong-lb-config-type",
			ccs: balancer.ClientConnState{
				// ResolverState: /*Any interesting logic here?*/,
				BalancerConfig: &lbConfig{ChildBalancerType: "non-existent-balancer", Config: nonExistentConfig{}/*Any interesting logic here?*/},
			},
			wantErr: balancer.ErrBadResolverState,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := balancer.Get(gracefulSwitchBalancerName)
			if builder == nil {
				t.Fatalf("balancer.Get(%q) returned nil", gracefulSwitchBalancerName)
			}
			tcc := testutils.NewTestClientConn(t)
			gsb := builder.Build(tcc, balancer.BuildOptions{})
			if err := gsb.UpdateClientConnState(test.ccs/*Client Conn State here that triggers a balancer down low to build or error, test.ccs*/); err != test.wantErr { // Maybe switch to containing string?
				t.Fatalf("gracefulSwitchBalancer.UpdateClientConnState failed with error: %v", err) // Make this more descriptive?
			}
			if test.wantErr != nil {
				return
			}



			// If successful, verify one of the two balancers down below (config will
			// specify right type) gets the right Update....ASSERT the balancer builds and calls UpdateClientConnState()...that should be the scope of this test

			// ASSERT that the balancer builds (i.e. gsb.balancerCurrent != nil)...i.e. verifying state how is it done elsewhere?
			// typecast gsb into graceful switch? and just read it?

			if gsb.(*gracefulSwitchBalancer).balancerCurrent == nil { // Is this the right way to do it? Or should we do dependency injection like NewTimer() and cds tests? Is there a better way to do this in general?
				t.Fatal("balancerCurrent was not built out when a correct update should've triggered the balancer to build")
			}
			// Would be overriding balancer.Get() in balancer
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
			defer cancel()
			// and that it gets an UpdateClientConnState()
			// Receive from a channel? Typecast balancerCurrent into balancer declared below?
			// ClientConnState is same Resolver State but lbCfg is looked into .Config
			if err := gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, test.wantCCS); err != nil {
				t.Fatalf("error in ClientConnState update: %v", err)
			}
		})
	}


}

// GET THIS TO COMPILE AND WORK :D


// Test that tests Update with 1 + Update with 1 = UpdateClientConnState twice

// UpdateState() causes it to forward to ClientConn...so mock balancer needs way
// of pinging UpdateState() and NewSubConn()...flow goes mock balancer (->) ccw ->
// gracefulswitch -> grpc.ClientConn (needs to verify gets UpdateState call with state + picker)

func (s) TestTwoUpdatesSameBalancer(t *testing.T) {
	// Set up balancer (maybe pull this out into a utility function)
	builder := balancer.Get(gracefulSwitchBalancerName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", gracefulSwitchBalancerName)
	}
	tcc := testutils.NewTestClientConn(t)
	gsb := builder.Build(tcc, balancer.BuildOptions{})
	ccs := balancer.ClientConnState{
		BalancerConfig: &lbConfig{ChildBalancerType: balancerName1, Config: mockBalancer1Config{}},
	}
	gsb.UpdateClientConnState(ccs)
	wantCCS := balancer.ClientConnState{
		BalancerConfig: mockBalancer1Config{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if err := gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, wantCCS); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	} // Like in previous projects, I don't think we need these validations?

	// gsb.UpdateClientConnState again with same type config
	ccs = balancer.ClientConnState{
		// There needs to be some way of differentiating these...maybe put state in the config that determines?
		BalancerConfig: &lbConfig{ChildBalancerType: balancerName1, Config: mockBalancer1Config{}},
	}
	gsb.UpdateClientConnState(ccs)

	// Downstream effects of UpdateClientConnState:

	// A new balancer does NOT get created and put into pending/updated
	// ASSERT balancer.pending != nil
	if gsb.(*gracefulSwitchBalancer).balancerPending != nil {
		t.Fatalf("An UpdateClientConnState() specifying the same lb config type should not lead to the creation of a pending balancer")
	}

	// Another update - the new update sent to same balancer (maybe have a new config to check)
	wantCCS = balancer.ClientConnState{
		BalancerConfig: mockBalancer1Config{},
	}
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, wantCCS); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	}

	// This child balancer calls updateState and updates state...this should cause that updateState
	// call to make it's way all the way to the ClientConn
	gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).updateState(balancer.State{
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
		// Validate picker somehow
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}

	// tcc. // verify that this received the state + picker from mockBalancer1 - how is this verified in cds test?

}







// Test that tests Update with 1 + Update with 2 = two balancers

// Current says it's READY, then Pending being READY should cause it to switch current to pending (i.e. UpdateState())
func (s) TestTwoUpdatesDifferentBalancer(t *testing.T) {
	// Set up balancer (maybe pull this out into a utility function)
	builder := balancer.Get(gracefulSwitchBalancerName)
	if builder == nil {
		t.Fatalf("balancer.Get(%q) returned nil", gracefulSwitchBalancerName)
	}
	tcc := testutils.NewTestClientConn(t)
	gsb := builder.Build(tcc, balancer.BuildOptions{})
	ccs := balancer.ClientConnState{
		BalancerConfig: &lbConfig{ChildBalancerType: balancerName1, Config: mockBalancer1Config{}},
	}
	gsb.UpdateClientConnState(ccs)
	wantCCS := balancer.ClientConnState{
		BalancerConfig: mockBalancer1Config{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer cancel()
	if err := gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).waitForClientConnUpdate(ctx, wantCCS); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	} // Like in previous projects, I don't think we need these validations?

	// gsb.UpdateClientConnState again with same type config
	ccs = balancer.ClientConnState{
		// There needs to be some way of differentiating these...maybe put state in the config that determines?
		BalancerConfig: &lbConfig{ChildBalancerType: balancerName2, Config: mockBalancer2Config{}},
	}
	gsb.UpdateClientConnState(ccs)

	// Downstream effects of UpdateClientConnState (with a new balancer entirely):




	// A new balancer gets created and put into pending/updated
	// ASSERT balancer.pending == nil
	if gsb.(*gracefulSwitchBalancer).balancerPending == nil {
		t.Fatalf("An UpdateClientConnState() specifying a different lb config type should lead to the creation of a pending balancer")
	}

	// Another update - the new update sent to pending balancer
	wantCCS = balancer.ClientConnState{
		BalancerConfig: mockBalancer2Config{},
	}
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := gsb.(*gracefulSwitchBalancer).balancerPending.(*mockBalancer2).waitForClientConnUpdate(ctx, wantCCS); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	}

	// This child balancer calls updateState and updates state...this should cause that updateState
	// call to make it's way all the way to the ClientConn
	gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1).updateState(balancer.State{
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
		// Validate picker somehow
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}

	balCurrentBeforeUpdate := gsb.(*gracefulSwitchBalancer).balancerCurrent.(*mockBalancer1)

	// Then pending says it's ready, this should move pending into current, and update client conn with pending's state
	gsb.(*gracefulSwitchBalancer).balancerPending.(*mockBalancer2).updateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		// Picker
	})

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Should also Close() the current balancer

	// verify Close() balancer here
	if err := balCurrentBeforeUpdate.waitForClose(ctx); err != nil {
		t.Fatalf("error in ClientConnState update: %v", err)
	}



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
		// Validate picker somehow
		if picker != nil {
			t.Fatal("picker should be nil")
		}
	}


	// assert pending == nil
	if gsb.(*gracefulSwitchBalancer).balancerPending != nil {
		t.Fatal("Pending LB switching to READY state should swap pending to current")
	}
}


// Test that tests Update with 1 + Update with 2 = two balancers

// Current isn't ready, Pending sending any Update should cause it to switch from current to pending ) i.e. UpdateState() call

// This is same as previous, except you don't update current with READY connectivity state...see if you want to pull out into common functionality



// Test that tests Update with 1 + Update with 2 = two balancers

// Current leaving READY should cause it to switch current to pending (i.e. UpdateState())

// Same as two ago, except you don't update pending at the end, you update current to a state that isn't READY



// Afterward...permutations of API calls

// Including stuff to do with NewSubconns and different types of Subconns - Add/Remove Subconns

// Def need to test whether Subconns updates end up at the correct balancer (i.e. scToSubBalancer works)

// What is flow of how balancers create Subconns?


// Test that tests Close(), downstream effects of closing SubConns, and also guarding everything else
func (s) TestBalancerClose(t *testing.T) {
	// Setup gsb balancer with current, pending, and also different types of SubConns

	// Close() the gsb balancer

	// Downstream effects of Close()
	// Remove() any created subconns
	// Close() both balancers

	// Also, once this event happens, trying to do anything else on both codepaths
	// should be a logical no-op
}



// Is there a way to test race conditions?







// Works normally in current system






// Mock balancer.Balancer here (the current or pending balancer)

// register it, also have an unexported function that allows it to ping up to balancer.ClientConn (updateState() and newSubConn() eventually)
const balancerName1 = "mock_balancer_1" // <- put this as name of config
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
		cc: cc,
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
	closeCh    *testutils.Channel
	// Hold onto Client Conn wrapper to communicate with it
	cc balancer.ClientConn
}

type subConnWithState struct {
	sc balancer.SubConn
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
	if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{})) {
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



// it's determined by config type, so need a second balancer here
type bb2 struct{}

func (bb2) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &mockBalancer2{
		ccsCh:         testutils.NewChannel(),
		scStateCh:     testutils.NewChannel(),
		resolverErrCh: testutils.NewChannel(),
		closeCh:       testutils.NewChannel(),
		cc: cc,
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
	closeCh    *testutils.Channel
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
	if !cmp.Equal(gotSCS, wantSCS, cmp.AllowUnexported(subConnWithState{})) {
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



// Order of what to do:

// 1. Fix Menghan's PR comments wrt implementation, get three tests to continue to pass
// 2. Add the tests that are give mes and get them to run, maybe factor the code out into reusable segments
// 3. Add Subconn tests/Close tests
// 4. Think of any more tests you need to add that are special cases?
