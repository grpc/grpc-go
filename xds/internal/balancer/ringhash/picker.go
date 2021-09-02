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

package ringhash

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
)

type picker struct {
	ring *ring
}

func newPicker(ring *ring) *picker {
	return &picker{ring: ring}
}

// handleRICS generates pick result if the entry is in Ready, IDLE, Connecting
// or Shutdown. TransientFailure will be handled specifically after this
// function returns.
//
// The first return value indicates if the state is in Ready, IDLE, Connecting
// or Shutdown. If it's true, the PickResult and error should be returned from
// Pick() as is.
func handleRICS(e *ringEntry) (bool, balancer.PickResult, error) {
	switch state := e.sc.effectiveState(); state {
	case connectivity.Ready:
		return true, balancer.PickResult{SubConn: e.sc.sc}, nil
	case connectivity.Idle:
		// Trigger Connect() and queue the pick.
		e.sc.connect()
		return true, balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	case connectivity.Connecting:
		return true, balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	case connectivity.TransientFailure:
		// Return ok==false, so TransientFailure will be handled afterwards.
		return false, balancer.PickResult{}, nil
	case connectivity.Shutdown:
		// Shutdown can happen in a race where the old picker is called. A new
		// picker should already be sent.
		return true, balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	default:
		// Should never reach this. All the connectivity states are already
		// handled in the cases.
		return true, balancer.PickResult{}, status.Errorf(codes.Unavailable, "SubConn has undefined connectivity state: %v", state)
	}
}

func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	e := p.ring.pick(getRequestHash(info.Ctx))
	if ok, pr, err := handleRICS(e); ok {
		return pr, err
	}
	// ok was false, the entry is in transient failure.
	return p.handleTransientFailure(e)
}

func (p *picker) handleTransientFailure(e *ringEntry) (balancer.PickResult, error) {
	// Queue a connect on the first picked SubConn.
	e.sc.connect()

	// Find next entry in the ring, skipping duplicate SubConns.
	e2 := nextSkippingDuplicates(p.ring, e)
	if e2 == nil {
		// There's no next entry available, fail the pick.
		return balancer.PickResult{}, status.Errorf(codes.Unavailable, "the only SubConn is not Ready")
	}

	// For the second SubConn, also check Ready/IDLE/Connecting as if it's the
	// first entry.
	if ok, pr, err := handleRICS(e2); ok {
		return pr, err
	}

	// The second SubConn is also in TransientFailure. Queue a connect on it.
	e2.sc.connect()

	// If it gets here, this is after the second SubConn, and the second SubConn
	// was in TransientFailure.
	//
	// Loop over all other SubConns:
	// - If all SubConns so far are all TransientFailure, trigger Connect() on
	// the TransientFailure SubConns, and keep going.
	// - If there's one SubConn that's not in TransientFailure, keep checking
	// the remaining SubConns (in case there's a Ready, which will be returned),
	// but don't not trigger Connect() on the other SubConns.
	var firstNonFailedFound bool
	for ee := nextSkippingDuplicates(p.ring, e2); ee != e; ee = nextSkippingDuplicates(p.ring, ee) {
		scState := ee.sc.effectiveState()
		if scState == connectivity.Ready {
			return balancer.PickResult{SubConn: ee.sc.sc}, nil
		}
		if firstNonFailedFound {
			continue
		}
		if scState == connectivity.TransientFailure {
			// This will queue a connect.
			ee.sc.connect()
			continue
		}
		// This is a SubConn in a non-failure state. We continue to check the
		// other SubConns, but remember that there was a non-failed SubConn
		// seen. After this, Pick() will never trigger any SubConn to Connect().
		firstNonFailedFound = true
		if scState == connectivity.Idle {
			// This is the first non-failed SubConn, and it is in a real IDLE
			// state. Trigger it to Connect().
			ee.sc.connect()
		}
	}
	return balancer.PickResult{}, status.Errorf(codes.Unavailable, "no connection is Ready")
}

func nextSkippingDuplicates(ring *ring, cur *ringEntry) *ringEntry {
	for next := ring.next(cur); next != cur; next = ring.next(next) {
		if next.sc != cur.sc {
			return next
		}
	}
	// There's no qualifying next entry.
	return nil
}
