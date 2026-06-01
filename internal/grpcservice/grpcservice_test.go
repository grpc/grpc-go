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

package grpcservice

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestConvert_Trusted(t *testing.T) {
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
		},
	}

	cfg, err := Convert(protoMsg, true, nil)
	if err != nil {
		t.Fatalf("Convert() returned unexpected error: %v", err)
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

	var headers []headerValueOption
	if err := json.Unmarshal([]byte(cfg.InitialMetadata), &headers); err != nil {
		t.Fatalf("Failed to unmarshal InitialMetadata: %v", err)
	}
	if len(headers) != 2 {
		t.Fatalf("Metadata count got: %d, want: 2", len(headers))
	}
	if headers[0].Key != "key1" || headers[1].Key != "key2" {
		t.Errorf("Metadata not sorted correctly got: %+v", headers)
	}
}

func TestConvert_Untrusted_Whitelisted(t *testing.T) {
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

	cfg, err := Convert(protoMsg, false, allowed)
	if err != nil {
		t.Fatalf("Convert() returned unexpected error: %v", err)
	}

	if cfg.ChannelCredentials.Type != "insecure" {
		t.Errorf("ChannelCredentials.Type got: %q, want: %q", cfg.ChannelCredentials.Type, "insecure")
	}
}

func TestConvert_Untrusted_Blocked(t *testing.T) {
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: "dns:///blocked-service:443",
			},
		},
	}

	_, err := Convert(protoMsg, false, nil)
	if err == nil {
		t.Fatal("Convert() succeeded for untrusted and un-whitelisted service, want error")
	}
}

func TestHeaderMutator_Mutate(t *testing.T) {
	mr := &v3mutationpb.HeaderMutationRules{
		AllowExpression:    &v3matcherpb.RegexMatcher{Regex: "allowed-.*"},
		DisallowExpression: &v3matcherpb.RegexMatcher{Regex: ".*-blocked"},
		DisallowIsError:    wrapperspb.Bool(true),
	}

	mutator, err := NewHeaderMutatorFromProto(mr)
	if err != nil {
		t.Fatalf("NewHeaderMutatorFromProto() returned error: %v", err)
	}

	md := metadata.Pairs("existing-key", "existing-val")

	mutations := []*v3corepb.HeaderValueOption{
		{
			Header:       &v3corepb.HeaderValue{Key: "allowed-header", Value: "mutated-val"},
			AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
		},
	}

	res, err := mutator.Mutate(md, mutations)
	if err != nil {
		t.Fatalf("Mutate() returned unexpected error: %v", err)
	}

	if got := res.Get("allowed-header"); len(got) != 1 || got[0] != "mutated-val" {
		t.Errorf("Mutate() failed to apply mutation: %v", res)
	}

	disallowedMutations := []*v3corepb.HeaderValueOption{
		{
			Header: &v3corepb.HeaderValue{Key: "header-blocked", Value: "val"},
		},
	}
	_, err = mutator.Mutate(md, disallowedMutations)
	if err == nil {
		t.Fatal("Mutate() succeeded for disallowed header with DisallowIsError=true, want error")
	}

	systemMutations := []*v3corepb.HeaderValueOption{
		{
			Header: &v3corepb.HeaderValue{Key: ":path", Value: "/new/path"},
		},
	}
	res, err = mutator.Mutate(md, systemMutations)
	if err != nil {
		t.Fatalf("Mutate() returned unexpected error for system header: %v", err)
	}
	if len(res.Get(":path")) != 0 {
		t.Errorf("System header mutation was applied: %v", res)
	}
}

func TestConvert_UnregisteredResolverScheme(t *testing.T) {
	protoMsg := &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri: "invalid-scheme:///my-service:443",
			},
		},
	}

	_, err := Convert(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "unregistered resolver scheme") {
		t.Fatalf("Convert() returned error: %v, want error with 'unregistered resolver scheme'", err)
	}
}

func TestConvert_InvalidTimeoutDuration(t *testing.T) {
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

	_, err := Convert(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "obey duration limits") {
		t.Fatalf("Convert() returned error: %v, want error with 'obey duration limits'", err)
	}
}

func TestHeaderMutator_KeepEmptyValue(t *testing.T) {
	mr := &v3mutationpb.HeaderMutationRules{}
	mutator, err := NewHeaderMutatorFromProto(mr)
	if err != nil {
		t.Fatalf("NewHeaderMutatorFromProto() returned error: %v", err)
	}

	t.Run("KeepEmptyValue = false (default)", func(t *testing.T) {
		md := metadata.Pairs("existing-key", "existing-val")
		mutations := []*v3corepb.HeaderValueOption{
			{
				Header:       &v3corepb.HeaderValue{Key: "existing-key", Value: ""},
				AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header:       &v3corepb.HeaderValue{Key: "new-key", Value: ""},
				AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		}
		res, err := mutator.Mutate(md, mutations)
		if err != nil {
			t.Fatalf("Mutate() returned error: %v", err)
		}
		if len(res.Get("existing-key")) != 0 {
			t.Errorf("existing-key was not removed: %v", res)
		}
		if len(res.Get("new-key")) != 0 {
			t.Errorf("new-key was added despite empty value and KeepEmptyValue=false: %v", res)
		}
	})

	t.Run("KeepEmptyValue = true", func(t *testing.T) {
		md := metadata.Pairs("existing-key", "existing-val")
		mutations := []*v3corepb.HeaderValueOption{
			{
				Header:         &v3corepb.HeaderValue{Key: "existing-key", Value: ""},
				AppendAction:   v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				KeepEmptyValue: true,
			},
			{
				Header:         &v3corepb.HeaderValue{Key: "new-key", Value: ""},
				AppendAction:   v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
				KeepEmptyValue: true,
			},
		}
		res, err := mutator.Mutate(md, mutations)
		if err != nil {
			t.Fatalf("Mutate() returned error: %v", err)
		}
		if got := res.Get("existing-key"); len(got) != 1 || got[0] != "" {
			t.Errorf("existing-key empty value was not kept: %v", res)
		}
		if got := res.Get("new-key"); len(got) != 1 || got[0] != "" {
			t.Errorf("new-key empty value was not kept: %v", res)
		}
	})
}

func TestConvert_InvalidHeaderKeyLength(t *testing.T) {
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

	_, err := Convert(protoMsg, true, nil)
	if err == nil || !strings.Contains(err.Error(), "header key exceeds maximum allowed length") {
		t.Fatalf("Convert() returned error: %v, want error for key length exceeding 16384", err)
	}
}
