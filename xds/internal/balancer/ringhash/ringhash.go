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

// Package ringhash implements the ringhash balancer.
package ringhash

import (
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
)

// Name is the name of the ring_hash balancer.
const Name = "ring_hash_experimental"

type subConn struct {
	addr string
	sc   balancer.SubConn

	mu sync.RWMutex
	// This is the actual state of this SubConn (as updated by the ClientConn).
	// The effective state can be different, see comment of attemptedToConnect.
	state connectivity.State
	// attemptedToConnect is whether this SubConn has attempted to connect ever.
	// So that only the initial Idle is Idle, after any attempt to connect,
	// following Idles are all TransientFailure.
	//
	// This affects the effective connectivity state of this SubConn, e.g. if
	// the actual state is Idle, but this SubConn has attempted to connect, the
	// effective state is TransientFailure.
	//
	// This is used in pick(). E.g. if a subConn is Idle, but has
	// attemptedToConnect as true, pick() will
	// - consider this SubConn as TransientFailure, and check the state of the
	// next SubConn.
	// - trigger Connect() (note that normally a SubConn in real
	// TransientFailure cannot Connect())
	//
	// Note this should only be set when updating the state (from Idle to
	// anything else), not when Connect() is called, because there's a small
	// window after the first Connect(), before the state switches to something
	// else.
	attemptedToConnect bool
	// connectQueued is true if a Connect() was queued for this SubConn while
	// it's not in Idle (most likely was in TransientFailure). A Connect() will
	// be triggered on this SubConn when it turns Idle.
	//
	// When connectivity state is updated to Idle for this SubConn, if
	// connectQueued is true, Connect() will be called on the SubConn.
	connectQueued bool
}

// SetState updates the state of this SubConn.
//
// It also handles the queued Connect(). If the new state is Idle, and a
// Connect() was queued, this SubConn will be triggered to Connect().
//
// FIXME: unexport this. It's exported so that staticcheck doesn't complain
// about unused functions.
func (sc *subConn) SetState(s connectivity.State) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	// Any state change to non-Idle means there was an attempt to connect.
	if s != connectivity.Idle {
		sc.attemptedToConnect = true
	}
	switch s {
	case connectivity.Idle:
		// Trigger Connect() if new state is Idle, and there is a queued connect.
		if sc.connectQueued {
			sc.connectQueued = false
			sc.sc.Connect()
		}
	case connectivity.Connecting, connectivity.Ready:
		// Clear connectQueued if the SubConn isn't failing. This state
		// transition is unlikely to happen, but handle this just in case.
		sc.connectQueued = false
	}
	sc.state = s
}

// effectiveState returns the effective state of this SubConn. It can be
// different from the actual state, e.g. Idle after any attempt to connect (any
// Idle other than the initial Idle) is considered TransientFailure.
func (sc *subConn) effectiveState() connectivity.State {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.state == connectivity.Idle && sc.attemptedToConnect {
		return connectivity.TransientFailure
	}
	return sc.state
}

// queueConnect sets a boolean so that when the SubConn state changes to Idle,
// it's Connect() will be triggered. If the SubConn state is already Idle, it
// will just call Connect().
func (sc *subConn) queueConnect() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.state == connectivity.Idle {
		sc.sc.Connect()
		return
	}
	// Queue this connect, and when this SubConn switches back to Idle (happens
	// after backoff in TransientFailure), it will Connect().
	sc.connectQueued = true
}
