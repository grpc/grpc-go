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

// Package statedata declares grpclb types to be set by resolvers wishing to
// pass information to grpclb via resolver.State Attributes.
package statedata

import (
	"google.golang.org/grpc/resolver"
)

// stateDataAttributesKey is the key to use for storing StateData in Attributes.
type sdKeyType string

const sdKey = sdKeyType("grpc.grpclb.state_data")

// StateData contains gRPCLB-relevant data passed from the name resolver.
type StateData struct {
	// BalancerAddresses contains the remote load balancer address(es).  If
	// set, overrides any resolver-provided addresses with Type of GRPCLB.
	BalancerAddresses []resolver.Address
}

// Set returns a copy of the provided state with attributes containing sd.
// sd's data should not be mutated after calling Set.
func Set(state resolver.State, sd *StateData) resolver.State {
	state.Attributes = state.Attributes.WithValues(sdKey, sd)
	return state
}

// Get returns the StateData in the resolver.State, or nil if not present.  The
// returned data should not be mutated.
func Get(state resolver.State) *StateData {
	sd, _ := state.Attributes.Value(sdKey).(*StateData)
	return sd
}
