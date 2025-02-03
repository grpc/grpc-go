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
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
)

type picker struct {
	ring   *ring
	logger *grpclog.PrefixLogger
	// endpointStates is a cache of endpoint connectivity states and pickers.
	// The cache is used to avoid locking the mutex for reading the state at RPC
	// time.
	endpointStates map[string]balancer.State // endpointState.firstAddr -> balancer.State
}

// newPicker generates a picker with the given ring. Callers must hold the
// mutex in the ringhash balancer to ensure the endpoint states are not updated
// when creating the picker. The picker copies the endpoint states over to avoid
// locking the mutex at RPC time. The picker should be re-generated every time
// an endpoint state is updated.
func newPicker(ring *ring, endpointStates *resolver.EndpointMap, logger *grpclog.PrefixLogger) *picker {
	states := make(map[string]balancer.State)
	for _, val := range endpointStates.Values() {
		epState := val.(*endpointState)
		states[epState.firstAddr] = epState.state
	}
	return &picker{ring: ring, logger: logger, endpointStates: states}
}

func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	e := p.ring.pick(getRequestHash(info.Ctx))
	ringSize := len(p.ring.items)
	// Per gRFC A61, because of sticky-TF with PickFirst's auto reconnect on TF,
	// we ignore all TF subchannels and find the first ring entry in READY,
	// CONNECTING or IDLE.  If that entry is in IDLE, we need to initiate a
	// connection. The idlePicker returned by the LazyLB or the new Pickfirst
	// should do this automatically.
	for i := 0; i < ringSize; i++ {
		index := (e.idx + i) % ringSize
		balState := p.balancerState(p.ring.items[index])
		switch balState.ConnectivityState {
		case connectivity.Ready, connectivity.Connecting, connectivity.Idle:
			return balState.Picker.Pick(info)
		case connectivity.TransientFailure:
		default:
			p.logger.Errorf("Found child balancer in unknown state: %v", balState.ConnectivityState)
		}
	}
	// All children are in transient failure. Return the first failure.
	return p.balancerState(e).Picker.Pick(info)
}

func (p *picker) balancerState(e *ringEntry) balancer.State {
	return p.endpointStates[e.firstAddr]
}
