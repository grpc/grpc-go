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

package certprovider

import (
	"sync"

	"google.golang.org/grpc/grpclog"
)

// store is the global singleton certificate provider store.
var store *Store

func init() {
	store = &Store{
		providers: make(map[storeKey]*wrappedProvider),
	}
}

// Key contains data which uniquely identifies a provider instance.
type Key struct {
	// Name is the registered name of the provider.
	Name string
	// Config is the configuration used by the provider instance.
	Config StableConfig
}

// storeKey acts the key to the map of providers maintained by the store. Go
// maps need to be indexed by comparable types, so the above Key struct cannot
// be used since it contains an interface.
type storeKey struct {
	// name of the certificate provider.
	name string
	// configuration of the certificate provider in string form.
	config string
}

// wrappedProvider wraps a provider instance with a reference count.
type wrappedProvider struct {
	Provider
	refCount int

	// A reference to the key and store are also kept here to override the
	// Close method on the provider.
	storeKey storeKey
	store    *Store
}

// Store is a collection of provider instances, safe for concurrent access.
type Store struct {
	mu        sync.Mutex
	providers map[storeKey]*wrappedProvider
}

// GetStore returns a global singleton store of provider instances.
func GetStore() *Store {
	return store
}

// GetProvider returns a provider instance corresponding to key. If a provider
// exists for key, its reference count is incremented before returning. If no
// provider exists for key, a new is created using the registered builder. If
// no registered builder is found, a nil provider is returned.
func (ps *Store) GetProvider(key Key) Provider {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	sk := storeKey{
		name:   key.Name,
		config: string(key.Config.Canonical()),
	}
	if wp, ok := ps.providers[sk]; ok {
		wp.refCount++
		return wp
	}

	b := getBuilder(key.Name)
	if b == nil {
		return nil
	}
	provider := b.Build(key.Config)
	if provider == nil {
		grpclog.Errorf("certprovider.Build(%v) failed", sk)
		return nil
	}
	wp := &wrappedProvider{
		Provider: provider,
		refCount: 1,
		storeKey: sk,
		store:    ps,
	}
	ps.providers[sk] = wp
	return wp
}

// Close overrides the Close method of the embedded provider. It releases the
// reference held by the caller on the underlying provider and if the
// provider's reference count reaches zero, it is removed from the store, and
// its Close method is also invoked.
func (wp *wrappedProvider) Close() {
	ps := wp.store
	ps.mu.Lock()
	defer ps.mu.Unlock()

	wp.refCount--
	if wp.refCount == 0 {
		wp.Provider.Close()
		delete(ps.providers, wp.storeKey)
	}
}
