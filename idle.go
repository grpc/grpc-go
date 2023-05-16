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
	"math"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// For overriding in unit tests.
var timeAfterFunc = func(d time.Duration, f func()) *time.Timer {
	return time.AfterFunc(d, f)
}

// idlenessEnforcer is the functionality provided by grpc.ClientConn to enter
// and exit from idle mode.
type idlenessEnforcer interface {
	exitIdleMode() error
	enterIdleMode() error
}

// idlenessManager defines the functionality required to track RPC activity on a
// channel.
type idlenessManager interface {
	onCallBegin() error
	onCallEnd()
	close()
}

type disabledIdlenessManager struct{}

func (disabledIdlenessManager) onCallBegin() error { return nil }
func (disabledIdlenessManager) onCallEnd()         {}
func (disabledIdlenessManager) close()             {}

func newDisabledIdlenessManager() idlenessManager { return disabledIdlenessManager{} }

// mutexIdlenessManager implements the idlenessManager interface and uses a
// mutex to synchronize access to shared state.
type mutexIdlenessManager struct {
	// The following fields are set when the manager is created and are never
	// written to after that. Therefore these can be accessed without a mutex.

	enforcer idlenessEnforcer // Functionality provided by grpc.ClientConn.
	timeout  int64            // Idle timeout duration nanos stored as an int64.

	// All state maintained by the manager is guarded by this mutex.
	mu                        sync.Mutex
	activeCallsCount          int         // Count of active RPCs.
	activeSinceLastTimerCheck bool        // True if there was an RPC since the last timer callback.
	lastCallEndTime           int64       // Time when the most recent RPC finished, stored as unix nanos.
	isIdle                    bool        // True if the channel is in idle mode.
	timer                     *time.Timer // Expires when the idle_timeout fires.
}

// newMutexIdlenessManager creates a new mutexIdlennessManager. A non-zero value
// must be passed for idle timeout.
func newMutexIdlenessManager(enforcer idlenessEnforcer, idleTimeout time.Duration) idlenessManager {
	i := &mutexIdlenessManager{
		enforcer: enforcer,
		timeout:  int64(idleTimeout),
	}
	i.timer = timeAfterFunc(idleTimeout, i.handleIdleTimeout)
	return i
}

// handleIdleTimeout is the timer callback when idle_timeout expires.
func (i *mutexIdlenessManager) handleIdleTimeout() {
	i.mu.Lock()
	defer i.mu.Unlock()

	// If there are ongoing RPCs, it means the channel is active. Reset the
	// timer to fire after a duration of idle_timeout, and return early.
	if i.activeCallsCount > 0 {
		i.timer.Reset(time.Duration(i.timeout))
		return
	}

	// There were some RPCs made since the last time we were here. So, the
	// channel is still active.  Reschedule the timer to fire after a duration
	// of idle_timeout from the time the last call ended.
	if i.activeSinceLastTimerCheck {
		i.activeSinceLastTimerCheck = false
		// It is safe to ignore the return value from Reset() because we are
		// already in the timer callback and this is only place from where we
		// reset the timer.
		i.timer.Reset(time.Duration(i.lastCallEndTime + i.timeout - time.Now().UnixNano()))
		return
	}

	// There are no ongoing RPCs, and there were no RPCs since the last time we
	// were here, we are all set to enter idle mode.
	if err := i.enforcer.enterIdleMode(); err != nil {
		logger.Warningf("Failed to enter idle mode: %v", err)
		return
	}
	i.isIdle = true
}

// onCallBegin is invoked by the ClientConn at the start of every RPC. If the
// channel is currently in idle mode, the manager asks the ClientConn to exit
// idle mode, and restarts the timer. The active calls count is incremented and
// the activeness bit is set to true.
func (i *mutexIdlenessManager) onCallBegin() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.activeCallsCount++
	i.activeSinceLastTimerCheck = true

	if !i.isIdle {
		return nil
	}

	if err := i.enforcer.exitIdleMode(); err != nil {
		return status.Errorf(codes.Internal, "grpc: ClientConn failed to exit idle mode: %v", err)
	}
	i.timer = timeAfterFunc(time.Duration(i.timeout), i.handleIdleTimeout)
	i.isIdle = false
	return nil
}

// onCallEnd is invoked by the ClientConn at the end of every RPC. The active
// calls count is decremented and `i.lastCallEndTime` is updated.
func (i *mutexIdlenessManager) onCallEnd() {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.activeCallsCount--
	if i.activeCallsCount < 0 {
		logger.Errorf("Number of active calls tracked by idleness manager is negative: %d", i.activeCallsCount)
	}
	i.lastCallEndTime = time.Now().UnixNano()
}

func (i *mutexIdlenessManager) close() {
	i.mu.Lock()
	i.timer.Stop()
	i.mu.Unlock()
}

// atomicIdlenessManger implements the idlenessManager interface. It uses atomic
// operations to synchronize access to shared state and a mutex to guarantee
// mutual exclusion in a critical section.
type atomicIdlenessManager struct {
	// State accessed atomically.
	lastCallEndTime           int64        // Unix timestamp in nanos; time when the most recent RPC completed.
	activeCallsCount          int32        // Count of active RPCs; math.MinInt32 indicates channel is idle.
	activeSinceLastTimerCheck int32        // Boolean; True if there was an RPC since the last timer callback.
	closed                    int32        // Boolean; True when the manager is closed.
	timer                     atomic.Value // Of type `*time.Timer`

	// Can be accessed without atomics or mutex since these are set at creation
	// time and read-only after that.
	enforcer idlenessEnforcer // Functionality provided by grpc.ClientConn.
	timeout  int64            // Idle timeout duration nanos stored as an int64.

	// idleMu is used to guarantee mutual exclusion in two scenarios:
	// - Opposing intentions. One is trying to put the channel in idle mode
	//   after a period of inactivity, while the other is trying to keep the
	//   channel from moving into idle mode to service an incoming RPC.
	// - Competing intentions. When the channel is in idle mode and there
	//   are multiple RPCs starting at the same time, all trying to move the
	//   channel out of idle, we want a single one to succeed in doing so, while
	//   the other RPCs should still be successfully handled.
	idleMu sync.RWMutex
}

// newAtomicIdlenessManager creates a new atomicIdlenessManager. A non-zero
// value must be passed for idle timeout.
func newAtomicIdlenessManager(enforcer idlenessEnforcer, idleTimeout time.Duration) idlenessManager {
	i := &atomicIdlenessManager{
		enforcer: enforcer,
		timeout:  int64(idleTimeout),
	}
	i.timer.Store(timeAfterFunc(idleTimeout, i.handleIdleTimeout))
	return i
}

// enterIdleMode instructs the ClientConn to enter idle mode. But before that,
// it performs a couple of last minute checks, to ensure that it is safe to
// instruct the ClientConn to enter idle mode.
//
// Return value indicates whether or not the ClientConn moved to idle mode.
//
// Holds idleMu which ensures mutual exclusion with exitIdleMode.
func (i *atomicIdlenessManager) enterIdleMode() bool {
	i.idleMu.Lock()
	defer i.idleMu.Unlock()

	// Compare-and-swap the active calls count with an old value of `0` and a
	// new value of `minInt32`. If this succeeds, it means that the active calls
	// count is still zero, i.e no RPCs are ongoing since we last checked the
	// count in the timer callback. If this fails, it means that RPCs have
	// started since we last checked the count in the timer callback, and
	// therefore we should not ask the ClientConn to enter idle mode.
	if !atomic.CompareAndSwapInt32(&i.activeCallsCount, 0, math.MinInt32) {
		return false
	}
	// It is possible that one or more RPCs started and finished since the last
	// time we checked the calls count and activity in the timer callback. In
	// this case, the calls count would be zero and the above compare-and-swap
	// operation would have succeeded. The `activeSinceLastTimerCheck` field is
	// set to false only from the timer callback and is set to true in
	// onCallBegin. Therefore, if one or more RPCs started and finished before
	// we got here, this field would be set to true. And in this case, we don't
	// want to enter idle mode.
	if active := atomic.LoadInt32(&i.activeSinceLastTimerCheck); active == 1 {
		return false
	}
	if err := i.enforcer.enterIdleMode(); err != nil {
		logger.Errorf("Failed to enter idle mode: %v", err)
		return false
	}
	// We are in enter idle mode now. Set the active calls count appropriately.
	atomic.StoreInt32(&i.activeCallsCount, math.MinInt32)
	return true
}

// exitIdleMode instructs the ClientConn to exit idle mode.
//
// Holds idleMu which ensures mutual exclusion with enterIdleMode.
func (i *atomicIdlenessManager) exitIdleMode() error {
	i.idleMu.Lock()
	defer i.idleMu.Unlock()

	// When the channel enters idle mode, the active calls count is set to
	// math.MinInt32. RPCs that start when the channel is in idle mode increment
	// the active calls count in onCallBegin. If there are multiple RPCs
	// competing to get the channel out of idle mode, the first one to grab the
	// lock here gets to do so and sets the active calls count back to 1. So, if
	// the count is not negative here, we know that someone else won the race
	// and moved the channel out of idle mode, and we have nothing to do here.
	if count := atomic.LoadInt32(&i.activeCallsCount); count >= 0 {
		atomic.AddInt32(&i.activeCallsCount, 1)
		return nil
	}

	// The first one to grab the lock will get here to instruct the channel to
	// move out of idle mode.
	if err := i.enforcer.exitIdleMode(); err != nil {
		return status.Errorf(codes.Internal, "grpc: ClientConn failed to exit idle mode: %v", err)
	}

	// Reset the calls count to 1 and reset the timer to fire after a duration
	// of the configured idle timeout.
	atomic.StoreInt32(&i.activeCallsCount, 1)
	i.timer.Store(timeAfterFunc(time.Duration(i.timeout), i.handleIdleTimeout))
	return nil
}

// handleIdleTimeout is the timer callback that is invoked upon expiry of the
// configured idle timeout. The channel is considered inactive if there are no
// ongoing calls and no RPC activity since the last time the timer fired.
func (i *atomicIdlenessManager) handleIdleTimeout() {
	if i.isClosed() {
		return
	}

	var timeoutDuration time.Duration
	if count := atomic.LoadInt32(&i.activeCallsCount); count > 0 {
		// Since the channel is currently active, reset the timer to the full
		// configured idle timeout duration.
		timeoutDuration = time.Duration(i.timeout)
	} else if active := atomic.LoadInt32(&i.activeSinceLastTimerCheck); active == 1 {
		// The channel is not currently active, but saw some activity since the
		// last time the timer fired. We set the timer to fire after a duration
		// of idle timeout, calculated from the time the most recent RPC
		// completed.
		atomic.StoreInt32(&i.activeSinceLastTimerCheck, 0)
		timeoutDuration = time.Duration(atomic.LoadInt64(&i.lastCallEndTime) + i.timeout - time.Now().UnixNano())
	} else {
		// Channel is inactive, try to move it idle. If we succeed in doing so,
		// we don't have to reset the timer. It will be done when the channel
		// moves out of idle.
		if i.enterIdleMode() {
			return
		}
		// We didn't move out of the idle because some RPC raced with us and
		// kept the channel active. Give the timer the full configured idle
		// timeout duration.
		timeoutDuration = time.Duration(i.timeout)
	}

	// It is safe to ignore the return value from Reset() because we are
	// already in the timer callback and this is only place from where we
	// reset the timer.
	timer := i.timer.Load().(*time.Timer)
	timer.Reset(timeoutDuration)
}

// onCallBegin is invoked at the start of every RPC.
func (i *atomicIdlenessManager) onCallBegin() error {
	if i.isClosed() {
		return nil
	}

	// Set RPC activity on the channel to true.
	atomic.StoreInt32(&i.activeSinceLastTimerCheck, 1)

	// Increment the calls count and if the new value is positive, it means that
	// the channel is not in idle mode. So, we can return early.
	if count := atomic.AddInt32(&i.activeCallsCount, 1); count > 0 {
		return nil
	}

	// The active calls count is set to math.MinIn32 when the channel enters
	// idle. So, if the channel is still in idle mode, we expect this value to
	// be negative at this point. If there are more than 2 billion RPCs that
	// start at the same time (when the channel is in idle mode), this count
	// could become positive, but it is highly unlikely to ever hit that case.
	//
	// Ask the ClientConn to exit idle mode now.
	return i.exitIdleMode()
}

// onCallEnd is invoked at the end of every RPC.
func (i *atomicIdlenessManager) onCallEnd() {
	if i.isClosed() {
		return
	}

	// Record the time at which the most recent call finished.
	atomic.StoreInt64(&i.lastCallEndTime, time.Now().UnixNano())

	// Decrement the active calls count. We expect this count to never go
	// negative. This count should be negative only when the channel is in idle
	// mode, and we cannot be in idle mode at this moment because we are
	// handling the completion of an RPC.
	if n := atomic.AddInt32(&i.activeCallsCount, -1); n < 0 {
		logger.Errorf("Number of active calls tracked by idleness manager is negative: %d", n)
	}
}

func (i *atomicIdlenessManager) isClosed() bool {
	closed := atomic.LoadInt32(&i.closed)
	return closed == 1
}

func (i *atomicIdlenessManager) close() {
	atomic.StoreInt32(&i.closed, 1)
	timer := i.timer.Load().(*time.Timer)
	timer.Stop()
}
