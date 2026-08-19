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
	"net/url"
	"strings"
	"time"

	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	xdspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	insecureCredsTypeURL      = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
	googleDefaultCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials"
	tlsCredsTypeURL           = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.tls.v3.TlsCredentials"
	xdsCredsTypeURL           = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.xds.v3.XdsCredentials"
	accessTokenCredsTypeURL   = "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials"

	maxHeaderKeyLen   = 16384
	maxHeaderValueLen = 16384
)

// Config is the parsed form of a GrpcService proto.
type Config struct {
	// TargetURI is the gRPC target URI of the side-channel service.
	TargetURI string
	// Timeout, if non-zero, is the deadline to use for RPCs on the side
	// channel.
	Timeout time.Duration
	// InitialMetadata is the metadata to add to RPCs on the side channel.
	InitialMetadata metadata.MD
	// ChannelCredentials are the channel credentials extracted from the
	// proto's channel credentials plugins. Empty if the proto configures no
	// supported channel credentials.
	ChannelCredentials bootstrap.ChannelCreds
	// CallCredentials are the call credentials extracted from the proto's
	// call credentials plugins, preserving order.
	CallCredentials []bootstrap.CallCredsConfig
}

// Parse parses and validates a GrpcService proto into a Config.
//
// Parsing is independent of the trust status of the xDS server that delivered
// the proto: the credentials configured in the proto are always extracted
// into the returned Config, and it is up to the caller to decide whether they
// may be used.
func Parse(gs *v3corepb.GrpcService) (*Config, error) {
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

	channelCreds, err := extractChannelCredentials(googleGrpc.GetChannelCredentialsPlugin())
	if err != nil {
		return nil, fmt.Errorf("grpcservice: failed to extract channel credentials: %v", err)
	}
	callCreds, err := extractCallCredentials(googleGrpc.GetCallCredentialsPlugin())
	if err != nil {
		return nil, fmt.Errorf("grpcservice: failed to extract call credentials: %v", err)
	}

	timeout, err := parseTimeout(gs)
	if err != nil {
		return nil, err
	}

	initialMetadata, err := parseInitialMetadata(gs.GetInitialMetadata())
	if err != nil {
		return nil, err
	}

	return &Config{
		TargetURI:          targetURI,
		Timeout:            timeout,
		InitialMetadata:    initialMetadata,
		ChannelCredentials: channelCreds,
		CallCredentials:    callCreds,
	}, nil
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

// extractChannelCredentials returns the first supported channel credential
// from the plugin list. If none of the configured plugins are supported, it
// returns empty credentials without error; whether credentials are required
// is a policy decision left to the caller.
func extractChannelCredentials(plugins []*anypb.Any) (bootstrap.ChannelCreds, error) {
	for _, cred := range plugins {
		if cred == nil {
			continue
		}
		switch cred.GetTypeUrl() {
		case insecureCredsTypeURL:
			return bootstrap.ChannelCreds{Type: "insecure"}, nil
		case googleDefaultCredsTypeURL:
			return bootstrap.ChannelCreds{Type: "google_default"}, nil
		case xdsCredsTypeURL:
			// A side-channel target is not an xDS cluster, so
			// there is no xDS security configuration for it; the
			// xds credential therefore resolves to its required
			// fallback credential.
			var xdsCfg xdspb.XdsCredentials
			if err := anypb.UnmarshalTo(cred, &xdsCfg, proto.UnmarshalOptions{}); err != nil {
				return bootstrap.ChannelCreds{}, fmt.Errorf("failed to unmarshal XdsCredentials: %v", err)
			}
			fallback := xdsCfg.GetFallbackCredentials()
			if fallback == nil {
				return bootstrap.ChannelCreds{}, fmt.Errorf("xds credentials missing required fallback credentials")
			}
			return extractChannelCredentials([]*anypb.Any{fallback})
		case tlsCredsTypeURL:
			// TODO: Support TLS channel credentials. This requires
			// a certificate-provider-backed channel credentials
			// builder, since the A102 TlsCredentials message
			// references certificate provider instances rather than
			// file paths. Until then it is treated as an
			// unsupported type and skipped, so iteration falls
			// through to any supported fallback in the list.
			continue
		}
	}
	return bootstrap.ChannelCreds{}, nil
}

// extractCallCredentials returns the supported call credentials from the plugin
// list, preserving order. Unsupported types are ignored; call credentials are
// optional, so an empty result is not an error.
func extractCallCredentials(plugins []*anypb.Any) ([]bootstrap.CallCredsConfig, error) {
	var out []bootstrap.CallCredsConfig
	for _, cred := range plugins {
		if cred == nil {
			continue
		}
		if cred.GetTypeUrl() != accessTokenCredsTypeURL {
			continue
		}
		var accessToken accesstokenpb.AccessTokenCredentials
		if err := anypb.UnmarshalTo(cred, &accessToken, proto.UnmarshalOptions{}); err != nil {
			return nil, fmt.Errorf("failed to unmarshal AccessTokenCredentials: %v", err)
		}
		if accessToken.GetToken() == "" {
			return nil, fmt.Errorf("access token must be non-empty")
		}
		cfgJSON, err := json.Marshal(map[string]string{"token": accessToken.GetToken()})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal access token config: %v", err)
		}
		out = append(out, bootstrap.CallCredsConfig{
			Type:   "access_token",
			Config: json.RawMessage(cfgJSON),
		})
	}
	return out, nil
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
