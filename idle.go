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
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/status"
)

// For overriding in unit tests.
var newTimer = func(d time.Duration, f func()) *time.Timer {
	return time.AfterFunc(d, f)
}

// idlenessEnforcer is the functionality provided by grpc.ClientConn to enter
// and exit from idle mode.
type idlenessEnforcer interface {
	exitIdleMode() error
	enterIdleMode() error
}

// idlenessManager contains functionality to track RPC activity on the channel
// and uses this to instruct the channel to enter or exit idle mode as
// appropriate.
type idlenessManager struct {
	// The following fields are set when the manager is created and are never
	// written to after that. Therefore these can be accessed without a mutex.

	enforcer   idlenessEnforcer // Functionality provided by grpc.ClientConn.
	timeout    int64            // Idle timeout duration nanos stored as an int64.
	isDisabled bool             // Disabled if idle_timeout is set to 0.

	// All state maintained by the manager is guarded by this mutex.
	mu                        sync.Mutex
	activeCallsCount          int         // Count of active RPCs.
	activeSinceLastTimerCheck bool        // True if there was an RPC since the last timer callback.
	lastCallEndTime           int64       // Time when the most recent RPC finished, stored as unix nanos.
	isIdle                    bool        // True if the channel is in idle mode.
	timer                     *time.Timer // Expires when the idle_timeout fires.
}

// newIdlenessManager creates a new idleness state manager which attempts to put
// the channel in idle mode when there is no RPC activity for the configured
// idleTimeout.
//
// Idleness support can be disabled by passing a value of 0 for idleTimeout.
func newIdlenessManager(enforcer idlenessEnforcer, idleTimeout time.Duration) *idlenessManager {
	if idleTimeout == 0 {
		logger.Infof("Channel idleness support explicitly disabled")
		return &idlenessManager{isDisabled: true}
	}

	i := &idlenessManager{
		enforcer: enforcer,
		timeout:  int64(idleTimeout),
	}
	i.timer = newTimer(idleTimeout, i.handleIdleTimeout)
	return i
}

// handleIdleTimeout is the timer callback when idle_timeout expires.
func (i *idlenessManager) handleIdleTimeout() {
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
func (i *idlenessManager) onCallBegin() error {
	if i.isDisabled {
		return nil
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.isIdle {
		if err := i.enforcer.exitIdleMode(); err != nil {
			return status.Errorf(codes.Internal, "grpc: ClientConn failed to exit idle mode: %v", err)
		}
		i.timer = newTimer(time.Duration(i.timeout), i.handleIdleTimeout)
		i.isIdle = false
	}
	i.activeCallsCount++
	i.activeSinceLastTimerCheck = true
	return nil
}

// onCallEnd is invoked by the ClientConn at the end of every RPC. The active
// calls count is decremented and `i.lastCallEndTime` is updated.
func (i *idlenessManager) onCallEnd() {
	if i.isDisabled {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.activeCallsCount--
	if i.activeCallsCount < 0 {
		logger.Errorf("Number of active calls tracked by idleness manager is negative: %d", i.activeCallsCount)
	}
	i.lastCallEndTime = time.Now().UnixNano()
}

func (i *idlenessManager) close() {
	if i.isDisabled {
		return
	}
	i.mu.Lock()
	i.timer.Stop()
	i.mu.Unlock()
}
