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
	"sync"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
)

// TODO: make this a environment variable?
const defaultInitTimeout = 10 * time.Second

// priorityBalancerGroup keeps a link list of balancer groups with priorities.
//
// The purpose of this struct is to manage priority. Each priorityBalancerGroup
// contains it's own balancerGroup, and a pointer to the priorityBalancerGroup
// with lower priority. This struct is responsible for starting/closing the
// priorityBalancerGroup with lower priority.
//
// Actions in priorityBalancerGroup are not synced. It relies on the underlying
// balancerGroup for that.
type priorityBalancerGroup struct {
	*balancerGroup
	cc balancer.ClientConn

	nextMu         sync.Mutex
	started        bool
	mainState      connectivity.State
	mainPicker     balancer.Picker
	mainInUse      bool
	nextPriorityBg *priorityBalancerGroup // BG with next lower priority.
	// initTimer needs to be under the same mutex as nextPriorityBG because its
	// value is synced with whether nextBG is started.
	initOnce    sync.Once
	initTimeout time.Duration
	initTimer   *time.Timer
}

func newPriorityBalancerGroup(bgcc balancer.ClientConn, bgLoadStore lrs.Store) *priorityBalancerGroup {
	ret := &priorityBalancerGroup{
		cc:          bgcc,
		initTimeout: defaultInitTimeout,
		mainInUse:   true,
	}
	ret.balancerGroup = newBalancerGroup(
		ret.wrapClientConn(bgcc),
		bgLoadStore,
	)
	return ret
}

func (pbg *priorityBalancerGroup) setNext(next *priorityBalancerGroup) {
	pbg.nextMu.Lock()
	defer pbg.nextMu.Unlock()

	pbg.nextPriorityBg = next
	if !pbg.started {
		return
	}

	if next == nil {
		// Send mainPicker if next is set to nil, and also if this pbg is
		// started. This means the lower priority was in effect before, and it
		// was just removed.
		pbg.cc.UpdateBalancerState(pbg.mainState, pbg.mainPicker)
		return
	}
	if pbg.started && pbg.mainState != connectivity.Ready {
		// Start next if pbg is started and is not READY.
		next.start()
	}
}

func (pbg *priorityBalancerGroup) start() {
	pbg.nextMu.Lock()
	pbg.started = true
	pbg.initOnce.Do(func() {
		pbg.initTimer = time.AfterFunc(pbg.initTimeout, pbg.handleInitTimeout)
	})
	pbg.nextMu.Unlock()
	pbg.balancerGroup.start()
}

func (pbg *priorityBalancerGroup) handleInitTimeout() {
	pbg.nextMu.Lock()
	// Check nil in case this races with updateBalancerState. If init is nil, it
	// means timer was stopped by another goroutine. That goroutine should
	// already started the next bg if necessary.
	if pbg.initTimer != nil {
		pbg.initTimer = nil
		if pbg.nextPriorityBg != nil {
			pbg.mainInUse = false
			pbg.nextPriorityBg.start()
		}
	}
	pbg.nextMu.Unlock()
}

func (pbg *priorityBalancerGroup) updateBalancerState(state connectivity.State, picker balancer.Picker) {
	pbg.nextMu.Lock()
	defer pbg.nextMu.Unlock()
	if pbg.syncBalancerState(state, picker) {
		pbg.cc.UpdateBalancerState(state, picker)
	}
}

func (pbg *priorityBalancerGroup) syncBalancerState(state connectivity.State, picker balancer.Picker) (forwardUpdate bool) {
	oldState := pbg.mainState
	pbg.mainState = state
	pbg.mainPicker = picker

	// There's no lower priority, always forward the updates.
	if pbg.nextPriorityBg == nil {
		pbg.mainInUse = true
		return true
	}

	if state == connectivity.Ready {
		// When new state is ready, stop the timer.
		if pbg.initTimer != nil {
			if !pbg.initTimer.Stop() {
				<-pbg.initTimer.C
			}
			pbg.initTimer = nil
		}
		// If new state is ready, use main, and forward the update.
		oldMainInUse := pbg.mainInUse
		pbg.mainInUse = true
		if !oldMainInUse {
			// If main balancer group was not in use, which mean next is in use.
			// Close the lower priority bg.
			pbg.nextPriorityBg.close()
		}
		return true
	}

	if state == connectivity.TransientFailure {
		// When new state is transient failure, stop the timer, because we will
		// start the lower bg now.
		if pbg.initTimer != nil {
			if !pbg.initTimer.Stop() {
				<-pbg.initTimer.C
			}
			pbg.initTimer = nil
		}
		// If new state is failure, not use main, and not forward.
		oldMainInUse := pbg.mainInUse
		pbg.mainInUse = false
		if oldMainInUse {
			// If main balancer group was in use, which mean next is not
			// started. Start the lower priority bg.
			pbg.nextPriorityBg.start()
		}
		return false
	}

	// When new state is Connecting, the behavior depends on previous state.
	//
	// If the previous state was Ready, this is a transition out from Ready to
	// connecting. Assuming there are multiple backends in the same priority,
	// this mean we are in a bad situation and we should failover to the next
	// priority (Side note: the current connectivity state aggregating algorhtim
	// (e.g. round-robin) is not handling this right, because if many backends
	// all go from Ready to Connecting, the overall situation is more like
	// TransientFailure, not Connecting).
	//
	// If the previous state was Idle, we don't do anything special with
	// failure, and simply forward the update. The init timer should be in
	// process, will handle failover if it timeouts.
	//
	// If the previous state was TransientFailure, we do not forward, because
	// the lower priority is in use.
	if state == connectivity.Connecting {
		switch oldState {
		case connectivity.Ready:
			pbg.mainInUse = false
			pbg.nextPriorityBg.start()
			return false
		case connectivity.Idle:
			return true
		case connectivity.TransientFailure:
			return false
		}
	}

	// It should never arrive here, because this means the new state is Idle, or
	// it was a connecing->connecting transition. mainInUse doesn't change. And
	// forward the update if main is in use.
	return pbg.mainInUse
}

// We need this to monitor the state of main bg, so to start/close next pbg.
func (pbg *priorityBalancerGroup) wrapClientConn(cc balancer.ClientConn) *priorityBalancerGroupCC {
	return &priorityBalancerGroupCC{
		ClientConn: cc,
		pbg:        pbg,
	}
}

func (pbg *priorityBalancerGroup) close() {
	pbg.balancerGroup.close()
	pbg.nextMu.Lock()
	if pbg.nextPriorityBg != nil {
		// Close next priority.
		pbg.nextPriorityBg.close()
	}
	pbg.started = false
	pbg.nextMu.Unlock()
}

type priorityBalancerGroupCC struct {
	balancer.ClientConn
	pbg *priorityBalancerGroup
}

func (pbgcc *priorityBalancerGroupCC) UpdateBalancerState(state connectivity.State, picker balancer.Picker) {
	pbgcc.pbg.updateBalancerState(state, picker)
}
