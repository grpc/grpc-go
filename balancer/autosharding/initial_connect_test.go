/*
 *
 * Copyright 2026 gRPC authors.
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

package autosharding_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/autosharding"
	"google.golang.org/grpc/balancer/autosharding/internal"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
)

// stateRecordingClientConn wraps a testutils.BalancerClientConn and records
// every connectivity state reported by the balancer. This is required because
// testutils.BalancerClientConn only retains the most recently reported state.
type stateRecordingClientConn struct {
	*testutils.BalancerClientConn

	mu     sync.Mutex
	states []connectivity.State
}

func (cc *stateRecordingClientConn) UpdateState(bs balancer.State) {
	cc.mu.Lock()
	cc.states = append(cc.states, bs.ConnectivityState)
	cc.mu.Unlock()
	cc.BalancerClientConn.UpdateState(bs)
}

// verifyNoIdleBefore fails the test if IDLE was reported before the first
// occurrence of the given state. If the given state was never reported, it
// verifies that IDLE was never reported.
func (cc *stateRecordingClientConn) verifyNoIdleBefore(t *testing.T, until connectivity.State) {
	t.Helper()
	cc.mu.Lock()
	defer cc.mu.Unlock()
	for _, s := range cc.states {
		if s == until {
			return
		}
		if s == connectivity.Idle {
			t.Fatalf("Balancer reported IDLE before %v; reported states: %v", until, cc.states)
		}
	}
}

// fakeTimer is a fake for the initial connect timer, which fires only when the
// test calls fire.
type fakeTimer struct {
	fire    func()
	stopped *grpcsync.Event
}

// overrideInitialConnectTimer overrides the function used by the balancer to
// start the initial connect timer, and returns a channel on which a fakeTimer
// is pushed every time the balancer starts the timer.
func overrideInitialConnectTimer(t *testing.T) chan *fakeTimer {
	t.Helper()
	// The balancer invokes AfterFunc while holding its mutex, so pushing onto the
	// channel must never block. Tests read each timer before triggering the start
	// of the next one, so a buffer of one suffices. The default case below catches
	// the balancer starting a timer that the test does not expect.
	ch := make(chan *fakeTimer, 1)
	orig := internal.AfterFunc
	t.Cleanup(func() { internal.AfterFunc = orig })
	internal.AfterFunc = func(_ time.Duration, f func()) func() bool {
		ft := &fakeTimer{fire: f, stopped: grpcsync.NewEvent()}
		select {
		case ch <- ft:
		default:
			t.Errorf("Too many initial connect timers started")
		}
		return func() bool { ft.stopped.Fire(); return true }
	}
	return ch
}

// overrideRandIntN overrides the function used by the balancer to pick a random
// endpoint to connect to.
func overrideRandIntN(t *testing.T, f func(int) int) {
	t.Helper()
	orig := internal.RandIntN
	t.Cleanup(func() { internal.RandIntN = orig })
	internal.RandIntN = f
}

func waitForTimer(ctx context.Context, t *testing.T, ch chan *fakeTimer) *fakeTimer {
	t.Helper()
	select {
	case ft := <-ch:
		return ft
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the initial connect timer to be started")
		return nil
	}
}

func verifyNoTimer(t *testing.T, ch chan *fakeTimer) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("Initial connect timer started unexpectedly")
	case <-time.After(defaultTestShortTimeout):
	}
}

func verifyNoSubConn(t *testing.T, cc *stateRecordingClientConn) {
	t.Helper()
	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("SubConn %v created unexpectedly", sc)
	case <-time.After(defaultTestShortTimeout):
	}
}

// waitForSubConnWithAddr waits for a SubConn to be created for the given
// address, and for Connect() to be called on it.
func waitForSubConnWithAddr(ctx context.Context, t *testing.T, cc *stateRecordingClientConn, addr string) *testutils.TestSubConn {
	t.Helper()
	sc := waitForSubConn(ctx, t, cc.BalancerClientConn)
	if got := sc.Addresses[0].Addr; got != addr {
		t.Fatalf("SubConn created with addr %q, want %q", got, addr)
	}
	select {
	case <-sc.ConnectCh:
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for Connect() on SubConn for %q", addr)
	}
	return sc
}

type initialConnectTestEnv struct {
	b        balancer.Balancer
	cc       *stateRecordingClientConn
	state    resolver.State
	lbConfig *autosharding.LBConfig
	tac      *testAutoshardingClient
	timerCh  chan *fakeTimer
}

// setupInitialConnectTest builds an autosharding balancer with a fake initial
// connect timer, sends it a configuration with two endpoints ("host-a" at
// "10.0.0.1:8080" and "host-b" at "10.0.0.2:8080"), and verifies that the
// balancer reports CONNECTING with a queueing picker.
func setupInitialConnectTest(ctx context.Context, t *testing.T, enableFallback bool) *initialConnectTestEnv {
	t.Helper()

	timerCh := overrideInitialConnectTimer(t)
	tacCh := overrideNewAutoshardingClientForTesting(t)
	cc := &stateRecordingClientConn{BalancerClientConn: testutils.NewBalancerClientConn(t)}
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	t.Cleanup(b.Close)

	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
		EnableFallback:     enableFallback,
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{ResolverState: state, BalancerConfig: lbConfig}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}

	var tac *testAutoshardingClient
	select {
	case tac = <-tacCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("WaitForPickerWithErr(ErrNoSubConnAvailable) failed: %v", err)
	}
	return &initialConnectTestEnv{b: b, cc: cc, state: state, lbConfig: lbConfig, tac: tac, timerCh: timerCh}
}

// twoSliceAssignment returns an assignment that routes ["", "m") to host-a and
// ["m", inf) to host-b.
func twoSliceAssignment() *sharding.Assignment {
	return &sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	}
}

// waitForAssignmentPicker waits for a picker that is not the queueing picker
// reported before the first result from the autosharding client. The queueing
// picker returns ErrNoSubConnAvailable for a pick without the sharding key
// header, whereas the autosharding picker fails such picks with a different
// error.
func waitForAssignmentPicker(ctx context.Context, t *testing.T, cc *stateRecordingClientConn) balancer.Picker {
	t.Helper()
	var picker balancer.Picker
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		if _, err := p.Pick(balancer.PickInfo{Ctx: ctx}); errors.Is(err, balancer.ErrNoSubConnAvailable) {
			return fmt.Errorf("still using the queueing picker")
		}
		picker = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return picker
}

// Tests that when the first assignment is received and a pick triggers a
// connection attempt before the initial connect timer fires, the balancer
// continues to report CONNECTING (and never reports IDLE), stops the timer, and
// does not trigger a connection attempt to a random endpoint.
func (s) TestInitialConnect_PickBeforeTimerFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	env := setupInitialConnectTest(ctx, t, false)
	env.tac.onAssignmentUpdate(twoSliceAssignment())
	ft := waitForTimer(ctx, t, env.timerCh)

	// The pick for host-a should trigger a connection attempt to host-a.
	p := waitForAssignmentPicker(ctx, t, env.cc)
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick(\"a\") error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConnWithAddr(ctx, t, env.cc, "10.0.0.1:8080")

	// Once host-a moves out of IDLE, the timer must be stopped. A callback
	// from the stopped timer must not trigger a connection attempt.
	waitForEvent(ctx, t, ft.stopped, "initial connect timer to be stopped")
	ft.fire()
	verifyNoSubConn(t, env.cc)
	verifyNoTimer(t, env.timerCh)

	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}
	env.cc.verifyNoIdleBefore(t, connectivity.Ready)
}

// Tests that when the first usable result is received from the autosharding
// client and no pick triggers a connection attempt, the balancer continues to
// report CONNECTING (including across resolver updates), and triggers a
// connection attempt to a random endpoint when the initial connect timer fires.
// Also verifies that the timer is restarted when it fires, and that the
// restarted timer is stopped once an endpoint moves out of IDLE.
func (s) TestInitialConnect_TimerFiresWithoutPick(t *testing.T) {
	tests := []struct {
		name           string
		enableFallback bool
		deliverResult  func(*testAutoshardingClient)
	}{
		{
			name:          "assignment",
			deliverResult: func(tac *testAutoshardingClient) { tac.onAssignmentUpdate(twoSliceAssignment()) },
		},
		{
			name:           "error_with_fallback_enabled",
			enableFallback: true,
			deliverResult:  func(tac *testAutoshardingClient) { tac.onAssignmentError(errors.New("autosharding client error")) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Pick the endpoint with the highest index, i.e. host-b.
			overrideRandIntN(t, func(n int) int { return n - 1 })

			env := setupInitialConnectTest(ctx, t, test.enableFallback)
			test.deliverResult(env.tac)
			ft1 := waitForTimer(ctx, t, env.timerCh)
			_ = waitForAssignmentPicker(ctx, t, env.cc)

			// A resolver update while waiting for the timer to fire must not
			// result in IDLE being reported, or in a connection attempt.
			if err := env.b.UpdateClientConnState(balancer.ClientConnState{ResolverState: env.state, BalancerConfig: env.lbConfig}); err != nil {
				t.Fatalf("UpdateClientConnState() failed: %v", err)
			}
			verifyNoSubConn(t, env.cc)

			// When the timer fires, a connection attempt must be triggered to
			// the chosen endpoint, and the timer must be restarted.
			ft1.fire()
			scB := waitForSubConnWithAddr(ctx, t, env.cc, "10.0.0.2:8080")
			ft2 := waitForTimer(ctx, t, env.timerCh)

			// The restarted timer must be stopped once host-b reports that it
			// has moved out of IDLE.
			waitForEvent(ctx, t, ft2.stopped, "restarted initial connect timer to be stopped")

			scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
			scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
			if err := env.cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
				t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
			}
			env.cc.verifyNoIdleBefore(t, connectivity.Ready)
		})
	}
}

// Tests that when ExitIdle is called before the first result is received from
// the autosharding client, a connection attempt is triggered to a random
// endpoint as soon as the first result is received, without waiting for the
// initial connect timer to fire. The timer is still started as a backstop, and
// is stopped once the endpoint moves out of IDLE.
func (s) TestInitialConnect_ExitIdleBeforeFirstResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Pick the endpoint with the lowest index, i.e. host-a.
	overrideRandIntN(t, func(int) int { return 0 })

	env := setupInitialConnectTest(ctx, t, false)

	// ExitIdle before the first result must not trigger a connection attempt.
	env.b.ExitIdle()
	verifyNoSubConn(t, env.cc)

	env.tac.onAssignmentUpdate(twoSliceAssignment())
	_ = waitForSubConnWithAddr(ctx, t, env.cc, "10.0.0.1:8080")
	ft := waitForTimer(ctx, t, env.timerCh)
	waitForEvent(ctx, t, ft.stopped, "initial connect timer to be stopped")

	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}
	env.cc.verifyNoIdleBefore(t, connectivity.Ready)
}

// Tests that when ExitIdle is called after the first result is received from
// the autosharding client, while the initial connect timer is running, a
// connection attempt is triggered to a random endpoint immediately (without
// waiting for the timer to fire), and the timer is left running. The timer is
// stopped once the endpoint moves out of IDLE, and a callback from it after
// that is ignored.
func (s) TestInitialConnect_ExitIdleWhileTimerRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Pick the endpoint with the highest index, i.e. host-b.
	overrideRandIntN(t, func(n int) int { return n - 1 })

	env := setupInitialConnectTest(ctx, t, false)
	env.tac.onAssignmentUpdate(twoSliceAssignment())
	ft := waitForTimer(ctx, t, env.timerCh)
	verifyNoSubConn(t, env.cc)

	// ExitIdle while the timer is running must trigger a connection attempt
	// right away, and must not restart the timer.
	env.b.ExitIdle()
	_ = waitForSubConnWithAddr(ctx, t, env.cc, "10.0.0.2:8080")
	verifyNoTimer(t, env.timerCh)

	// The timer must be stopped once host-b reports that it has moved out of
	// IDLE.
	waitForEvent(ctx, t, ft.stopped, "initial connect timer to be stopped")

	// A callback from the stopped timer must not trigger another connection
	// attempt.
	ft.fire()
	verifyNoSubConn(t, env.cc)
	verifyNoTimer(t, env.timerCh)

	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}
	env.cc.verifyNoIdleBefore(t, connectivity.Ready)
}

// Tests that when the first result from the autosharding client is an error and
// fallback is disabled, the balancer reports TRANSIENT_FAILURE and does not
// start the initial connect timer. When a valid assignment is received
// subsequently, normal aggregation rules apply (resulting in IDLE, since all
// endpoints are IDLE), and ExitIdle triggers a connection attempt to a random
// endpoint.
func (s) TestInitialConnect_TransientFailureThenIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Pick the endpoint with the highest index, i.e. host-b.
	overrideRandIntN(t, func(n int) int { return n - 1 })

	env := setupInitialConnectTest(ctx, t, false)

	env.tac.onAssignmentError(errors.New("autosharding client error"))
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}
	verifyNoTimer(t, env.timerCh)
	env.cc.verifyNoIdleBefore(t, connectivity.TransientFailure)

	env.tac.onAssignmentUpdate(twoSliceAssignment())
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}
	verifyNoTimer(t, env.timerCh)
	verifyNoSubConn(t, env.cc)

	// ExitIdle while IDLE must trigger a connection attempt to a random
	// endpoint.
	env.b.ExitIdle()
	_ = waitForSubConnWithAddr(ctx, t, env.cc, "10.0.0.2:8080")
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}
}

// Tests that after a resolver error results in TRANSIENT_FAILURE, a subsequent
// valid configuration and assignment result in normal aggregation rules being
// applied, i.e. the initial connect logic is not re-armed.
func (s) TestInitialConnect_ResolverErrorClearsPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	env := setupInitialConnectTest(ctx, t, false)

	// An update with no endpoints results in TRANSIENT_FAILURE.
	if err := env.b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), nil),
		BalancerConfig: env.lbConfig,
	}); err == nil {
		t.Fatal("UpdateClientConnState() with no endpoints succeeded, want error")
	}
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}

	// A subsequent valid update creates a new autosharding client. Until it
	// returns a result, RPCs are queued.
	tacCh := overrideNewAutoshardingClientForTesting(t)
	if err := env.b.UpdateClientConnState(balancer.ClientConnState{ResolverState: env.state, BalancerConfig: env.lbConfig}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	var tac *testAutoshardingClient
	select {
	case tac = <-tacCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Since TRANSIENT_FAILURE was reported, the first assignment from the new
	// client results in IDLE (since all endpoints are IDLE), and the initial
	// connect timer is not started.
	tac.onAssignmentUpdate(twoSliceAssignment())
	if err := env.cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}
	verifyNoTimer(t, env.timerCh)
	env.cc.verifyNoIdleBefore(t, connectivity.TransientFailure)
}
