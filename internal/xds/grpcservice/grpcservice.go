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

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	access_tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	xdspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
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

// GrpcService parses GrpcService protos in the context of a bootstrap
// configuration and a trust level for the delivering xDS server.
type GrpcService struct {
	config  *bootstrap.Config
	trusted bool
}

// New returns a GrpcService that parses GrpcService protos against the given
// bootstrap configuration. The trusted argument indicates whether the xDS
// server that delivered the resource is configured with the trusted_xds_server
// server feature.
func New(config *bootstrap.Config, trusted bool) *GrpcService {
	return &GrpcService{config: config, trusted: trusted}
}

// Config is the parsed form of a GrpcService proto.
type Config struct {
	// TargetURI is the gRPC target URI of the side-channel service.
	TargetURI string
	// Timeout, if non-zero, is the deadline to use for RPCs on the side
	// channel.
	Timeout time.Duration
	// InitialMetadata is the metadata to add to RPCs on the side channel.
	InitialMetadata metadata.MD
	// ChannelCredentials are the channel credentials used to create the
	// side channel. Set only on the trusted path.
	ChannelCredentials bootstrap.ChannelCreds
	// CallCredentials are the call credentials to apply to RPCs sent on the
	// side channel. Set only on the trusted path.
	CallCredentials []bootstrap.CallCredsConfig
}

// Parse parses and validates a GrpcService proto into a Config.
//
// When the delivering server is trusted, the credentials are taken from the
// proto; otherwise the target URI must be present in the allowed_grpc_services
// map, and the credentials are resolved later at channel creation time and left
// empty here.
func (g *GrpcService) Parse(gs *v3corepb.GrpcService) (Config, error) {
	googleGrpc := gs.GetGoogleGrpc()
	if googleGrpc == nil {
		return Config{}, fmt.Errorf("grpcservice: only google_grpc GrpcService config is supported")
	}

	targetURI := googleGrpc.GetTargetUri()
	if targetURI == "" {
		return Config{}, fmt.Errorf("grpcservice: target_uri must be non-empty")
	}
	if err := validateTargetURI(targetURI); err != nil {
		return Config{}, err
	}

	var channelCreds bootstrap.ChannelCreds
	var callCreds []bootstrap.CallCredsConfig
	if g.trusted {
		var err error
		if channelCreds, err = extractChannelCredentials(googleGrpc.GetChannelCredentialsPlugin()); err != nil {
			return Config{}, fmt.Errorf("grpcservice: failed to extract channel credentials: %v", err)
		}
		if callCreds, err = extractCallCredentials(googleGrpc.GetCallCredentialsPlugin()); err != nil {
			return Config{}, fmt.Errorf("grpcservice: failed to extract call credentials: %v", err)
		}
	} else {
		// For untrusted servers we ignore the credentials in the proto.
		// The target must be present in the allowed_grpc_services
		// allowlist, but the credentials themselves are resolved later,
		// at channel creation time; they are left empty in the parsed
		// config here.
		allowedSvc, ok := g.config.AllowedGrpcService(targetURI)
		if !ok {
			return Config{}, fmt.Errorf("grpcservice: target_uri %q is not present in allowed_grpc_services", targetURI)
		}
		if allowedSvc == nil {
			return Config{}, fmt.Errorf("grpcservice: allowed gRPC service %q has nil configuration", targetURI)
		}
	}

	timeout, err := parseTimeout(gs)
	if err != nil {
		return Config{}, err
	}

	initialMetadata, err := parseInitialMetadata(gs.GetInitialMetadata())
	if err != nil {
		return Config{}, err
	}

	return Config{
		TargetURI:          targetURI,
		Timeout:            timeout,
		InitialMetadata:    initialMetadata,
		ChannelCredentials: channelCreds,
		CallCredentials:    callCreds,
	}, nil
}

// validateTargetURI verifies that the target URI uses a registered resolver
// scheme. A target with an explicit "scheme://" prefix must use a registered
// scheme; otherwise the default resolver scheme is assumed.
func validateTargetURI(targetURI string) error {
	scheme := resolver.GetDefaultScheme()
	if strings.Contains(targetURI, "://") {
		u, err := url.Parse(targetURI)
		if err != nil {
			return fmt.Errorf("grpcservice: target_uri %q is invalid: %v", targetURI, err)
		}
		scheme = u.Scheme
	}
	if resolver.Get(scheme) == nil {
		return fmt.Errorf("grpcservice: target_uri %q uses unregistered resolver scheme %q", targetURI, scheme)
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
		if err := validateHeaderValue(val); err != nil {
			return nil, fmt.Errorf("grpcservice: invalid value for header key %q: %v", key, err)
		}
		md.Append(key, val)
	}
	return md, nil
}

// extractChannelCredentials returns the first supported channel credential from
// the plugin list. It is an error if none of the configured plugins are
// supported.
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
	return bootstrap.ChannelCreds{}, fmt.Errorf("no supported channel credentials found in plugins")
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
		var accessToken access_tokenpb.AccessTokenCredentials
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
	case key == "":
		return fmt.Errorf("header key cannot be empty")
	case len(key) > maxHeaderKeyLen:
		return fmt.Errorf("header key exceeds maximum allowed length of %d", maxHeaderKeyLen)
	case key == "host":
		return fmt.Errorf("header key cannot be %q", "host")
	case strings.HasPrefix(key, ":"):
		return fmt.Errorf("header key cannot start with %q", ":")
	case strings.HasPrefix(key, "grpc-"):
		return fmt.Errorf("header key cannot start with %q", "grpc-")
	}
	for _, ch := range key {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.') {
			return fmt.Errorf("header key contains invalid character %q", ch)
		}
	}
	return nil
}

func validateHeaderValue(val string) error {
	if len(val) > maxHeaderValueLen {
		return fmt.Errorf("header value exceeds maximum allowed length of %d", maxHeaderValueLen)
	}
	for _, ch := range val {
		if ch != '\t' && (ch < 32 || ch > 126) {
			return fmt.Errorf("header value contains invalid character %q", ch)
		}
	}
	return nil
}
