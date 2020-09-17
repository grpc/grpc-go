/*
 *
 * Copyright 2020 gRPC authors.
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

package rls

import (
	"errors"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/cache"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	"google.golang.org/grpc/metadata"
)

var errRLSThrottled = errors.New("RLS call throttled at client side")

// RLS rlsPicker selects the subConn to be used for a particular RPC. It does
// not manage subConns directly and usually deletegates to pickers provided by
// child policies.
//
// The RLS LB policy creates a new rlsPicker object whenever its ServiceConfig
// is updated and provides a bunch of hooks for the rlsPicker to get the latest
// state that it can used to make its decision.
type rlsPicker struct {
	// The keyBuilder map used to generate RLS keys for the RPC. This is built
	// by the LB policy based on the received ServiceConfig.
	kbm keys.BuilderMap

	// The following hooks are setup by the LB policy to enable the rlsPicker to
	// access state stored in the policy. This approach has the following
	// advantages:
	// 1. The rlsPicker is loosely coupled with the LB policy in the sense that
	//    updates happening on the LB policy like the receipt of an RLS
	//    response, or an update to the default rlsPicker etc are not explicitly
	//    pushed to the rlsPicker, but are readily available to the rlsPicker
	//    when it invokes these hooks. And the LB policy takes care of
	//    synchronizing access to these shared state.
	// 2. It makes unit testing the rlsPicker easy since any number of these
	//    hooks could be overridden.

	// readCache is used to read from the data cache and the pending request
	// map in an atomic fashion. The first return parameter is the entry in the
	// data cache, and the second indicates whether an entry for the same key
	// is present in the pending cache.
	readCache func(cache.Key) (*cache.Entry, bool)
	// shouldThrottle decides if the current RPC should be throttled at the
	// client side. It uses an adaptive throttling algorithm.
	shouldThrottle func() bool
	// startRLS kicks off an RLS request in the background for the provided RPC
	// path and keyMap. An entry in the pending request map is created before
	// sending out the request and an entry in the data cache is created or
	// updated upon receipt of a response. See implementation in the LB policy
	// for details.
	startRLS func(string, keys.KeyMap)
	// defaultPick enables the rlsPicker to delegate the pick decision to the
	// rlsPicker returned by the child LB policy pointing to the default target
	// specified in the service config.
	defaultPick func(balancer.PickInfo) (balancer.PickResult, error)
}

// Pick makes the routing decision for every outbound RPC.
func (p *rlsPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	// For every incoming request, we first build the RLS keys using the
	// keyBuilder we received from the LB policy. If no metadata is present in
	// the context, we end up using an empty key.
	km := keys.KeyMap{}
	md, ok := metadata.FromOutgoingContext(info.Ctx)
	if ok {
		km = p.kbm.RLSKey(md, info.FullMethodName)
	}

	// We use the LB policy hook to read the data cache and the pending request
	// map (whether or not an entry exists) for the RPC path and the generated
	// RLS keys. We will end up kicking off an RLS request only if there is no
	// pending request for the current RPC path and keys, and either we didn't
	// find an entry in the data cache or the entry was stale and it wasn't in
	// backoff.
	startRequest := false
	now := time.Now()
	entry, pending := p.readCache(cache.Key{Path: info.FullMethodName, KeyMap: km.Str})
	if entry == nil {
		startRequest = true
	} else {
		entry.Mu.Lock()
		defer entry.Mu.Unlock()
		if entry.StaleTime.Before(now) && entry.BackoffTime.Before(now) {
			// This is the proactive cache refresh.
			startRequest = true
		}
	}

	if startRequest && !pending {
		if p.shouldThrottle() {
			// The entry doesn't exist or has expired and the new RLS request
			// has been throttled. Treat it as an error and delegate to default
			// pick, if one exists, or fail the pick.
			if entry == nil || entry.ExpiryTime.Before(now) {
				if p.defaultPick != nil {
					return p.defaultPick(info)
				}
				return balancer.PickResult{}, errRLSThrottled
			}
			// The proactive refresh has been throttled. Nothing to worry, just
			// keep using the existing entry.
		} else {
			p.startRLS(info.FullMethodName, km)
		}
	}

	if entry != nil {
		if entry.ExpiryTime.After(now) {
			// This is the jolly good case where we have found a valid entry in
			// the data cache. We delegate to the LB policy associated with
			// this cache entry.
			return entry.ChildPicker.Pick(info)
		} else if entry.BackoffTime.After(now) {
			// The entry has expired, but is in backoff. We delegate to the
			// default pick, if one exists, or return the error from the last
			// failed RLS request for this entry.
			if p.defaultPick != nil {
				return p.defaultPick(info)
			}
			return balancer.PickResult{}, entry.CallStatus
		}
	}

	// We get here only in the following cases:
	// * No data cache entry or expired entry, RLS request sent out
	// * No valid data cache entry and Pending cache entry exists
	// We need to queue to pick which will be handled once the RLS response is
	// received.
	return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
}
