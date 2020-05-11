package certprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
)

// m is a map from name to provider builder.
var m = make(map[string]Builder)

// Register registers the provider builder, whose name as returned by its
// Name() method will be used as the name registered with this builder.
func Register(b Builder) {
	m[strings.ToLower(b.Name())] = b
}

// Get returns the provider builder registered with the given name.
// If no builder is registered with the provided name, nil will be returned.
func Get(name string) Builder {
	if b, ok := m[strings.ToLower(name)]; ok {
		return b
	}
	return nil
}

// BuildOptions contains additional information for Build.
type BuildOptions struct {
	// Config holds provider specific configuration.
	Config StableConfig
}

// StableConfig wraps the method to return a stable provider configuration.
//
// Provider implementations must implement this interface.
type StableConfig interface {
	// Canonical returns provider config as an arbitrary byte slice. Repeated
	// invocations must return the same contents.
	Canonical() []byte
}

// Builder creates a Provider.
type Builder interface {
	// Build creates a new provider with opts.
	Build(BuildOptions) (Provider, error)

	// ParseConfig converts config input in a format specific to individual
	// implementations and returns an implementation of the StableConfig
	// interface.
	ParseConfig(interface{}) (StableConfig, error)

	// Name returns the name of providers built by this builder.
	Name() string
}

// Provider makes it possible to keep channel credential implementations up to
// date with secrets that they rely on to secure communications on the
// underlying channel.
//
// Provider implementations are free to rely on local or remote sources to fetch
// the latest secrets, and free to share any state between different
// instantiations as they deem fit.
//
// Provider implementations are passed a sink interface at build time which
// they should use to furnish new key material.
type Provider interface {
	// Fetch returns the key material sourced by the provider.
	//
	// Implementations must honor the deadline specified in ctx.
	Fetch(ctx context.Context, opts FetchOptions) (KeyMaterial, error)

	// Close cleans up resources allocated by the provider.
	Close()
}

// FetchOptions contains additional parameters to configure the Fetch call. It
// is empty for now, and can be extended in the future.
type FetchOptions struct{}

// KeyMaterial wraps the certificates and keys returned by a provider instance.
type KeyMaterial struct {
	// Certs contains a slice of cert/key pairs used to prove local identity.
	Certs []*tls.Certificate
	// Roots contains the set of trusted roots to validate the peer's identity.
	Roots *x509.CertPool
}

// Distributor is a helper type which makes it easy for provider
// implementations to furnish new key materials.
//
// Provider implementations must embed this type into themselves, and whenever
// they have new key material, they should invoke the Set() method. When users
// of the provider call Fetch(), it will be handled by the distributor which
// will return the most up-to-date key material provided by the provider.
type Distributor struct {
	mu     sync.Mutex
	certs  []*tls.Certificate
	roots  *x509.CertPool
	ready  *grpcsync.Event
	closed *grpcsync.Event
}

// NewDistributor returns a new Distributor.
func NewDistributor() *Distributor {
	return &Distributor{
		ready:  grpcsync.NewEvent(),
		closed: grpcsync.NewEvent(),
	}
}

// Set updates the key material in the distributor with km.
func (d *Distributor) Set(km KeyMaterial) {
	d.mu.Lock()
	// TODO(easwars): Simple pointer copy will not do. We need a deep copy.
	d.certs = km.Certs
	d.roots = km.Roots
	d.ready.Fire()
	d.mu.Unlock()
}

// Fetch returns the most recent key material provided to the distributor. If
// no key material was provided to the distributor at the time of the fetch
// call, it will block till the deadline on the context expires or fresh key
// material arrives.
func (d *Distributor) Fetch(ctx context.Context, opts FetchOptions) (KeyMaterial, error) {
	if d.ready.HasFired() {
		d.mu.Lock()
		// TODO(easwars): Simple pointer copy will not do. We need a deep copy.
		km := KeyMaterial{Certs: d.certs, Roots: d.roots}
		d.mu.Unlock()
		return km, nil
	}

	select {
	case <-ctx.Done():
		return KeyMaterial{}, ctx.Err()
	case <-d.closed.Done():
		return KeyMaterial{}, errors.New("distributor closed before receiving any key material from the provider")
	case <-d.ready.Done():
		d.mu.Lock()
		// TODO(easwars): Simple pointer copy will not do. We need a deep copy.
		km := KeyMaterial{Certs: d.certs, Roots: d.roots}
		d.mu.Unlock()
		return km, nil
	}
}

// Shutdown releases resources allocated by the distributor and fails any
// active fetch call blocked on new key material.
func (d *Distributor) Shutdown() {
	d.closed.Fire()
}
