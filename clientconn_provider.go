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

package grpc

import (
	"reflect"

	"google.golang.org/grpc/resolver"
)

// ClientConnProvider is a factory to create a ClientConnInterface for the given
// key.
//
// Note: Callers must invoke cancel once they are done using cc.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
type ClientConnProvider func(key string) (cc ClientConnInterface, cancel func(), err error)

// WithClientConnProvider returns a DialOption that makes the passed in
// ClientConnProvider available to LB policies.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
func WithClientConnProvider(ccp ClientConnProvider) DialOption {
	return newFuncDialOption(func(o *dialOptions) {
		o.clientConnProvider = ccp
	})
}

type clientConnProviderKey struct{}

// clientConnProviderVal is a wrapper around ClientConnProvider to implement
// the Equal method for comparison in attributes.
type clientConnProviderVal struct {
	ccp ClientConnProvider
}

// Equal allows the values to be compared by Attributes.Equal.
func (v clientConnProviderVal) Equal(o any) bool {
	// A direct comparison of function values is not allowed in Go, so we use
	// reflection to compare the pointers of the function values.
	// Note: This only compares the underlying code pointers. Two different
	// instances of the same closure capturing different variables will compare
	// equal.
	ov, ok := o.(clientConnProviderVal)
	return ok && reflect.ValueOf(v.ccp).Pointer() == reflect.ValueOf(ov.ccp).Pointer()
}

// ClientConnProviderFromResolverState returns a ClientConnProvider from the
// given resolver state, or nil if not present.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
func ClientConnProviderFromResolverState(state resolver.State) ClientConnProvider {
	v, ok := state.Attributes.Value(clientConnProviderKey{}).(clientConnProviderVal)
	if !ok {
		return nil
	}
	return v.ccp
}

// SetClientConnProvider returns a copy of the resolver state with the
// ClientConnProvider set as an attribute.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
func SetClientConnProvider(state resolver.State, ccp ClientConnProvider) resolver.State {
	state.Attributes = state.Attributes.WithValue(clientConnProviderKey{}, clientConnProviderVal{ccp: ccp})
	return state
}
