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

package xdsresource

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	access_tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	xdspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/resolver"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// GRPCServiceConfig represents the parsed GrpcService configuration.
type GRPCServiceConfig struct {
	TargetURI          string
	ChannelCredentials bootstrap.ChannelCreds
	// CallCredentials is a JSON-serialized list of call creds configs.
	CallCredentials string
	Timeout         time.Duration
	// InitialMetadata is a JSON-serialized list of metadata headers.
	InitialMetadata string
}

// Equal reports whether c and other are considered equal.
func (c GRPCServiceConfig) Equal(other GRPCServiceConfig) bool {
	return c.TargetURI == other.TargetURI &&
		c.ChannelCredentials.Equal(other.ChannelCredentials) &&
		c.CallCredentials == other.CallCredentials &&
		c.Timeout == other.Timeout &&
		c.InitialMetadata == other.InitialMetadata
}

// InitialMetadataOptions returns the parsed key-value pairs of metadata.
func (c GRPCServiceConfig) InitialMetadataOptions() ([]HeaderValueOption, error) {
	if c.InitialMetadata == "" {
		return nil, nil
	}
	var headers []HeaderValueOption
	if err := json.Unmarshal([]byte(c.InitialMetadata), &headers); err != nil {
		return nil, fmt.Errorf("xdsresource: failed to unmarshal initial metadata: %v", err)
	}
	return headers, nil
}

// ParseGRPCServiceConfig parses a GrpcService proto into GRPCServiceConfig.
func ParseGRPCServiceConfig(gs *v3corepb.GrpcService, trusted bool, allowed map[string]*bootstrap.AllowedGrpcService) (GRPCServiceConfig, error) {
	if gs.GetGoogleGrpc() == nil {
		return GRPCServiceConfig{}, fmt.Errorf("xdsresource: only google_grpc GrpcService config is supported")
	}
	google := gs.GetGoogleGrpc()

	targetURI := google.GetTargetUri()
	if targetURI == "" {
		return GRPCServiceConfig{}, fmt.Errorf("xdsresource: target_uri must be non-empty")
	}
	var scheme string
	if strings.Contains(targetURI, "://") {
		u, err := url.Parse(targetURI)
		if err != nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: target_uri %q is invalid: %v", targetURI, err)
		}
		scheme = u.Scheme
	}
	if scheme == "" {
		scheme = "passthrough"
	}
	if resolver.Get(scheme) == nil {
		return GRPCServiceConfig{}, fmt.Errorf("xdsresource: target_uri %q has unregistered resolver scheme %q", targetURI, scheme)
	}

	var channelCreds bootstrap.ChannelCreds
	var callCreds string

	if !trusted {
		allowedSvc, ok := allowed[targetURI]
		if !ok {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: target_uri %q is not whitelisted in allowed_grpc_services", targetURI)
		}
		if allowedSvc == nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: allowed gRPC service %q has nil configuration", targetURI)
		}
		channelCreds = allowedSvc.SelectedChannelCreds()

		comps := append([]bootstrap.CallCredsConfig(nil), allowedSvc.CallCredsConfigs()...)
		sort.Slice(comps, func(i, j int) bool {
			if comps[i].Type != comps[j].Type {
				return comps[i].Type < comps[j].Type
			}
			return string(comps[i].Config) < string(comps[j].Config)
		})
		data, _ := json.Marshal(comps)
		callCreds = string(data)
	} else {
		var err error
		channelCreds, err = extractChannelCredentials(google.GetChannelCredentialsPlugin())
		if err != nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: failed to extract channel credentials: %v", err)
		}

		callCreds, err = extractCallCredentials(google.GetCallCredentialsPlugin())
		if err != nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: failed to extract call credentials: %v", err)
		}
	}

	var timeout time.Duration
	if gs.GetTimeout() != nil {
		t := gs.GetTimeout()
		if t.GetSeconds() < 0 || t.GetNanos() < 0 || t.GetNanos() >= 1e9 || (t.GetSeconds() == 0 && t.GetNanos() == 0) {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: timeout must be strictly positive and obey duration limits")
		}
		timeout = time.Duration(t.GetSeconds())*time.Second + time.Duration(t.GetNanos())*time.Nanosecond
	}

	var headers []HeaderValueOption
	for _, h := range gs.GetInitialMetadata() {
		key := h.GetKey()
		var val string
		if len(h.GetRawValue()) > 0 {
			val = string(h.GetRawValue())
		} else {
			val = h.GetValue()
		}

		if err := validateHeaderKey(key); err != nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: invalid header key %q: %v", key, err)
		}
		if err := validateHeaderValue(val); err != nil {
			return GRPCServiceConfig{}, fmt.Errorf("xdsresource: invalid header value: %v", err)
		}

		headers = append(headers, HeaderValueOption{
			Key:   key,
			Value: val,
		})
	}
	sort.Slice(headers, func(i, j int) bool {
		if headers[i].Key != headers[j].Key {
			return headers[i].Key < headers[j].Key
		}
		return headers[i].Value < headers[j].Value
	})
	headersJSON, _ := json.Marshal(headers)

	return GRPCServiceConfig{
		TargetURI:          targetURI,
		ChannelCredentials: channelCreds,
		CallCredentials:    callCreds,
		Timeout:            timeout,
		InitialMetadata:    string(headersJSON),
	}, nil
}

// HeaderValueOption represents a header key-value pair.
type HeaderValueOption struct {
	Key   string
	Value string
}

func extractChannelCredentials(plugins []*anypb.Any) (bootstrap.ChannelCreds, error) {
	for _, cred := range plugins {
		if cred == nil {
			continue
		}
		switch cred.TypeUrl {
		case "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials":
			return bootstrap.ChannelCreds{Type: "google_default"}, nil
		case "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials":
			return bootstrap.ChannelCreds{Type: "insecure"}, nil
		case "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.xds.v3.XdsCredentials":
			var xdsConfig xdspb.XdsCredentials
			if err := anypb.UnmarshalTo(cred, &xdsConfig, proto.UnmarshalOptions{}); err != nil {
				return bootstrap.ChannelCreds{}, fmt.Errorf("failed to unmarshal XdsCredentials: %v", err)
			}
			fallback := xdsConfig.GetFallbackCredentials()
			if fallback == nil {
				return bootstrap.ChannelCreds{}, fmt.Errorf("xds credentials missing fallback credentials")
			}
			return extractChannelCredentials([]*anypb.Any{fallback})
		case "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.tls.v3.TlsCredentials":
			return bootstrap.ChannelCreds{}, fmt.Errorf("tls credentials not supported")
		case "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.local.v3.LocalCredentials":
			return bootstrap.ChannelCreds{}, fmt.Errorf("local credentials not supported")
		}
	}
	return bootstrap.ChannelCreds{}, fmt.Errorf("no supported channel credentials found in plugins")
}

func extractCallCredentials(plugins []*anypb.Any) (string, error) {
	var comps []bootstrap.CallCredsConfig
	for _, cred := range plugins {
		if cred == nil {
			continue
		}
		if cred.TypeUrl == "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials" {
			var accessToken access_tokenpb.AccessTokenCredentials
			if err := anypb.UnmarshalTo(cred, &accessToken, proto.UnmarshalOptions{}); err != nil {
				return "", fmt.Errorf("failed to unmarshal AccessTokenCredentials: %v", err)
			}
			if accessToken.GetToken() == "" {
				return "", fmt.Errorf("access token must be non-empty")
			}
			cfgJSON, _ := json.Marshal(map[string]string{"token": accessToken.GetToken()})
			comps = append(comps, bootstrap.CallCredsConfig{
				Type:   "access_token",
				Config: json.RawMessage(cfgJSON),
			})
		}
	}
	sort.Slice(comps, func(i, j int) bool {
		if comps[i].Type != comps[j].Type {
			return comps[i].Type < comps[j].Type
		}
		return string(comps[i].Config) < string(comps[j].Config)
	})
	data, err := json.Marshal(comps)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func validateHeaderKey(key string) error {
	if key == "" {
		return fmt.Errorf("header key cannot be empty")
	}
	if len(key) > 16384 {
		return fmt.Errorf("header key exceeds maximum allowed length of 16384")
	}
	for _, ch := range key {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.') {
			return fmt.Errorf("header key %q contains invalid character %q", key, ch)
		}
	}
	if key == "host" {
		return fmt.Errorf("header key cannot be \"host\"")
	}
	if strings.HasPrefix(key, ":") {
		return fmt.Errorf("header key cannot start with \":\"")
	}
	if strings.HasPrefix(key, "grpc-") {
		return fmt.Errorf("header key cannot start with \"grpc-\"")
	}
	return nil
}

func validateHeaderValue(val string) error {
	if len(val) > 16384 {
		return fmt.Errorf("header value exceeds maximum allowed length of 16384")
	}
	return nil
}
