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

package priority

import (
	"errors"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
)

var (
	errAllPrioritiesRemoved    = errors.New("no locality is provided, all priorities are removed")
	defaultPriorityInitTimeout = 10 * time.Second
)

// syncPriority handles priority after a config update. It makes sure the
// balancer state (started or not) is in sync with the priorities (even in
// tricky cases where a child is moved from a priority to another).
//
// It's guaranteed that after this function returns:
// - If some child is READY, it is childInUse, and all lower priorities are
// closed.
// - If some child is newly started(in Connecting for the first time), it is
// childInUse, and all lower priorities are closed.
// - Otherwise, the lowest priority is childInUse (none of the children is
// ready, and the overall state is not ready).
//
// Steps:
// - If all priorities were deleted, unset childInUse (to an empty string), and
// set parent ClientConn to TransientFailure
// - Otherwise, Scan all children from p0, and check balancer stats:
//   - For any of the following cases:
// 	   - If balancer is not started (not built), this is either a new child
//       with high priority, or a new builder for an existing child.
// 	   - If balancer is READY
// 	   - If this is the lowest priority
//   - do the following:
//     - if this is not the old childInUse, override picker so old picker is no
//       longer used.
//     - switch to it (because all higher priorities are neither new or Ready)
//     - forward the new addresses and config
//
// Caller must hold pb.mu.
func (pb *priorityBalancer) syncPriority() {
	// Everything was removed by the update.
	if len(pb.priorities) == 0 {
		pb.childInUse = ""
		pb.priorityInUse = 0
		// Stop the init timer. This can happen if the only priority is removed
		// shortly after it's added.
		if timer := pb.priorityInitTimer; timer != nil {
			timer.Stop()
			pb.priorityInitTimer = nil
		}
		pb.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(errAllPrioritiesRemoved),
		})
		return
	}

	for p, name := range pb.priorities {
		child, ok := pb.children[name]
		if !ok {
			pb.logger.Warningf("child with name %q is not found in children", name)
			continue
		}

		if !child.started ||
			child.state.ConnectivityState == connectivity.Ready ||
			p == len(pb.priorities)-1 {
			if pb.childInUse != "" && pb.childInUse != child.name {
				// childInUse was set and is different from this child, will
				// change childInUse later. We need to update picker here
				// immediately so parent stops using the old picker.
				pb.cc.UpdateState(child.state)
			}
			pb.logger.Warningf("switching to (%q, %v) in syncPriority", child.name, p)
			pb.switchToChild(child, p)
			child.sendUpdate()
			break
		}
	}
}

// Stop priorities [p+1, lowest].
//
// caller must hold pb.mu.
func (pb *priorityBalancer) stopSubBalancersLowerThanPriority(p int) {
	for i := p + 1; i < len(pb.priorities); i++ {
		name := pb.priorities[i]
		child, ok := pb.children[name]
		if !ok {
			pb.logger.Warningf("child with name %q is not found in children", name)
			continue
		}
		child.stop()
	}
}

// switchToChild does the following:
// - stop all child with lower priorities
// - if childInUse is not this child
//   - set childInUse to this child
//   - stops init timer
//   - if this child is not started, start it, and start a init timer
//
// Note that it does NOT send the current child state (picker) to the parent
// ClientConn. The caller needs to send it if necessary.
//
// this can be called when
// 1. first update, start p0
// 2. an update moves a READY child from a lower priority to higher
// 2. a different builder is updated for this child
// 3. a high priority goes Failure, start next
// 4. a high priority init timeout, start next
//
// caller must hold pb.mu.
func (pb *priorityBalancer) switchToChild(child *childBalancer, priority int) {
	// Stop lower priorities even if childInUse is same as this child. It's
	// possible this child was moved from a priority to another.
	pb.stopSubBalancersLowerThanPriority(priority)

	// If this child is already in use, do nothing.
	//
	// This can happen:
	// - all priorities are not READY, an config update always triggers switch
	// to the lowest. In this case, the lowest child could still be connecting,
	// so we don't stop the init timer.
	// - a high priority is READY, an config update always triggers switch to
	// it.
	if pb.childInUse == child.name && child.started {
		return
	}
	pb.childInUse = child.name
	pb.priorityInUse = priority

	// Init timer is always for childInUse. Since we are switching to a
	// different child, we will stop the init timer no matter what. If this
	// child is not started, we will start the init timer later.
	if pb.priorityInitTimer != nil {
		pb.priorityInitTimer.Stop()
		pb.priorityInitTimer = nil
	}

	if !child.started {
		child.start()
		pb.priorityInitTimer = time.AfterFunc(defaultPriorityInitTimeout, func() {
			pb.mu.Lock()
			defer pb.mu.Unlock()
			if pb.childInUse != child.name {
				return
			}
			pb.priorityInitTimer = nil
			// Switch to the next priority if there's any.
			if pNext := priority + 1; pNext < len(pb.priorities) {
				nameNext := pb.priorities[pNext]
				if childNext, ok := pb.children[nameNext]; ok {
					pb.switchToChild(childNext, pNext)
					childNext.sendUpdate()
				}
			}
		})
	}
}

// handleChildStateUpdate start/close priorities based on the connectivity
// state.
func (pb *priorityBalancer) handleChildStateUpdate(childName string, s balancer.State) {
	if pb.done.HasFired() {
		return
	}

	pb.mu.Lock()
	defer pb.mu.Unlock()

	priority, ok := pb.childToPriority[childName]
	if !ok {
		pb.logger.Warningf("priority: received picker update with unknown child %v", childName)
		return
	}

	if pb.childInUse == "" {
		pb.logger.Warningf("priority: no child is in use when picker update is received")
		return
	}

	// priorityInUse is higher than this priority.
	if pb.priorityInUse < priority {
		// Lower priorities should all be closed, this is an unexpected update.
		pb.logger.Warningf("priority: received picker update from priority %v,  lower then priority in use %v", priority, pb.priorityInUse)
		return
	}

	// Update state in child. The updated picker will be sent to parent later if
	// necessary.
	child, ok := pb.children[childName]
	if !ok {
		pb.logger.Warningf("priority: child balancer not found for child %v, priority %v", childName, priority)
		return
	}
	oldState := child.state.ConnectivityState
	child.state = s

	switch s.ConnectivityState {
	case connectivity.Ready:
		pb.handlePriorityWithNewStateReady(child, priority)
	case connectivity.TransientFailure:
		pb.handlePriorityWithNewStateTransientFailure(child, priority)
	case connectivity.Connecting:
		pb.handlePriorityWithNewStateConnecting(child, priority, oldState)
	default:
		// New state is Idle, should never happen. Don't forward.
	}
}

// handlePriorityWithNewStateReady handles state Ready from a higher or equal
// priority.
//
// An update with state Ready:
// - If it's from higher priority:
//   - Switch to this priority
//   - Forward the update
// - If it's from priorityInUse:
//   - Forward only
//
// Caller must make sure priorityInUse is not higher than priority.
//
// Caller must hold mu.
func (pb *priorityBalancer) handlePriorityWithNewStateReady(child *childBalancer, priority int) {
	// If one priority higher or equal to priorityInUse goes Ready, stop the
	// init timer. If update is from higher than priorityInUse, priorityInUse
	// will be closed, and the init timer will become useless.
	if timer := pb.priorityInitTimer; timer != nil {
		timer.Stop()
		pb.priorityInitTimer = nil
	}

	// priorityInUse is lower than this priority, switch to this.
	if pb.priorityInUse > priority {
		pb.logger.Infof("Switching priority from %v to %v, because latter became Ready", pb.priorityInUse, priority)
		pb.switchToChild(child, priority)
	}
	// Forward the update since it's READY.
	pb.cc.UpdateState(child.state)
}

// handlePriorityWithNewStateTransientFailure handles state TransientFailure
// from a higher or equal priority.
//
// An update with state TransientFailure:
// - If it's from a higher priority:
//   - Do not forward, and do nothing
// - If it's from priorityInUse:
//   - If there's no lower:
//     - Forward and do nothing else
//   - If there's a lower priority:
//     - Switch to the lower
//     - Forward the lower child's state
//     - Do NOT forward this update
//
// Caller must make sure priorityInUse is not higher than priority.
//
// Caller must hold mu.
func (pb *priorityBalancer) handlePriorityWithNewStateTransientFailure(child *childBalancer, priority int) {
	// priorityInUse is lower than this priority, do nothing.
	if pb.priorityInUse > priority {
		return
	}
	// priorityInUse sends a failure. Stop its init timer.
	if timer := pb.priorityInitTimer; timer != nil {
		timer.Stop()
		pb.priorityInitTimer = nil
	}
	priorityNext := priority + 1
	if priorityNext >= len(pb.priorities) {
		// Forward this update.
		pb.cc.UpdateState(child.state)
		return
	}
	pb.logger.Infof("Switching priority from %v to %v, because former became TransientFailure", priority, priorityNext)
	nameNext := pb.priorities[priorityNext]
	childNext := pb.children[nameNext]
	pb.switchToChild(childNext, priorityNext)
	pb.cc.UpdateState(childNext.state)
	childNext.sendUpdate()
}

// handlePriorityWithNewStateConnecting handles state Connecting from a higher
// than or equal priority.
//
// An update with state Connecting:
// - If it's from a higher priority
//   - Do nothing
// - If it's from priorityInUse, the behavior depends on previous state.
//
// When new state is Connecting, the behavior depends on previous state. If the
// previous state was Ready, this is a transition out from Ready to Connecting.
// Assuming there are multiple backends in the same priority, this mean we are
// in a bad situation and we should failover to the next priority (Side note:
// the current connectivity state aggregating algorithm (e.g. round-robin) is
// not handling this right, because if many backends all go from Ready to
// Connecting, the overall situation is more like TransientFailure, not
// Connecting).
//
// If the previous state was Idle, we don't do anything special with failure,
// and simply forward the update. The init timer should be in process, will
// handle failover if it timeouts. If the previous state was TransientFailure,
// we do not forward, because the lower priority is in use.
//
// Caller must make sure priorityInUse is not higher than priority.
//
// Caller must hold mu.
func (pb *priorityBalancer) handlePriorityWithNewStateConnecting(child *childBalancer, priority int, oldState connectivity.State) {
	// priorityInUse is lower than this priority, do nothing.
	if pb.priorityInUse > priority {
		return
	}

	switch oldState {
	case connectivity.Ready:
		// Handling transition from Ready to Connecting, is same as handling
		// TransientFailure. There's no need to stop the init timer, because it
		// should have been stopped when state turned Ready.
		priorityNext := priority + 1
		if priorityNext >= len(pb.priorities) {
			// Forward this update.
			pb.cc.UpdateState(child.state)
			return
		}
		pb.logger.Infof("Switching priority from %v to %v, because former became TransientFailure", priority, priorityNext)
		nameNext := pb.priorities[priorityNext]
		childNext := pb.children[nameNext]
		pb.switchToChild(childNext, priorityNext)
		pb.cc.UpdateState(childNext.state)
		childNext.sendUpdate()
	case connectivity.Idle:
		pb.cc.UpdateState(child.state)
	default:
		// Old state is Connecting, TransientFailure or Shutdown. Don't forward.
	}
}
