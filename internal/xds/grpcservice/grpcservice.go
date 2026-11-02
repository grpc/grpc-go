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

// Package grpcservice parses and validates envoy GrpcService protos into a
// form usable for creating side-channel gRPC connections.
package grpcservice

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	maxHeaderKeyLen   = 16384
	maxHeaderValueLen = 16384
)

// Config is the validated form of a GrpcService proto. It owns no resources
// and may be shared or discarded without cleanup. Callers must not mutate it.
type Config struct {
	// TargetURI is the gRPC target URI of the side-channel service.
	TargetURI string
	// Timeout, if non-zero, is the deadline to use for RPCs on the side
	// channel.
	Timeout time.Duration
	// InitialMetadata is the metadata to add to RPCs on the side channel.
	InitialMetadata metadata.MD
	// ChannelCredentials are the channel credentials to create the side
	// channel with, paired with the identity of their source configuration.
	ChannelCredentials xdscreds.BuildableChannelCreds
	// CallCredentials are the call credentials to apply to RPCs sent on the
	// side channel, paired with the identities of their source
	// configurations, preserving order.
	CallCredentials []xdscreds.BuildableCallCreds
}

// Key returns a string that uniquely identifies the config , suitable for use
// as a map key. Two configs return the same value from Key if and only if they
// are Equal.
func (c *Config) Key() string {
	// identityKey mirrors xdscreds.Identity. omitempty makes nil and empty
	// Data encode the same way.
	type identityKey struct {
		Type string
		Data []byte `json:",omitempty"`
	}
	toKey := func(id xdscreds.Identity) identityKey {
		return identityKey{Type: id.Type, Data: id.Data}
	}
	// Metadata values are encoded as []byte, which encoding/json renders as
	// base64. Encoding them as strings would coerce invalid UTF-8 (allowed in
	// "-bin" values) to U+FFFD, mapping distinct values to the same key.
	md := make(map[string][][]byte, len(c.InitialMetadata))
	for k, vs := range c.InitialMetadata {
		bs := make([][]byte, len(vs))
		for i, v := range vs {
			bs[i] = []byte(v)
		}
		md[k] = bs
	}
	callCreds := make([]identityKey, len(c.CallCredentials))
	for i, cc := range c.CallCredentials {
		callCreds[i] = toKey(cc.Identity())
	}
	key := &struct {
		TargetURI       string              `json:"TargetURI"`
		Timeout         time.Duration       `json:"Timeout,omitempty"`
		InitialMetadata map[string][][]byte `json:"InitialMetadata,omitempty"`
		ChannelCreds    identityKey         `json:"ChannelCreds"`
		CallCreds       []identityKey       `json:"CallCreds,omitempty"`
	}{
		TargetURI:       c.TargetURI,
		Timeout:         c.Timeout,
		InitialMetadata: md,
		ChannelCreds:    toKey(c.ChannelCredentials.Identity()),
		CallCreds:       callCreds,
	}
	// Marshal cannot fail: the key contains only strings, integers, and byte
	// slices, and maps and slices of those.
	res, _ := json.Marshal(key)
	return string(res)
}

// Equal reports whether c and other are equal.
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	targetEqual := c.TargetURI == other.TargetURI
	timeoutEqual := c.Timeout == other.Timeout
	metadataEqual := maps.EqualFunc(c.InitialMetadata, other.InitialMetadata, slices.Equal)
	channelCredsEqual := xdscreds.EqualChannelCreds(c.ChannelCredentials, other.ChannelCredentials)
	callCredsEqual := slices.EqualFunc(c.CallCredentials, other.CallCredentials, xdscreds.EqualCallCreds)
	return targetEqual && timeoutEqual && metadataEqual && channelCredsEqual && callCredsEqual
}

// Dial creates a channel and acquires its credentials. The returned release
// function closes the channel before releasing its credentials. It is safe to
// call repeatedly or concurrently, and is the only way to close the channel.
// Failures release any credentials already acquired.
func (c *Config) Dial(opts ...grpc.DialOption) (_ grpc.ClientConnInterface, _ func(), err error) {
	bundle, release, err := c.ChannelCredentials.Build()
	if err != nil {
		return nil, nil, err
	}
	cleanups := []func(){release}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			if cleanups[i] != nil {
				cleanups[i]()
			}
		}
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	// Append credentials last so the supplied options cannot override them.
	dialOpts := make([]grpc.DialOption, 0, len(opts)+len(c.CallCredentials)+1)
	dialOpts = append(dialOpts, opts...)
	dialOpts = append(dialOpts, grpc.WithCredentialsBundle(bundle))
	for _, config := range c.CallCredentials {
		creds, release, err := config.Build()
		if err != nil {
			return nil, nil, err
		}
		cleanups = append(cleanups, release)
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(creds))
	}
	conn, err := grpc.NewClient(c.TargetURI, dialOpts...)
	if err != nil {
		return nil, nil, err
	}
	return conn, sync.OnceFunc(func() {
		conn.Close()
		cleanup()
	}), nil
}

// Parse parses and validates a GrpcService proto into a Config, applying the
// gRFC A102 trust policy. bc is the bootstrap configuration of the xDS client
// that received the resource, and sc is the configuration of the management
// server that sent it; both must be non-nil.
//
// Credentials configured in the proto are honored only when the delivering
// management server is trusted, i.e. configured with the trusted_xds_server
// server feature. For untrusted management servers the target must be present
// in the bootstrap allowed_grpc_services map, and the returned Config carries
// the credentials configured there.
//
// Parse validates credential configurations without acquiring resources.
// Credentials are built only when Dial creates a channel.
func Parse(gs *v3corepb.GrpcService, bc *bootstrap.Config, sc *bootstrap.ServerConfig) (*Config, error) {
	googleGrpc := gs.GetGoogleGrpc()
	if googleGrpc == nil {
		return nil, fmt.Errorf("grpcservice: only google_grpc GrpcService config is supported")
	}

	targetURI := googleGrpc.GetTargetUri()
	if targetURI == "" {
		return nil, fmt.Errorf("grpcservice: target_uri must be non-empty")
	}
	if err := validateTargetURI(targetURI); err != nil {
		return nil, err
	}

	cfg := &Config{TargetURI: targetURI}
	var err error
	if cfg.Timeout, err = parseTimeout(gs); err != nil {
		return nil, err
	}
	if cfg.InitialMetadata, err = parseInitialMetadata(gs.GetInitialMetadata()); err != nil {
		return nil, err
	}

	// Validate credentials without acquiring resources.
	if sc.ServerFeaturesTrustedXDSServer() {
		if cfg.ChannelCredentials, err = parseChannelCredentials(googleGrpc.GetChannelCredentialsPlugin(), bc); err != nil {
			return nil, err
		}
		if cfg.CallCredentials, err = parseCallCredentials(googleGrpc.GetCallCredentialsPlugin()); err != nil {
			return nil, err
		}
	} else {
		svc := bc.AllowedGRPCService(targetURI)
		if svc == nil {
			return nil, fmt.Errorf("grpcservice: target_uri %q is not present in allowed_grpc_services", targetURI)
		}
		// Allowlisted credentials remain owned by bootstrap. Their Build
		// methods return the shared credentials with no-op releases.
		cfg.ChannelCredentials, cfg.CallCredentials = svc.SideChannelCredentials()
	}
	return cfg, nil
}

// parseChannelCredentials validates the first supported channel plugin.
// A malformed supported plugin is an error, not a reason to try the next one.
func parseChannelCredentials(plugins []*anypb.Any, bc *bootstrap.Config) (xdscreds.BuildableChannelCreds, error) {
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		parser := xdscreds.GetChannelCredsParser(plugin.GetTypeUrl())
		if parser == nil {
			continue
		}
		parsed, err := parser(plugin, bc)
		if err != nil {
			return nil, fmt.Errorf("grpcservice: failed to parse channel credentials %q: %v", plugin.GetTypeUrl(), err)
		}
		return parsed, nil
	}
	return nil, fmt.Errorf("grpcservice: no supported channel credentials found in grpc_service")
}

// parseCallCredentials validates supported call plugins, preserving their order.
// Unsupported plugins are skipped. Call credentials are optional.
func parseCallCredentials(plugins []*anypb.Any) ([]xdscreds.BuildableCallCreds, error) {
	var out []xdscreds.BuildableCallCreds
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		parser := xdscreds.GetCallCredsParser(plugin.GetTypeUrl())
		if parser == nil {
			continue
		}
		parsed, err := parser(plugin)
		if err != nil {
			return nil, fmt.Errorf("grpcservice: failed to parse call credentials %q: %v", plugin.GetTypeUrl(), err)
		}
		out = append(out, parsed)
	}
	return out, nil
}

// validateTargetURI verifies that the target URI can be handled by a
// registered resolver.
func validateTargetURI(targetURI string) error {
	// Mirror the scheme resolution performed by grpc.NewClient: use the
	// target's scheme if it parses and is registered; otherwise fall back
	// to the default scheme with the whole target as the endpoint.
	if u, err := url.Parse(targetURI); err == nil && resolver.Get(u.Scheme) != nil {
		return nil
	}
	canonicalTarget := resolver.GetDefaultScheme() + ":///" + targetURI
	u, err := url.Parse(canonicalTarget)
	if err != nil {
		return fmt.Errorf("grpcservice: target_uri %q is invalid: %v", targetURI, err)
	}
	if resolver.Get(u.Scheme) == nil {
		return fmt.Errorf("grpcservice: no resolver for default scheme %q", u.Scheme)
	}
	return nil
}

// parseTimeout validates and converts the GrpcService timeout. A zero timeout
// (unset) is allowed; any set value must be strictly positive.
func parseTimeout(gs *v3corepb.GrpcService) (time.Duration, error) {
	d := gs.GetTimeout()
	if d == nil {
		return 0, nil
	}
	if err := d.CheckValid(); err != nil {
		return 0, fmt.Errorf("grpcservice: invalid timeout: %v", err)
	}
	timeout := d.AsDuration()
	if timeout <= 0 {
		return 0, fmt.Errorf("grpcservice: timeout must be strictly positive, got %v", timeout)
	}
	return timeout, nil
}

// parseInitialMetadata validates the HeaderValue protos and returns them as
// metadata.MD, preserving the control-plane order for each key.
func parseInitialMetadata(headers []*v3corepb.HeaderValue) (metadata.MD, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	md := metadata.MD{}
	for _, h := range headers {
		key := h.GetKey()
		// raw_value takes precedence over the legacy value field.
		val := h.GetValue()
		if len(h.GetRawValue()) > 0 {
			val = string(h.GetRawValue())
		}
		if err := validateHeaderKey(key); err != nil {
			return nil, fmt.Errorf("grpcservice: invalid header key %q: %v", key, err)
		}
		if err := validateHeaderValue(key, val); err != nil {
			return nil, fmt.Errorf("grpcservice: invalid value for header key %q: %v", key, err)
		}
		md.Append(key, val)
	}
	return md, nil
}

func validateHeaderKey(key string) error {
	switch {
	case len(key) > maxHeaderKeyLen:
		return fmt.Errorf("header key exceeds maximum allowed length of %d", maxHeaderKeyLen)
	case key == "host":
		return fmt.Errorf("header key cannot be %q", "host")
	case strings.HasPrefix(key, ":"):
		// imetadata.ValidateKey ignores pseudo-headers, but gRFC A102
		// requires them to be rejected.
		return fmt.Errorf("header key cannot start with %q", ":")
	case strings.HasPrefix(key, "grpc-"):
		return fmt.Errorf("header key cannot start with %q", "grpc-")
	}
	return imetadata.ValidateKey(key)
}

func validateHeaderValue(key, val string) error {
	if len(val) > maxHeaderValueLen {
		return fmt.Errorf("header value exceeds maximum allowed length of %d", maxHeaderValueLen)
	}
	// ValidatePair skips value validation for "-bin" keys, which may carry
	// arbitrary bytes.
	return imetadata.ValidatePair(key, val)
}
