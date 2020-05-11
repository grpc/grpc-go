package certprovider

import (
	"encoding/json"
	"fmt"
	"sync"
)

var store *Store

func init() {
	store = &Store{
		providers: make(map[Key]*wrappedProvider),
	}
}

// Key contains data which uniquely identifies a provider instance.
type Key struct {
	// Name is the registered name of the provider.
	Name string
	// Config is a string representation of the configuration used by a
	// particular instance of the provider.
	Config string
}

// wrappedProvider wraps a provider instance with the following additional
// functionality:
// - maintains a reference count to enable sharing and cleanup
// - creates a certificate distributor which is passed as a Sink to the
//   provider at creation time, and is returned as a Source to callers who are
//   interested in using the key material returned by the provider.
type wrappedProvider struct {
	provider Provider
	refCount int
	dist     *distributor
}

// Store is a collection of provider instances.
// It is safe for concurrent access.
type Store struct {
	mu        sync.Mutex
	providers map[Key]*wrappedProvider
}

// GetStore returns a global singleton store of provider instances.
func GetStore() *Store {
	return store
}

// GetProvider returns a source for key materials updated by the provider
// corresponding to key. If a provider exists for key, its reference count is
// incremented before returning. If no provider exists for key, a new is
// created using the registered builder. If no registered builder is found, a
// non-nil error is returned.
func (ps *Store) GetProvider(key Key) (Source, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if rcp, ok := ps.providers[key]; ok {
		rcp.refCount++
		return rcp.dist, nil
	}

	b := Get(key.Name)
	if b == nil {
		return nil, fmt.Errorf("unregistered certificate provider: %s", key.Name)
	}
	dist := &distributor{}
	provider, err := b.Build(BuildOptions{
		CertSink: dist,
		Config:   json.RawMessage([]byte(key.Config)),
	})
	if err != nil {
		return nil, err
	}
	rcp := &wrappedProvider{
		provider: provider,
		refCount: 1,
		dist:     dist,
	}
	ps.providers[key] = rcp
	return rcp.dist, nil
}

// ReleaseProvider releases the reference held by the caller on the provider
// instance identified by key. It decrements the reference count on the wrapped
// provider, and if the provider's reference count reaches zero, it is removed
// from the store, and its Close method is also invoked.
func (ps *Store) ReleaseProvider(key Key) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	rcp, ok := ps.providers[key]
	if !ok {
		return
	}

	rcp.refCount--
	if rcp.refCount == 0 {
		rcp.provider.Close()
		delete(ps.providers, key)
	}
}
