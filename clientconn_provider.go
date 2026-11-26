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
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
)

// ClientConnProvider is a factory to create a ClientConnInterface for the given
// key.
//
// Callers must invoke cancel once they are done using cc. The returned cc may
// be shared with other callers that requested the same key, and cancel only
// releases this caller's reference to it.
//
// When a ClientConnProvider is passed to WithClientConnProvider, gRPC ensures
// that ClientConns are shared: it invokes the provided function at most once
// for a given key while there are outstanding references to the ClientConn
// returned for that key, and invokes the cancel function returned by it once
// all references have been released. Hence, the key must uniquely identify
// all the parameters used to create the ClientConn.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a later
// release.
type ClientConnProvider func(key string) (cc ClientConnInterface, cancel func(), err error)

// WithClientConnProvider returns a DialOption that makes the passed in
// ClientConnProvider available to LB policies. See the documentation on
// ClientConnProvider for how ClientConns returned by it are shared.
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

// sharedClientConnProvider wraps a ClientConnProvider and ensures that callers
// requesting the same key share a single ClientConn, which is reference
// counted. The delegate provider is invoked to create a ClientConn for a key
// only when there is no ClientConn with outstanding references for that key,
// and the cancel function returned by it is invoked when the last reference is
// released. Errors returned by the delegate provider are not cached.
type sharedClientConnProvider struct {
	// The ClientConnProvider being wrapped, to which the creation of new
	// ClientConns is delegated.
	delegate ClientConnProvider

	// mu guards clientConns. It must not be held when decrementing the
	// reference count of a ClientConn in clientConns, since the cleanup
	// function that runs when the reference count drops to zero acquires mu.
	mu sync.Mutex
	// Map from key to the reference-counted ClientConn shared by all callers
	// that requested that key.
	clientConns map[string]*grpcsync.RefCounted[ClientConnInterface]
}

// newSharedClientConnProvider returns a ClientConnProvider that wraps the given
// provider and shares ClientConns between callers requesting the same key.
func newSharedClientConnProvider(delegate ClientConnProvider) ClientConnProvider {
	s := &sharedClientConnProvider{
		delegate:    delegate,
		clientConns: make(map[string]*grpcsync.RefCounted[ClientConnInterface]),
	}
	return s.get
}

func (s *sharedClientConnProvider) get(key string) (ClientConnInterface, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// TryIncrement fails if the reference count of the existing entry has
	// already dropped to zero, but its cleanup function has not yet removed it
	// from the map. In that case, a new ClientConn is created and replaces the
	// existing entry.
	rc, ok := s.clientConns[key]
	if !ok || !rc.TryIncrement() {
		cc, cancel, err := s.delegate(key)
		if err != nil {
			return nil, nil, err
		}
		var newRC *grpcsync.RefCounted[ClientConnInterface]
		newRC = grpcsync.NewRefCounted(cc, func() {
			s.mu.Lock()
			// Remove the entry only if it has not already been replaced.
			if s.clientConns[key] == newRC {
				delete(s.clientConns, key)
			}
			s.mu.Unlock()

			// Invoked without holding the lock, since closing the ClientConn
			// could block.
			if cancel != nil {
				cancel()
			}
		})
		rc = newRC
		s.clientConns[key] = rc
	}
	// The returned cancel function decrements the reference count without
	// holding mu (as required), and is idempotent.
	return rc.Value(), sync.OnceFunc(rc.Decrement), nil
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
