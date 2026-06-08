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
	"strings"
	"testing"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestParseGRPCServiceConfig_Trusted(t *testing.T) {
	insecurePlugin := &anypb.Any{
		TypeUrl: "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials",
	}

	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                "dns:///my-service:443",
				ChannelCredentialsPlugin: []*anypb.Any{insecurePlugin},
			},
		},
		Timeout: durationpb.New(5 * time.Second),
		InitialMetadata: []*v3corepb.HeaderValue{
			{Key: "key2", Value: "value2"},
			{Key: "key1", Value: "value1"},
			{Key: "key0", RawValue: []byte("value0")},
		},
	}

	cfg, err := ParseGRPCServiceConfig(protoMsg, true, nil)
	if err != nil {
		t.Fatalf("ParseGRPCServiceConfig() returned unexpected error: %v", err)
	}

	if cfg.TargetURI != "dns:///my-service:443" {
		t.Errorf("TargetURI got: %q, want: %q", cfg.TargetURI, "dns:///my-service:443")
	}
	if cfg.ChannelCredentials.Type != "insecure" {
		t.Errorf("ChannelCredentials.Type got: %q, want: %q", cfg.ChannelCredentials.Type, "insecure")
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout got: %v, want: %v", cfg.Timeout, 5*time.Second)
	}

	var headers []HeaderValueOption
	if err := json.Unmarshal([]byte(cfg.InitialMetadata), &headers); err != nil {
		t.Fatalf("Failed to unmarshal InitialMetadata: %v", err)
	}
	if len(headers) != 3 {
		t.Fatalf("Metadata count got: %d, want: 3", len(headers))
	}
	if headers[0].Key != "key0" || headers[0].Value != "value0" ||
		headers[1].Key != "key1" || headers[1].Value != "value1" ||
		headers[2].Key != "key2" || headers[2].Value != "value2" {
		t.Errorf("Metadata not parsed or sorted correctly got: %+v", headers)
	}
}

func TestParseGRPCServiceConfig_Untrusted_Whitelisted(t *testing.T) {
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: "dns:///my-service:443",
			},
		},
	}

	allowedJSON := []byte(`{
		"channel_creds": [{"type": "insecure"}]
	}`)
	var allowedSvc bootstrap.AllowedGrpcService
	if err := json.Unmarshal(allowedJSON, &allowedSvc); err != nil {
		t.Fatalf("Failed to unmarshal AllowedGrpcService: %v", err)
	}

	allowed := map[string]*bootstrap.AllowedGrpcService{
		"dns:///my-service:443": &allowedSvc,
	}

	cfg, err := ParseGRPCServiceConfig(protoMsg, false, allowed)
	if err != nil {
		t.Fatalf("ParseGRPCServiceConfig() returned unexpected error: %v", err)
	}

	if cfg.ChannelCredentials.Type != "insecure" {
		t.Errorf("ChannelCredentials.Type got: %q, want: %q", cfg.ChannelCredentials.Type, "insecure")
	}
}

func TestParseGRPCServiceConfig_Untrusted_Blocked(t *testing.T) {
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: "dns:///blocked-service:443",
			},
		},
	}

	_, err := ParseGRPCServiceConfig(protoMsg, false, nil)
	if err == nil {
		t.Fatal("ParseGRPCServiceConfig() succeeded for untrusted and un-whitelisted service, want error")
	}
}

func TestParseGRPCServiceConfig_UnregisteredResolverScheme(t *testing.T) {
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: "invalid-scheme:///my-service:443",
			},
		},
	}

	_, err := ParseGRPCServiceConfig(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "unregistered resolver scheme") {
		t.Fatalf("ParseGRPCServiceConfig() returned error: %v, want error with 'unregistered resolver scheme'", err)
	}
}

func TestParseGRPCServiceConfig_InvalidTimeoutDuration(t *testing.T) {
	insecurePlugin := &anypb.Any{
		TypeUrl: "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials",
	}

	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                "dns:///my-service:443",
				ChannelCredentialsPlugin: []*anypb.Any{insecurePlugin},
			},
		},
		Timeout: &durationpb.Duration{
			Seconds: 5,
			Nanos:   1e9 + 5, // Invalid nanoseconds
		},
	}

	_, err := ParseGRPCServiceConfig(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "obey duration limits") {
		t.Fatalf("ParseGRPCServiceConfig() returned error: %v, want error with 'obey duration limits'", err)
	}
}

func TestParseGRPCServiceConfig_InvalidHeaderKeyLength(t *testing.T) {
	insecurePlugin := &anypb.Any{
		TypeUrl: "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials",
	}
	longKey := strings.Repeat("a", 16385)
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                "dns:///my-service:443",
				ChannelCredentialsPlugin: []*anypb.Any{insecurePlugin},
			},
		},
		InitialMetadata: []*v3corepb.HeaderValue{
			{Key: longKey, Value: "value"},
		},
	}

	_, err := ParseGRPCServiceConfig(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "header key exceeds maximum allowed length") {
		t.Fatalf("ParseGRPCServiceConfig() returned error: %v, want error for key length exceeding 16384", err)
	}
}

func TestParseGRPCServiceConfig_InvalidHeaderKeyCharacters(t *testing.T) {
	insecurePlugin := &anypb.Any{
		TypeUrl: "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials",
	}
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                "dns:///my-service:443",
				ChannelCredentialsPlugin: []*anypb.Any{insecurePlugin},
			},
		},
		InitialMetadata: []*v3corepb.HeaderValue{
			{Key: "Key_With_Uppercase", Value: "value"},
		},
	}

	_, err := ParseGRPCServiceConfig(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "contains invalid character") {
		t.Fatalf("ParseGRPCServiceConfig() returned error: %v, want error with 'contains invalid character'", err)
	}
}
