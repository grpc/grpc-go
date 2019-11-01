/*
 *
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

package edsbalancer

import (
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
)

// syncPriorityAfterPriorityChange handles priority after EDS adds/removes a
// priority.
//
// E.g. when priorityInUse was removed, or all priorities were down, and a new
// lower priority was added.
func (xdsB *EDSBalancer) syncPriorityAfterPriorityChange() {
	xdsB.priorityMu.Lock()
	defer xdsB.priorityMu.Unlock()

	// If there's no prioriy at all, everything was removed by EDS, unset priorityInUse.
	if xdsB.priorityMax == nil {
		xdsB.priorityInUse = nil
		xdsB.cc.UpdateBalancerState(connectivity.TransientFailure, base.NewErrPicker(balancer.ErrTransientFailure))
		return
	}

	// If priorityInUse wasn't set, this is either the first EDS resp, or the
	// previous EDS resp deleted everything.
	//
	// Set priorityInUse to 0, and start 0.
	if xdsB.priorityInUse == nil {
		xdsB.priorityInUse = new(uint32)
		xdsB.startPriority(0)
		return
	}

	// If priorityInUse was deleted, send the picker from the new lowest
	// priority to parent ClientConn, and set priorityInUse to the new lowest.
	//
	// There's no need to start the new lowest, because it must have been
	// started.
	if _, ok := xdsB.priorityToLocalities[*xdsB.priorityInUse]; !ok {
		*xdsB.priorityInUse = *xdsB.priorityMax
		if s, ok := xdsB.priorityToState[*xdsB.priorityMax]; ok {
			xdsB.cc.UpdateBalancerState(s.state, s.picker)
		} else {
			// If state for pMax is not found, this means pMax was started, but
			// never sent any update. And old_pInUse was started after timeout.
			// We don't have an old state to send to parent, but we also don't
			// want parent to keep using picker from old_pInUse. Send an update
			// to trigger block picks until a new picker is ready.
			xdsB.cc.UpdateBalancerState(connectivity.Connecting, base.NewErrPicker(balancer.ErrNoSubConnAvailable))
		}
		return
	}

	// priorityInUse is in map, and it had got a state that was not ready, and
	// also there's a priority lower than InUse. This means a lower priority was
	// added.
	//
	// Set next as new priorityInUse, and start it.
	if s, ok := xdsB.priorityToState[*xdsB.priorityInUse]; ok && s.state != connectivity.Ready {
		pNext := *xdsB.priorityInUse + 1
		if _, ok := xdsB.priorityToLocalities[pNext]; ok {
			xdsB.startPriority(pNext)
		}
	}
}

// startPriority sets priorityInUse to p, and starts the balancer group for p.
// It also starts a timer to fall to next priority after timeout.
//
// Caller must hold priorityMu, priority must exist, and xdsB.priorityInUse must
// be non-nil.
func (xdsB *EDSBalancer) startPriority(priority uint32) {
	*xdsB.priorityInUse = priority
	p := xdsB.priorityToLocalities[priority]
	// NOTE: this will eventually send addresses to sub-balancers. If the
	// sub-balancer tries to update picker, it will result in a deadlock on
	// priorityMu. But it's not an expected behavior for the balancer to
	// update picker when handling addresses.
	p.bg.start()
	// startPriority can be called when
	// 1. first EDS resp, start p0
	// 2. a high priority goes Failure, start next
	// 3. a high priority init timeout, start next
	//
	// In all the cases, the existing init timer is either closed, also already
	// expired. There's no need to close the old timer.
	xdsB.priorityInitTimer = time.AfterFunc(defaultPriorityInitTimeout, func() {
		xdsB.priorityMu.Lock()
		defer xdsB.priorityMu.Unlock()
		if *xdsB.priorityInUse != priority {
			return
		}
		xdsB.priorityInitTimer = nil
		pNext := priority + 1
		if _, ok := xdsB.priorityToLocalities[pNext]; ok {
			xdsB.startPriority(pNext)
		}
	})
}

// handlePriorityWithNewState start/close priorities based on the connectivity
// state. It returns whether the state should be forwarded to parent ClientConn.
func (xdsB *EDSBalancer) handlePriorityWithNewState(priority uint32, s connectivity.State, p balancer.Picker) bool {
	xdsB.priorityMu.Lock()
	defer xdsB.priorityMu.Unlock()

	if xdsB.priorityInUse == nil {
		grpclog.Infof("eds: received picker update when no priority is in use (this means EDS returned an empty list)")
		return false
	}

	if *xdsB.priorityInUse < priority {
		// Lower priorities should all be closed, this is an unexpected update.
		grpclog.Infof("eds: received picker update from priority lower then priorityInUse")
		return false
	}

	// Update state if exists, or add a new entry if this is the first update.
	var bState *balancerStateAndPicker
	if st, ok := xdsB.priorityToState[priority]; ok {
		bState = st
	} else {
		bState = new(balancerStateAndPicker)
		xdsB.priorityToState[priority] = bState
	}
	oldState := bState.state
	bState.state = s
	bState.picker = p

	if s == connectivity.Ready {
		// If one priority higher or equal to priorityInUse goes Ready, stop the
		// init timer even if update is not from priorityInUse.
		if timer := xdsB.priorityInitTimer; timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		// An update with state Ready:
		// - If it's from higher priority:
		//   - Forward the update
		//   - Set the priority as priorityInUse
		//   - Close all priorities lower than this one
		// - If it's from priorityInUse:
		//   - Forward and do nothing else
		if *xdsB.priorityInUse > priority {
			*xdsB.priorityInUse = priority
			for i := priority + 1; i <= *xdsB.priorityMax; i++ {
				xdsB.priorityToLocalities[i].bg.close()
			}
			return true
		}
		// else can only be *pInUse == priority.
		return true
	}

	if s == connectivity.TransientFailure {
		// An update with state Failure:
		// - If it's from a higher priority:
		//   - Do not forward, and do nothing
		// - If it's from priorityInUse:
		//   - If there's no lower:
		//     - Forward and do nothing else
		//   - If there's a lower priority:
		//     - Forward
		//     - Set lower as priorityInUse
		//     - Start lower
		if *xdsB.priorityInUse > priority {
			return false
		}
		// else can only be *pInUse == priority. priorityInUse sends a failure.
		// Stop its init timer.
		if timer := xdsB.priorityInitTimer; timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		pNext := priority + 1
		if _, okNext := xdsB.priorityToLocalities[pNext]; !okNext {
			return true
		}
		xdsB.startPriority(pNext)
		return true
	}

	if s == connectivity.Connecting {
		// An update with state Connecting:
		// - If it's from a higher priority
		//   - Do nothing
		// - If it's from priorityInUse, the behavior depends on previous state.
		if *xdsB.priorityInUse > priority {
			return false
		}
		// else can only be *pInUse == priority, check next.

		// When new state is Connecting, the behavior depends on previous state.
		//
		// If the previous state was Ready, this is a transition out from Ready
		// to Connecting. Assuming there are multiple backends in the same
		// priority, this mean we are in a bad situation and we should failover
		// to the next priority (Side note: the current connectivity state
		// aggregating algorhtim (e.g. round-robin) is not handling this right,
		// because if many backends all go from Ready to Connecting, the overall
		// situation is more like TransientFailure, not Connecting).
		//
		// If the previous state was Idle, we don't do anything special with
		// failure, and simply forward the update. The init timer should be in
		// process, will handle failover if it timeouts.
		// If the previous state was TransientFailure, we do not forward,
		// because the lower priority is in use.
		switch oldState {
		case connectivity.Ready:
			pNext := priority + 1
			if _, okNext := xdsB.priorityToLocalities[pNext]; !okNext {
				return true
			}
			xdsB.startPriority(pNext)
			return true
		case connectivity.Idle:
			return true
		case connectivity.TransientFailure:
			return false
		}
	}

	// New state is Idle, should never happen. Don't forward.
	return false
}
