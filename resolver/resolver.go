/*
 *
 * Copyright 2017 gRPC authors.
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

// Package resolver defines APIs for name resolution in gRPC.
// All APIs in this package are experimental.
package resolver

var (
	// m is a map from scheme to resolver builder.
	m map[string]Builder
	// defaultScheme is the default scheme to use.
	defaultScheme string
)

func init() {
	// TODO(bar) install dns resolver.
	m = make(map[string]Builder)
}

// Register registers the resolver builder to the resolver map.
// b.Scheme will be used as the scheme registered with this builder.
func Register(b Builder) {
	m[b.Scheme()] = b
}

// Get returns the resolver builder registered with the given scheme.
// If no builder is register with the scheme, the default scheme will
// be used.
// If the default scheme is not modified, "dns" will be the default
// scheme, and the preinstalled dns resolver will be used.
// If the default scheme is modified, and a resolver is registered with
// the scheme, that resolver will be returned.
// If the default scheme is modified, and no resolver is registered with
// the scheme, (nil, false) will be returned.
func Get(scheme string) (b Builder, ok bool) {
	b, ok = m[scheme]
	if ok {
		return
	}
	b, ok = m[defaultScheme]
	return
}

// SetDefaultScheme sets the default scheme that will be used.
// The default default scheme is "dns".
func SetDefaultScheme(scheme string) {
	defaultScheme = scheme
}

// AddressType indicates the address type returned by name resolution.
type AddressType uint8

const (
	// Backend indicates the address is for a backend server.
	Backend AddressType = iota
	// GRPCLB indicates the address is for a grpclb load balancer.
	GRPCLB
)

// Address represents a server the client connects to.
// This is the EXPERIMENTAL API and may be changed or extended in the future.
type Address struct {
	// Addr is the server address on which a connection will be established.
	Addr string
	// Type is the type of this address.
	Type AddressType
	// ServerName is the name of this address.
	// It's the name of the grpc load balancer, which will be used for authentication.
	ServerName string
	// Metadata is the information associated with Addr, which may be used
	// to make load balancing decision.
	Metadata interface{}
}

// BuildOption includes additional information for the builder to create
// the resolver.
type BuildOption struct {
}

// ClientConnection contains the callbacks for resolver to notify any updates
// to the gRPC ClientConn.
type ClientConnection interface {
	// NewAddress is called by resolver to notify ClientConnection a new list
	// of resolved addresses.
	// The address list should be the complete list of new addresses.
	// Two consecutive call with the same addresses will be noop. It's preferred
	// that resolver doesn't call this consecutively with non-updated addresses.
	NewAddress(addresses []Address)
	// NewServiceConfig is called by resolver to notify ClientConnection a new
	// service config. The service config should be provided as a json string.
	NewServiceConfig(serviceConfig string)
}

// Builder creates a resolver that will be used to watch name resolution updates.
type Builder interface {
	// Build creates a new resolver for the given target.
	Build(target string, cc ClientConnection, opts BuildOption) Resolver
	// Scheme returns the scheme supported by this resolver.
	// Scheme is defined at https://github.com/grpc/grpc/blob/master/doc/naming.md.
	Scheme() string
}

// ResolveNowOption includes additional information for ResolveNow.
type ResolveNowOption struct{}

// Resolver watches for the updates on the specified target.
// Updates include address updates and service config updates.
type Resolver interface {
	// ResolveNow will be called by gRPC to try to resolve the target name again.
	// It's just a hint, resolver can ignore this if it's not necessary.
	ResolveNow(ResolveNowOption)
	// Close closes the resolver.
	Close()
}
