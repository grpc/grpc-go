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
	"fmt"
	"sync"
)

// store is the global singleton certificate provider store.
var store = &Store{
	providers: make(map[storeKey]*wrappedProvider),
}

// storeKey acts as the key to the map of providers maintained by the store. A
// combination of provider name and configuration is used to uniquely identify
// every provider instance in the store. Go maps need to be indexed by
// comparable types, so the provider configuration is converted from
// `interface{}` to string using the ParseConfig method while creating this key.
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
//
// Users should not create instances of this type directly, they should use
// GetStore() instead.
type Store struct {
	mu        sync.Mutex
	providers map[storeKey]*wrappedProvider
}

// GetStore returns a global singleton store of provider instances.
func GetStore() *Store {
	return store
}

// GetProvider returns a provider instance corresponding to name and config.
// name is the registered name of the provider instance while config is a
// provider specific configuration blob to configure a provider instance.
// Implementations of the Provider interface should clearly document the type of
// configuration accepted by them.
//
// If a provider exists for the (name+config) combination, its reference count
// is incremented before returning. If no provider exists for the (name+config)
// combination, a new one is created using the registered builder. If no
// registered builder is found, or the provider configuration blob is rejected
// by it, a non-nil error is returned.
func (ps *Store) GetProvider(name string, config interface{}) (Provider, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	builder := getBuilder(name)
	if builder == nil {
		return nil, fmt.Errorf("no registered builder for provider name: %s", name)
	}
	stableConfig, err := builder.ParseConfig(config)
	if err != nil {
		return nil, err
	}

	sk := storeKey{
		name:   name,
		config: string(stableConfig.Canonical()),
	}
	if wp, ok := ps.providers[sk]; ok {
		wp.refCount++
		return wp, nil
	}

	provider := builder.Build(stableConfig)
	if provider == nil {
		return nil, fmt.Errorf("certprovider.Build(%v) failed", sk)
	}
	wp := &wrappedProvider{
		Provider: provider,
		refCount: 1,
		storeKey: sk,
		store:    ps,
	}
	ps.providers[sk] = wp
	return wp, nil
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
