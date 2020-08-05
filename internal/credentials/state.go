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

// Package credentials declares grpclb types to be set by resolvers wishing to pass
// information to grpclb via State Attributes.
package credentials

// keyType is the key to use for storing State in Attributes.
type keyType string

const key = keyType("grpc.grpclb.state")

// State contains gRPCLB-relevant data passed from the name
type State struct {
	// BalancerAddresses contains the remote load balancer address(es).  If
	// set, overrides any resolver-provided addresses with Type of GRPCLB.
	BalancerAddresses []Address
}

// Set returns a copy of the provided state with attributes containing s.  s's
// data should not be mutated after calling Set.
func Set(state State, s *State) State {
	state.Attributes = state.Attributes.WithValues(key, s)
	return state
}

// Get returns the grpclb State in the State, or nil if not present.
// The returned data should not be mutated.
func Get(state State) *State {
	s, _ := state.Attributes.Value(key).(*State)
	return s
}
