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
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/grpcservice/creds"
	"google.golang.org/grpc/internal/xds/grpcservice/credsregistry"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	maxHeaderKeyLen   = 16384
	maxHeaderValueLen = 16384
)

// Config is the parsed form of a GrpcService proto. It carries the built,
// ready-to-use credentials for the side channel.
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
	ChannelCredentials *creds.ChannelCreds
	// CallCredentials are the call credentials to apply to RPCs sent on the
	// side channel, paired with the identities of their source
	// configurations, preserving order.
	CallCredentials []*creds.CallCreds
}

// Equal reports whether c and other describe the same side channel: the same
// target with the same channel and call credential identities. Timeout and
// initial metadata are applied per-RPC and intentionally do not affect
// channel sharing.
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.TargetURI == other.TargetURI &&
		c.ChannelCredentials.Equal(other.ChannelCredentials) &&
		slices.EqualFunc(c.CallCredentials, other.CallCredentials, (*creds.CallCreds).Equal)
}

// Close releases the credentials owned by the config. It is idempotent, and a
// no-op for credentials owned by another component (e.g. the allowlisted
// credentials owned by the bootstrap config).
func (c *Config) Close() {
	if c == nil {
		return
	}
	c.ChannelCredentials.Close()
	for _, cc := range c.CallCredentials {
		cc.Close()
	}
}

// Parse parses and validates a GrpcService proto into a Config, applying the
// gRFC A102 trust policy.
//
// Credentials configured in the proto are honored only when the xDS
// management server that delivered it is trusted, i.e. configured with the
// trusted_xds_server server feature; a nil server config means the delivering
// management server is unknown and is treated as untrusted. For untrusted
// management servers the target must be present in the bootstrap
// allowed_grpc_services map, and the returned Config carries the credentials
// configured there.
//
// The credentials in the returned Config are built and ready to use. Owned
// credentials are released by Config.Close; the xDS client's CreateChannel
// takes over that responsibility when a channel is created from the Config.
func Parse(gs *v3corepb.GrpcService, bc *bootstrap.Config, sc *bootstrap.ServerConfig) (_ *Config, err error) {
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
	// Release any credentials built before a mid-parse failure.
	defer func() {
		if err != nil {
			cfg.Close()
		}
	}()

	if trusted := sc != nil && sc.ServerFeaturesTrustedXDSServer(); trusted {
		if cfg.ChannelCredentials, err = buildChannelCredentials(googleGrpc.GetChannelCredentialsPlugin(), bc); err != nil {
			return nil, fmt.Errorf("grpcservice: %v", err)
		}
		if cfg.CallCredentials, err = buildCallCredentials(googleGrpc.GetCallCredentialsPlugin()); err != nil {
			return nil, fmt.Errorf("grpcservice: %v", err)
		}
	} else {
		// A nil bootstrap config has no allowlist, so all targets are
		// rejected.
		var svc *bootstrap.AllowedGRPCService
		if bc != nil {
			svc = bc.AllowedGRPCService(targetURI)
		}
		if svc == nil {
			return nil, fmt.Errorf("grpcservice: target_uri %q is not present in allowed_grpc_services", targetURI)
		}
		// The allowlisted credentials are owned by the bootstrap config:
		// their pairs carry no cleanup, so Config.Close does not affect
		// them.
		cfg.ChannelCredentials, cfg.CallCredentials = svc.SideChannelCredentials()
	}

	if cfg.Timeout, err = parseTimeout(gs); err != nil {
		return nil, err
	}
	if cfg.InitialMetadata, err = parseInitialMetadata(gs.GetInitialMetadata()); err != nil {
		return nil, err
	}
	return cfg, nil
}

// buildChannelCredentials builds the first channel credential from the plugin
// list whose proto type has a registered builder. It is an error if none of
// the configured plugins are supported, or if building the selected plugin
// fails.
func buildChannelCredentials(plugins []*anypb.Any, bc *bootstrap.Config) (*creds.ChannelCreds, error) {
	for _, p := range plugins {
		if p == nil {
			continue
		}
		b := credsregistry.GetChannelCredsBuilder(p.GetTypeUrl())
		if b == nil {
			continue
		}
		bundle, cleanup, err := b.Build(p, bc)
		if err != nil {
			return nil, fmt.Errorf("failed to build channel credentials %q: %v", p.GetTypeUrl(), err)
		}
		return creds.NewChannelCreds(bundle, creds.NewProtoIdentity(p), cleanup), nil
	}
	return nil, fmt.Errorf("no supported channel credentials found in grpc_service")
}

// buildCallCredentials builds the call credentials from the plugin list,
// preserving order. Plugins whose proto type has no registered builder are
// skipped; call credentials are optional, so an empty result is not an error.
func buildCallCredentials(plugins []*anypb.Any) (_ []*creds.CallCreds, err error) {
	var out []*creds.CallCreds
	// Release any credentials built before a mid-iteration failure.
	defer func() {
		if err != nil {
			for _, cc := range out {
				cc.Close()
			}
		}
	}()
	for _, p := range plugins {
		if p == nil {
			continue
		}
		b := credsregistry.GetCallCredsBuilder(p.GetTypeUrl())
		if b == nil {
			continue
		}
		cc, cleanup, err := b.Build(p)
		if err != nil {
			return nil, fmt.Errorf("failed to build call credentials %q: %v", p.GetTypeUrl(), err)
		}
		out = append(out, creds.NewCallCreds(cc, creds.NewProtoIdentity(p), cleanup))
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
