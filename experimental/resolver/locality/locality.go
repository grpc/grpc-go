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

// Package locality contains utilities to set and get the locality attribute
// from a resolver.State.
//
// # Experimental
//
// Notice: All APIs in this package are EXPERIMENTAL and may be changed
// or removed in a later release.
package locality

import "google.golang.org/grpc/resolver"

type localityKey struct{}

// Set returns a copy of the given resolver.State with the locality attribute
// set. If loc is empty, the state is returned unmodified.
func Set(state resolver.State, loc string) resolver.State {
	if loc == "" {
		return state
	}
	state.Attributes = state.Attributes.WithValue(localityKey{}, loc)
	return state
}

// FromResolverState returns the locality attribute of the resolver.State. If
// this attribute is not set, it returns the empty string.
func FromResolverState(state resolver.State) string {
	l, _ := state.Attributes.Value(localityKey{}).(string)
	return l
}
