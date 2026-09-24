/*
 *
 * Copyright 2026 gRPC authors.
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

package autosharding

import (
	"context"
	"errors"
	"fmt"
	"maps"
	rand "math/rand/v2"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
)

var randIntN = rand.IntN

// pickerEndpoint holds the snapshot of an endpoint's state needed by the
// picker to route RPCs without synchronizing with the LB policy.
type pickerEndpoint struct {
	state    connectivity.State
	picker   balancer.Picker
	exitIdle func()
}

// picker routes RPCs to endpoints assigned to the matching key-range in the
// sliceMap, or to the fallback pool when configured.
type picker struct {
	sliceMap        *sliceMap
	endpoints       []pickerEndpoint // Ordered 1:1 by endpointState.index
	sliceInFallback []bool           // Precomputed per-slice fallback status
	cfg             *lbConfig
}

// newPicker constructs a new picker from the given endpointMap, sliceMap, and
// LB policy configuration.
func newPicker(em *endpointMap, sm *sliceMap, cfg *lbConfig) *picker {
	// Every endpoint in em.m has a unique index in the range [0, len(em.m)-1].
	// Placing each entry at endpoints[es.index] orders the slice by index
	// without needing to sort.
	endpoints := make([]pickerEndpoint, len(em.m))
	for es := range maps.Values(em.m) {
		endpoints[es.index] = pickerEndpoint{
			state:    es.childState.State.ConnectivityState,
			picker:   es.childState.State.Picker,
			exitIdle: es.childState.ExitIdle,
		}
	}

	sliceInFallback := make([]bool, len(sm.slices))
	for i, se := range sm.slices {
		sliceInFallback[i] = isPoolInFallback(se.endpoints, endpoints)
	}

	return &picker{
		sliceMap:        sm,
		endpoints:       endpoints,
		sliceInFallback: sliceInFallback,
		cfg:             cfg,
	}
}

// isPoolInFallback reports whether a slice's endpoint pool contains zero valid
// endpoints or has all of its assigned endpoints in TransientFailure.
func isPoolInFallback(indices []int, endpoints []pickerEndpoint) bool {
	for _, idx := range indices {
		if endpoints[idx].state != connectivity.TransientFailure {
			return false
		}
	}
	return true
}

// Pick selects an endpoint for the RPC based on the sharding key header in the
// outgoing request metadata.
func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	key := extractKeyFromMetadata(info.Ctx, p.cfg.KeyHeaderName)
	if key == nil {
		return balancer.PickResult{}, fmt.Errorf("autosharding: header %q not found in outgoing metadata", p.cfg.KeyHeaderName)
	}

	sliceIdx := p.sliceMap.lookup(key)

	// No assignment covers this key. This happens when the initial assignment
	// timeout has expired and no valid assignments have been received from the
	// sharding service.
	if sliceIdx < 0 {
		if p.cfg.EnableFallback {
			return p.pickFromEndpointIndices(p.sliceMap.fallbackPool, info)
		}
		return balancer.PickResult{}, errors.New("autosharding: no assignment available and fallback is disabled")
	}

	// If the matching slice is in fallback mode and fallback is enabled, route
	// using the fallback pool across all resolver endpoints.
	if p.sliceInFallback[sliceIdx] && p.cfg.EnableFallback {
		return p.pickFromEndpointIndices(p.sliceMap.fallbackPool, info)
	}

	// Delegate to the assigned endpoints for the matching key-range. When the
	// slice is in fallback because all its endpoints are in TransientFailure
	// and fallback is disabled, delegating here surfaces the child picker's
	// connection error.
	return p.pickFromEndpointIndices(p.sliceMap.slices[sliceIdx].endpoints, info)
}

// pickFromEndpointIndices selects an endpoint from the given slice of indices
// into p.endpoints by starting at a random position and scanning circularly.
func (p *picker) pickFromEndpointIndices(indices []int, info balancer.PickInfo) (balancer.PickResult, error) {
	if len(indices) == 0 {
		return balancer.PickResult{}, errors.New("autosharding: matching slice has no available endpoints")
	}

	firstIndex := randIntN(len(indices))
	requestedConnection := false
	foundConnecting := false

	for i := range len(indices) {
		epIdx := indices[(firstIndex+i)%len(indices)]
		ep := p.endpoints[epIdx]

		if ep.state == connectivity.Ready {
			return ep.picker.Pick(info)
		}

		if ep.state == connectivity.Connecting {
			foundConnecting = true
		}

		if !requestedConnection && ep.state == connectivity.Idle {
			ep.exitIdle()
			requestedConnection = true
		}
	}

	if requestedConnection || foundConnecting {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}

	// All endpoints in the pool are in TransientFailure. Delegate to the
	// initially selected endpoint's picker to return a detailed error message.
	firstEpIdx := indices[firstIndex]
	return p.endpoints[firstEpIdx].picker.Pick(info)
}

// extractKeyFromMetadata returns the sharding key stored under headerName in
// the outgoing context metadata, or nil if the header is not present.
func extractKeyFromMetadata(ctx context.Context, headerName string) []byte {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return nil
	}
	vals := md.Get(headerName)
	if len(vals) == 0 {
		return nil
	}
	return []byte(vals[0])
}
