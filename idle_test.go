/*
 *
 * Copyright 2023 gRPC authors.
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

package grpc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	defaultTestIdleTimeout  = 500 * time.Millisecond // A short idle_timeout for tests.
	defaultTestShortTimeout = 10 * time.Millisecond  // A small deadline to wait for events expected to not happen.
)

type testIdlenessEnforcer struct {
	exitIdleCh  chan struct{}
	enterIdleCh chan struct{}
}

func (ti *testIdlenessEnforcer) exitIdleMode() error {
	ti.exitIdleCh <- struct{}{}
	return nil

}

func (ti *testIdlenessEnforcer) enterIdleMode() error {
	ti.enterIdleCh <- struct{}{}
	return nil

}

func newTestIdlenessEnforcer() *testIdlenessEnforcer {
	return &testIdlenessEnforcer{
		exitIdleCh:  make(chan struct{}, 1),
		enterIdleCh: make(chan struct{}, 1),
	}
}

// overrideNewTimer overrides the new timer creation function by ensuring that a
// message is pushed on the returned channel everytime the timer fires.
func overrideNewTimer(t *testing.T) <-chan struct{} {
	t.Helper()

	ch := make(chan struct{}, 1)
	origTimeAfterFunc := timeAfterFunc
	timeAfterFunc = func(d time.Duration, callback func()) *time.Timer {
		return time.AfterFunc(d, func() {
			select {
			case ch <- struct{}{}:
			default:
			}
			callback()
		})
	}
	t.Cleanup(func() { timeAfterFunc = origTimeAfterFunc })
	return ch
}

// TestIdlenessManager_Disabled tests the case where the idleness manager is
// disabled by passing an idle_timeout of 0. Verifies the following things:
//   - timer callback does not fire
//   - an RPC does not trigger a call to exitIdleMode on the ClientConn
//   - more calls to RPC termination (as compared to RPC initiation) does not
//     result in an error log
func (s) TestIdlenessManager_Disabled(t *testing.T) {
	callbackCh := overrideNewTimer(t)

	// Create an idleness manager that is disabled because of idleTimeout being
	// set to `0`.
	enforcer := newTestIdlenessEnforcer()
	mgr := newIdlenessManager(enforcer, time.Duration(0))

	// Ensure that the timer callback does not fire within a short deadline.
	select {
	case <-callbackCh:
		t.Fatal("Idle timer callback fired when manager is disabled")
	case <-time.After(defaultTestShortTimeout):
	}

	// The first invocation of onCallBegin() would lead to a call to
	// exitIdleMode() on the enforcer, unless the idleness manager is disabled.
	mgr.onCallBegin()
	select {
	case <-enforcer.exitIdleCh:
		t.Fatalf("exitIdleMode() called on enforcer when manager is disabled")
	case <-time.After(defaultTestShortTimeout):
	}

	// If the number of calls to onCallEnd() exceeds the number of calls to
	// onCallBegin(), the idleness manager is expected to throw an error log
	// (which will cause our TestLogger to fail the test). But since the manager
	// is disabled, this should not happen.
	mgr.onCallEnd()
	mgr.onCallEnd()

	// The idleness manager is explicitly not closed here. But since the manager
	// is disabled, it will not start the run goroutine, and hence we expect the
	// leakchecker to not find any leaked goroutines.
}

// TestIdlenessManager_Enabled_TimerFires tests the case where the idle manager
// is enabled. Ensures that when there are no RPCs, the timer callback is
// invoked and the enterIdleMode() method is invoked on the enforcer.
func (s) TestIdlenessManager_Enabled_TimerFires(t *testing.T) {
	callbackCh := overrideNewTimer(t)

	enforcer := newTestIdlenessEnforcer()
	mgr := newIdlenessManager(enforcer, time.Duration(defaultTestIdleTimeout))
	defer mgr.close()

	// Ensure that the timer callback fires within a appropriate amount of time.
	select {
	case <-callbackCh:
	case <-time.After(2 * defaultTestIdleTimeout):
		t.Fatal("Timeout waiting for idle timer callback to fire")
	}

	// Ensure that the channel moves to idle mode eventually.
	select {
	case <-enforcer.enterIdleCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout waiting for channel to move to idle")
	}
}

// TestIdlenessManager_Enabled_OngoingCall tests the case where the idle manager
// is enabled. Ensures that when there is an ongoing RPC, the channel does not
// enter idle mode.
func (s) TestIdlenessManager_Enabled_OngoingCall(t *testing.T) {
	callbackCh := overrideNewTimer(t)

	enforcer := newTestIdlenessEnforcer()
	mgr := newIdlenessManager(enforcer, time.Duration(defaultTestIdleTimeout))
	defer mgr.close()

	// Fire up a goroutine that simulates an ongoing RPC that is terminated
	// after the timer callback fires for the first time.
	timerFired := make(chan struct{})
	go func() {
		mgr.onCallBegin()
		<-timerFired
		mgr.onCallEnd()
	}()

	// Ensure that the timer callback fires and unblock the above goroutine.
	select {
	case <-callbackCh:
		close(timerFired)
	case <-time.After(2 * defaultTestIdleTimeout):
		t.Fatal("Timeout waiting for idle timer callback to fire")
	}

	// The invocation of the timer callback should not put the channel in idle
	// mode since we had an ongoing RPC.
	select {
	case <-enforcer.enterIdleCh:
		t.Fatalf("enterIdleMode() called on enforcer when active RPC exists")
	case <-time.After(defaultTestShortTimeout):
	}

	// Since we terminated the ongoing RPC and we have no other active RPCs, the
	// channel must move to idle eventually.
	select {
	case <-enforcer.enterIdleCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout waiting for channel to move to idle")
	}
}

// TestIdlenessManager_Enabled_ActiveSinceLastCheck tests the case where the
// idle manager is enabled. Ensures that when there are active RPCs in the last
// period (even though there is no active call when the timer fires), the
// channel does not enter idle mode.
func (s) TestIdlenessManager_Enabled_ActiveSinceLastCheck(t *testing.T) {
	callbackCh := overrideNewTimer(t)

	enforcer := newTestIdlenessEnforcer()
	mgr := newIdlenessManager(enforcer, time.Duration(defaultTestIdleTimeout))
	defer mgr.close()

	// Fire up a goroutine that simulates unary RPCs until the timer callback
	// fires.
	timerFired := make(chan struct{})
	go func() {
		for ; ; <-time.After(defaultTestShortTimeout) {
			mgr.onCallBegin()
			mgr.onCallEnd()

			select {
			case <-timerFired:
				return
			default:
			}
		}
	}()

	// Ensure that the timer callback fires, and that we don't enter idle as
	// part of this invocation of the timer callback, since we had some RPCs in
	// this period.
	select {
	case <-callbackCh:
		close(timerFired)
	case <-time.After(2 * defaultTestIdleTimeout):
		t.Fatal("Timeout waiting for idle timer callback to fire")
	}
	select {
	case <-enforcer.enterIdleCh:
		t.Fatalf("enterIdleMode() called on enforcer when one RPC completed in the last period")
	case <-time.After(defaultTestShortTimeout):
	}

	// Since the unrary RPC terminated and we have no other active RPCs, the
	// channel must move to idle eventually.
	select {
	case <-enforcer.enterIdleCh:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout waiting for channel to move to idle")
	}
}

// TestIdlenessManager_Enabled_ExitIdleOnRPC tests the case where the idle
// manager is enabled. Ensures that the channel moves out of idle when an RPC is
// initiated.
func (s) TestIdlenessManager_Enabled_ExitIdleOnRPC(t *testing.T) {
	overrideNewTimer(t)

	enforcer := newTestIdlenessEnforcer()
	mgr := newIdlenessManager(enforcer, time.Duration(defaultTestIdleTimeout))
	defer mgr.close()

	// Ensure that the channel moves to idle since there are no RPCs.
	select {
	case <-enforcer.enterIdleCh:
	case <-time.After(2 * defaultTestIdleTimeout):
		t.Fatal("Timeout waiting for channel to move to idle mode")
	}

	for i := 0; i < 100; i++ {
		// A call to onCallBegin and onCallEnd simulates an RPC.
		go func() {
			if err := mgr.onCallBegin(); err != nil {
				t.Errorf("onCallBegin() failed: %v", err)
			}
			mgr.onCallEnd()
		}()
	}

	// Ensure that the channel moves out of idle as a result of the above RPC.
	select {
	case <-enforcer.exitIdleCh:
	case <-time.After(2 * defaultTestIdleTimeout):
		t.Fatal("Timeout waiting for channel to move out of idle mode")
	}

	// Ensure that only one call to exit idle mode is made to the CC.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-enforcer.exitIdleCh:
		t.Fatal("More than one call to exit idle mode on the ClientConn; only one expected")
	case <-sCtx.Done():
	}
}

type racyIdlenessState int32

const (
	stateInital racyIdlenessState = iota
	stateEnteredIdle
	stateExitedIdle
	stateActiveRPCs
)

// racyIdlnessEnforcer is a test idleness enforcer used specifically to test the
// race between idle timeout and incoming RPCs.
type racyIdlenessEnforcer struct {
	state *racyIdlenessState // Accessed atomically.
}

// exitIdleMode sets the internal state to stateExitedIdle. We should only ever
// exit idle when we are currently in idle.
func (ri *racyIdlenessEnforcer) exitIdleMode() error {
	if !atomic.CompareAndSwapInt32((*int32)(ri.state), int32(stateEnteredIdle), int32(stateExitedIdle)) {
		return fmt.Errorf("idleness enforcer asked to exit idle when it did not enter idle earlier")
	}
	return nil
}

// enterIdleMode attempts to set the internal state to stateEnteredIdle. We should only ever enter idle before RPCs start.
func (ri *racyIdlenessEnforcer) enterIdleMode() error {
	if !atomic.CompareAndSwapInt32((*int32)(ri.state), int32(stateInital), int32(stateEnteredIdle)) {
		return fmt.Errorf("idleness enforcer asked to enter idle after rpcs started")
	}
	return nil
}

// TestIdlenessManager_IdleTimeoutRacesWithOnCallBegin tests the case where
// firing of the idle timeout races with an incoming RPC. The test verifies that
// if the timer callback win the race and puts the channel in idle, the RPCs can
// kick it out of idle. And if the RPCs win the race and keep the channel
// active, then the timer callback should not attempt to put the channel in idle
// mode.
func (s) TestIdlenessManager_IdleTimeoutRacesWithOnCallBegin(t *testing.T) {
	// Run multiple iterations to simulate different possibilities.
	for i := 0; i < 10; i++ {
		t.Run(fmt.Sprintf("iteration=%d", i), func(t *testing.T) {
			var idlenessState racyIdlenessState
			enforcer := &racyIdlenessEnforcer{state: &idlenessState}

			// Configure a large idle timeout so that we can control the
			// race between the timer callback and RPCs.
			mgr := newIdlenessManager(enforcer, time.Duration(10*time.Minute))
			defer mgr.close()

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := mgr.(interface{ handleIdleTimeout() })
				<-time.After(defaultTestIdleTimeout)
				m.handleIdleTimeout()
			}()
			for j := 0; j < 100; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					// Wait for the configured idle timeout and simulate an RPC to
					// race with the idle timeout timer callback.
					<-time.After(defaultTestIdleTimeout)
					if err := mgr.onCallBegin(); err != nil {
						t.Errorf("onCallBegin() failed: %v", err)
					}
					atomic.StoreInt32((*int32)(&idlenessState), int32(stateActiveRPCs))
					mgr.onCallEnd()
				}()
			}
			wg.Wait()
		})
	}
}
