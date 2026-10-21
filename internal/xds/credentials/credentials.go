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

// Package credentials provides the credentials used for xDS-configured side
// channels (gRFC A102): buildable channel and call credentials paired with the
// identity of the configuration they were built from, and registries of
// credential parsers keyed by proto type URL.
package credentials

import (
	"bytes"
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
)

const (
	insecureCredsTypeURL      = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
	googleDefaultCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials"
	xdsCredsTypeURL           = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.xds.v3.XdsCredentials"
)

func init() {
	RegisterChannelCredsParser(insecureCredsTypeURL, func(config *anypb.Any, _ CertProviderConfigResolver) (BuildableChannelCreds, error) {
		return newChannelCreds(config, func() (credentials.Bundle, func(), error) { return insecure.NewBundle(), func() {}, nil }), nil
	})
	RegisterChannelCredsParser(googleDefaultCredsTypeURL, func(config *anypb.Any, _ CertProviderConfigResolver) (BuildableChannelCreds, error) {
		return newChannelCreds(config, func() (credentials.Bundle, func(), error) { return google.NewDefaultCredentials(), func() {}, nil }), nil
	})
	// Side channels have no xDS cluster security configuration. Resolve the
	// required fallback without building it, preserving the outer identity.
	RegisterChannelCredsParser(xdsCredsTypeURL, func(config *anypb.Any, resolver CertProviderConfigResolver) (BuildableChannelCreds, error) {
		var cfg xdscredspb.XdsCredentials
		if err := anypb.UnmarshalTo(config, &cfg, proto.UnmarshalOptions{}); err != nil {
			return nil, fmt.Errorf("credentials: failed to unmarshal XdsCredentials: %v", err)
		}
		fallback := cfg.GetFallbackCredentials()
		if fallback == nil {
			return nil, fmt.Errorf("credentials: xds credentials missing required fallback credentials")
		}
		parser := GetChannelCredsParser(fallback.GetTypeUrl())
		if parser == nil {
			return nil, fmt.Errorf("credentials: unsupported fallback credentials type %q in xds credentials", fallback.GetTypeUrl())
		}
		parsed, err := parser(fallback, resolver)
		if err != nil {
			return nil, err
		}
		return newChannelCreds(config, parsed.Build), nil
	})
}

// CertProviderConfigResolver resolves certificate provider instance names to
// their configuration. It is implemented by the bootstrap config, and
// declared here so that credential parsers do not depend on the bootstrap
// package.
type CertProviderConfigResolver interface {
	CertProviderConfigs() map[string]*certprovider.BuildableConfig
}

// BuildableChannelCreds is validated channel credential configuration.
// Parsed configurations hold no acquired resources. Bootstrap adapters borrow
// credentials whose lifetime remains owned by bootstrap.
// Build acquires resources for one channel; the returned function releases
// them after the channel closes. Build must clean up partial acquisitions on error.
// Each successful Build must be released independently, even when providers
// are shared internally.
type BuildableChannelCreds interface {
	Identity() Identity
	Build() (credentials.Bundle, func(), error)
}

// BuildableCallCreds is validated call credential configuration, with the same
// acquisition and release contract as BuildableChannelCreds.
type BuildableCallCreds interface {
	Identity() Identity
	Build() (credentials.PerRPCCredentials, func(), error)
}

// ChannelCredsParser validates a plugin and resolves bootstrap references.
// It must not acquire resources.
type ChannelCredsParser func(*anypb.Any, CertProviderConfigResolver) (BuildableChannelCreds, error)

// CallCredsParser validates a call credentials plugin without acquiring resources.
type CallCredsParser func(*anypb.Any) (BuildableCallCreds, error)

var (
	// channelCredsParsers is a map from proto type URL to
	// ChannelCredsParser.
	channelCredsParsers = make(map[string]ChannelCredsParser)
	// callCredsParsers is a map from proto type URL to CallCredsParser.
	callCredsParsers = make(map[string]CallCredsParser)
)

// RegisterChannelCredsParser registers the parser for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterChannelCredsParser(typeURL string, b ChannelCredsParser) {
	channelCredsParsers[typeURL] = b
}

// GetChannelCredsParser returns the parser registered for the given proto
// type URL, or nil if there is none.
func GetChannelCredsParser(typeURL string) ChannelCredsParser {
	return channelCredsParsers[typeURL]
}

// RegisterCallCredsParser registers the parser for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterCallCredsParser(typeURL string, b CallCredsParser) {
	callCredsParsers[typeURL] = b
}

// GetCallCredsParser returns the parser registered for the given proto type
// URL, or nil if there is none.
func GetCallCredsParser(typeURL string) CallCredsParser {
	return callCredsParsers[typeURL]
}

// Identity identifies the configuration a credential was built from.
type Identity struct {
	// Type is the bootstrap credential type name for JSON-sourced
	// credentials, or the proto type URL for proto-sourced ones. The two
	// namespaces cannot collide: proto type URLs contain dots and slashes.
	Type string
	// Data is the raw JSON configuration, or the proto config value bytes.
	Data []byte
}

// Equal reports whether i and other describe the same configuration.
func (i Identity) Equal(other Identity) bool {
	return i.Type == other.Type && bytes.Equal(i.Data, other.Data)
}

// ChannelCreds is buildable channel credential configuration.
type ChannelCreds struct {
	identity Identity
	build    func() (credentials.Bundle, func(), error)
}

func protoIdentity(config *anypb.Any) Identity {
	return Identity{Type: config.GetTypeUrl(), Data: bytes.Clone(config.GetValue())}
}

func newChannelCreds(config *anypb.Any, build func() (credentials.Bundle, func(), error)) *ChannelCreds {
	return &ChannelCreds{identity: protoIdentity(config), build: build}
}

// NewChannelCreds adapts a bundle owned by bootstrap. Build borrows the bundle
// and returns a no-op release; bootstrap remains responsible for cleanup.
func NewChannelCreds(bundle credentials.Bundle, identity Identity) *ChannelCreds {
	identity.Data = bytes.Clone(identity.Data)
	return &ChannelCreds{identity: identity, build: func() (credentials.Bundle, func(), error) {
		return bundle, func() {}, nil
	}}
}

// Build acquires credentials for a channel.
func (c *ChannelCreds) Build() (credentials.Bundle, func(), error) { return c.build() }

// Identity returns the source configuration identity.
func (c *ChannelCreds) Identity() Identity {
	return Identity{Type: c.identity.Type, Data: bytes.Clone(c.identity.Data)}
}

// Equal compares credential configuration identities.
func (c *ChannelCreds) Equal(other *ChannelCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// CallCreds is buildable call credential configuration.
type CallCreds struct {
	identity Identity
	build    func() (credentials.PerRPCCredentials, func(), error)
}

// NewCallCreds adapts call credentials owned by bootstrap. Build borrows them
// and returns a no-op release.
func NewCallCreds(creds credentials.PerRPCCredentials, identity Identity) *CallCreds {
	identity.Data = bytes.Clone(identity.Data)
	return &CallCreds{identity: identity, build: func() (credentials.PerRPCCredentials, func(), error) {
		return creds, func() {}, nil
	}}
}

// Build acquires call credentials for a channel.
func (c *CallCreds) Build() (credentials.PerRPCCredentials, func(), error) { return c.build() }

// Identity returns the source configuration identity.
func (c *CallCreds) Identity() Identity {
	return Identity{Type: c.identity.Type, Data: bytes.Clone(c.identity.Data)}
}

// Equal compares credential configuration identities.
func (c *CallCreds) Equal(other *CallCreds) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.identity.Equal(other.identity)
}

// EqualChannelCreds compares buildable channel credential identities.
func EqualChannelCreds(a, b BuildableChannelCreds) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Identity().Equal(b.Identity())
}

// EqualCallCreds compares buildable call credential identities.
func EqualCallCreds(a, b BuildableCallCreds) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Identity().Equal(b.Identity())
}
