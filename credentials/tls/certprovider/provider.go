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

// Package certprovider defines APIs for certificate providers in gRPC.
//
// Experimental
//
// Notice: All APIs in this package are experimental and may be removed in a
// later release.
package certprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
)

var (
	// errProviderClosed is returned by Distributor.KeyMaterial when it is
	// closed.
	errProviderClosed = errors.New("provider instance is closed")

	// m is a map from name to provider builder.
	m = make(map[string]Builder)
)

// Register registers the provider builder, whose name as returned by its Name()
// method will be used as the name registered with this builder. Registered
// Builders are used by the Store to create Providers.
func Register(b Builder) {
	m[b.Name()] = b
}

// getBuilder returns the provider builder registered with the given name.
// If no builder is registered with the provided name, nil will be returned.
func getBuilder(name string) Builder {
	if b, ok := m[name]; ok {
		return b
	}
	return nil
}

// Builder creates a Provider.
type Builder interface {
	// Build creates a new provider with the provided config.
	Build(StableConfig) Provider

	// ParseConfig converts config input in a format specific to individual
	// implementations and returns an implementation of the StableConfig
	// interface.
	// Equivalent configurations must return StableConfig types whose
	// Canonical() method returns the same output.
	ParseConfig(interface{}) (StableConfig, error)

	// Name returns the name of providers built by this builder.
	Name() string
}

// StableConfig wraps the method to return a stable provider configuration.
type StableConfig interface {
	// Canonical returns provider config as an arbitrary byte slice.
	// Equivalent configurations must return the same output.
	Canonical() []byte
}

// Provider makes it possible to keep channel credential implementations up to
// date with secrets that they rely on to secure communications on the
// underlying channel.
//
// Provider implementations are free to rely on local or remote sources to fetch
// the latest secrets, and free to share any state between different
// instantiations as they deem fit.
type Provider interface {
	// KeyMaterial returns the key material sourced by the provider.
	// Callers are expected to use the returned value as read-only.
	KeyMaterial(ctx context.Context) (*KeyMaterial, error)

	// Close cleans up resources allocated by the provider.
	Close()
}

// KeyMaterial wraps the certificates and keys returned by a provider instance.
type KeyMaterial struct {
	// Certs contains a slice of cert/key pairs used to prove local identity.
	Certs []tls.Certificate
	// Roots contains the set of trusted roots to validate the peer's identity.
	Roots *x509.CertPool
}
