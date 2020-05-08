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

// Package secrets defines APIs for dynamic fetching of secrets in gRPC.
//
// All APIs in this package are experimental.
package async

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"strings"
)

// m is a map from name to plugin builder.
var m = make(map[string]Builder)

// Register registers the plugin builder to the plugin map.
func Register(b Builder) {
	m[strings.ToLower(b.Name())] = b
}

// Get returns the plugin builder registered with the given name.
func Get(name string) Builder {
	if b, ok := m[strings.ToLower(name)]; ok {
		return b
	}
	return nil
}

type BuildOptions struct {
	PluginConfig json.RawMessage
}

// Builder creates a Plugin.
type Builder interface {
	// Build creates a new plugin with the provided opts.
	Build(opts BuildOptions) Plugin
	// Name returns the name of plugins built by this builder.
	Name() string
}

// Secrets contains the local certificate (along with the associated private
// key material) and the set of trusted root certs to validate the peer.
type Secrets struct {
	cert  *tls.Certificate
	roots *x509.CertPool
}

type FetchCallback func(Secrets, error)

// FetchOpts contains parameters which can only be specified during the call to
// FetchSecrets and cannot be passed through the config.
type FetchOptions struct {
	Cb FetchCallback
}

type Plugin interface {
	// UpdateConfig passes new configuration to the plugin.
	//
	// Implementations should not block while handling new configurations.
	UpdateConfig(config json.RawMessage) error

	// Close cleans up resources allocated by the plugin.
	Close() error

	// FetchSecrets returns dynamic secrets fetched by the plugin through the
	// callback provided in opts.
	//
	// Implementations must honor the deadline specified in context.
	FetchSecrets(ctx context.Context, opts FetchOptions) (Secrets, error)
}
