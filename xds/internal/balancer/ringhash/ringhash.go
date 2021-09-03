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
	// attemptedToConnect is whether this SubConn has attempted to connect.
	//
	// This affect the effective connectivity state of this SubConn, e.g. if the
	// actual state is IDLE, but this SubConn has attempted to connect, the
	// effective state is TransientFailure.
	//
	// This is used in pick(). E.g. if a subConn is IDLE, but has
	// attemptedToConnect as true, pick() will
	// - consider this SubConn as TransientFailure, and check the state of the
	// next SubConn.
	// - trigger Connect() (note that normally a SubConn in real
	// TransientFailure cannot Connect())
	//
	// Note this should only be set when updating the state (from IDLE to
	// anything else), not when Connect() is called, because there's a small
	// window after the first Connect(), before the state switches to something
	// else.
	attemptedToConnect bool
	// connectQueued is true if a Connect() was queued for this SubConn while
	// it's not in IDLE (most likely was in TransientFailure). A Connect() will
	// be triggered on this SubConn when it turns IDLE.
	//
	// When connectivity state is updated to IDLE for this SubConn, if
	// connectQueued is true, Connect() will be called on the SubConn.
	connectQueued bool
}

// FIXME: uncomment this. This is commented out because staticcheck complains
// about unused functions. Keeping this here for completeness. Will be
// uncommented in a future PR.
//
// // setState updates the state of this SubConn.
// //
// // It also handles the queued Connect(). If the new state is IDLE, and a
// // Connect() was queued, this SubConn will be triggered to Connect().
// func (sc *subConn) setState(s connectivity.State) {
// 	sc.mu.Lock()
// 	defer sc.mu.Unlock()
// 	// Any state change means there was an attempt to connect.
// 	if s != connectivity.Idle {
// 		sc.attemptedToConnect = true
// 	}
// 	// Trigger Connect() if new state is IDLE, and there is a queued connect.
// 	if s == connectivity.Idle && sc.connectQueued {
// 		sc.connectQueued = false
// 		sc.sc.Connect()
// 	}
// 	sc.state = s
// }

// effectiveState returns the effective state of this SubConn. It can be
// different from the actual state, e.g. IDLE after any attempt to connect (any
// IDLE other than the initial IDLE) is considered TransientFailure.
func (sc *subConn) effectiveState() connectivity.State {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.state == connectivity.Idle && sc.attemptedToConnect {
		return connectivity.TransientFailure
	}
	return sc.state
}

func (sc *subConn) connect() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.state == connectivity.Idle {
		sc.sc.Connect()
		return
	}
	// Queue this connect, and when this SubConn switches back to IDLE (happens
	// after backoff in TransientFailure), it will Connect().
	sc.connectQueued = true
}
