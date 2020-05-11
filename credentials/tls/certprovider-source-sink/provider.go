package certprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"strings"
	"sync"
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
	// Sink provides a way for the provider to furnish updated key material.
	CertSink Sink

	// Config holds provider specific configuration in JSON form.
	Config json.RawMessage
}

// Builder creates a Provider.
type Builder interface {
	// Build creates a new provider with opts.
	Build(BuildOptions) (Provider, error)

	// ParseConfig converts config input in a format specific to individual
	// implementations and returns a JSON representation of the same.
	// Implementations should produce stable output.
	ParseConfig(interface{}) (json.RawMessage, error)

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
	// Close cleans up resources allocated by the provider.
	Close()
}

// KeyMaterial wraps the certificates and keys returned by a provider instance.
type KeyMaterial struct {
	// Certs contains a slice of cert/key pairs used to prove local identity.
	Certs []*tls.Certificate
	// Roots contains the set of trusted roots to validate the peer's identity.
	Roots *x509.CertPool
}

// Sink wraps the method used by provider implementations to furnish updated
// key material.
type Sink interface {
	// SetKeyMaterial updates both the local and root certificates.
	SetKeyMaterial(km KeyMaterial)
}

// FetchOptions contains additional parameters to configure the Fetch call. It
// is empty for now, and can be extended in the future.
type FetchOptions struct {
}

// Source wraps the method which allows fetching of key materials.
type Source interface {
	// Fetch returns the key material associated with the cert source.
	//
	// Implementations must honor the deadline specified in ctx.
	Fetch(ctx context.Context, opts FetchOptions) KeyMaterial
}

// distributor provides synchronized access to underlying key material by
// implementing both the Sink and the Source interface.
type distributor struct {
	mu    sync.Mutex
	certs []*tls.Certificate
	roots *x509.CertPool
}

func (d *distributor) SetKeyMaterial(km KeyMaterial) {
	d.mu.Lock()
	d.certs = km.Certs
	d.roots = km.Roots
	d.mu.Unlock()
}

func (d *distributor) Fetch(ctx context.Context, opts FetchOptions) KeyMaterial {
	// Handle deadline in ctx.
	d.mu.Lock()
	km := KeyMaterial{
		Certs: d.certs,
		Roots: d.roots,
	}
	d.mu.Unlock()
	return km
}
