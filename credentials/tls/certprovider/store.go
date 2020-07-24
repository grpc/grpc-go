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

// provStore is the global singleton certificate provider store.
var provStore = &store{
	providers:    make(map[providerKey]*wrappedProvider),
	distributors: make(map[distributorKey]*wrappedDistributor),
}

// providerKey acts as the key to a map of providers maintained by the store. A
// combination of provider name and configuration is used to uniquely identify
// every provider instance in the store. Go maps need to be indexed by
// comparable types, so the provider configuration is converted from
// `interface{}` to string using the ParseConfig method while creating this key.
type providerKey struct {
	// name of the certificate provider.
	name string
	// configuration of the certificate provider in string form.
	config string
}

// distributorKey acts as the key to map of distributors maintained by the
// store. These distributor instances are returned by the provider
// implementation's GetKeyMaterial() method.
type distributorKey struct {
	// name of the certificate provider.
	name string
	// configuration of the certificate provider in string form.
	config string
	// options for reading key material.
	options KeyMaterialOptions
}

// store is a collection of provider instances, safe for concurrent access.
type store struct {
	mu           sync.Mutex
	providers    map[providerKey]*wrappedProvider
	distributors map[distributorKey]*wrappedDistributor
}

// GetKeyMaterialReader returns a reader to read key materials from.
//
// name is the registered name of the provider, config is the provider-specific
// configuration, and opts contains key material specific options.
//
// If no provider exists for the (name+config) combination in the store, a new
// one is created using the registered builder. If not, the existing provider is
// used. If no registered builder is found, or the provider configuration is
// rejected by it, a non-nil error is returned. If no distributor exists for the
// (name+config+opts) combo in the store, a new one is created by calling the
// KeyMaterialReader() method on the provider.
func GetKeyMaterialReader(name string, config interface{}, opts KeyMaterialOptions) (KeyMaterialReader, error) {
	provStore.mu.Lock()
	defer provStore.mu.Unlock()

	builder := getBuilder(name)
	if builder == nil {
		return nil, fmt.Errorf("no registered builder for provider name: %s", name)
	}
	stableConfig, err := builder.ParseConfig(config)
	if err != nil {
		return nil, err
	}
	pk := providerKey{
		name:   name,
		config: string(stableConfig.Canonical()),
	}

	// Create a new provider for the (name+config) combo or use an existing one.
	var wp *wrappedProvider
	wp, ok := provStore.providers[pk]
	if !ok {
		provider := builder.Build(stableConfig)
		if provider == nil {
			return nil, fmt.Errorf("certprovider.Build(%v) failed", pk)
		}
		wp = &wrappedProvider{
			prov:     provider,
			storeKey: pk,
			store:    provStore,
		}
		provStore.providers[pk] = wp
	}

	// Create a new distributor for the (name+config+opts) combo or use an
	// existing one. The reference count for the provider is incremented only
	// when creating a new distributor.
	dk := distributorKey{
		name:    name,
		config:  string(stableConfig.Canonical()),
		options: opts,
	}
	if wd, ok := provStore.distributors[dk]; ok {
		wd.refCount++
		return wd, nil
	}
	distributor, err := wp.prov.KeyMaterialReader(opts)
	if err != nil {
		return nil, err
	}
	wd := &wrappedDistributor{
		KeyMaterialReader: distributor,
		refCount:          1,
		wp:                wp,
		storeKey:          dk,
		store:             provStore,
	}
	provStore.distributors[dk] = wd
	wp.refCount++
	return wd, nil
}

// wrappedProvider wraps a provider instance with a reference count.
type wrappedProvider struct {
	prov Provider
	// refcount tracks the number of KeyMaterialReader instances (or
	// distributors) doled out by this provider.
	refCount int
	// A reference to the key and store are also kept here to invoke the
	// Close method on the provider, and to remove it from the store map.
	storeKey providerKey
	store    *store
}

// close releases the reference held by the caller on the underlying provider
// and if the provider's reference count reaches zero, it is removed from the
// store, and its Close method is also invoked.
//
// Caller must hold store mutex.
func (wp *wrappedProvider) close() {
	ps := wp.store
	wp.refCount--
	if wp.refCount == 0 {
		wp.prov.Close()
		delete(ps.providers, wp.storeKey)
	}
}

// wrappedDistributor wraps a distributor instance with a reference count.
type wrappedDistributor struct {
	KeyMaterialReader
	refCount int
	// The underlying provider which is the source for key material stored in
	// this distributor.
	wp *wrappedProvider
	// A reference to the key and store are also kept here to invoke the
	// Close method on the distributor, and to remove it from the store map.
	storeKey distributorKey
	store    *store
}

// Close overrides the underlying distributor's Close() method. It updates the
// reference count, and when it reaches zero, calls Close() on the underlying
// distributor, removes it from the store map, and also calls close() on the
// associated wrapped provider to update its reference count.
func (wd *wrappedDistributor) Close() {
	ps := wd.store
	ps.mu.Lock()
	defer ps.mu.Unlock()

	wd.refCount--
	if wd.refCount == 0 {
		wd.KeyMaterialReader.Close()
		delete(ps.distributors, wd.storeKey)
		wd.wp.close()
	}
}
