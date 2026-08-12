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

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

const target = "dns:///my-service:443"

// bootstrapConfig builds a bootstrap Config whose allowed_grpc_services is set
// to the provided JSON (a map from target URI to allowed service config).
func bootstrapConfig(t *testing.T, allowed string) *bootstrap.Config {
	t.Helper()
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             json.RawMessage(`[{"server_uri":"td.googleapis.com:443","channel_creds":[{"type":"insecure"}]}]`),
		Node:                json.RawMessage(`{}`),
		AllowedGRPCServices: json.RawMessage(allowed),
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	cfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}
	return cfg
}

func googleGrpcService(target string, channelPlugins []*anypb.Any, timeout *durationpb.Duration) *v3corepb.GrpcService {
	return &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                target,
				ChannelCredentialsPlugin: channelPlugins,
			},
		},
		Timeout: timeout,
	}
}

func TestParse(t *testing.T) {
	// The allowed_grpc_services bootstrap field is parsed only when a
	// consuming feature is enabled.
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	allowedInsecure := `{"dns:///my-service:443":{"channel_creds":[{"type":"insecure"}]}}`

	tests := []struct {
		name    string
		gs      *v3corepb.GrpcService
		trusted bool
		config  *bootstrap.Config
		want    Config
		wantErr string
	}{
		{
			name:    "trusted_insecure_channel_creds",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil),
			trusted: true,
			config:  bootstrapConfig(t, "{}"),
			want:    Config{TargetURI: target, ChannelCredentials: bootstrap.ChannelCreds{Type: "insecure"}},
		},
		{
			name:    "untrusted_allowlisted_leaves_creds_empty",
			gs:      googleGrpcService(target, nil, nil),
			trusted: false,
			config:  bootstrapConfig(t, allowedInsecure),
			want:    Config{TargetURI: target},
		},
		{
			name:    "untrusted_not_allowlisted",
			gs:      googleGrpcService(target, nil, nil),
			trusted: false,
			config:  bootstrapConfig(t, "{}"),
			wantErr: "not present in allowed_grpc_services",
		},
		{
			name:    "missing_google_grpc",
			gs:      &v3corepb.GrpcService{},
			trusted: true,
			config:  bootstrapConfig(t, "{}"),
			wantErr: "only google_grpc",
		},
		{
			name:    "zero_timeout_rejected",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, durationpb.New(0)),
			trusted: true,
			config:  bootstrapConfig(t, "{}"),
			wantErr: "timeout must be strictly positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := New(test.config, test.trusted).Parse(test.gs)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Parse() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseInitialMetadata(t *testing.T) {
	gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: insecureCredsTypeURL}}, nil)
	gs.InitialMetadata = []*v3corepb.HeaderValue{
		{Key: "key-b", Value: "b"},
		{Key: "key-a", Value: "legacy", RawValue: []byte("raw-a")},
	}
	got, err := New(bootstrapConfig(t, "{}"), true).Parse(gs)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	want := metadata.MD{"key-b": []string{"b"}, "key-a": []string{"raw-a"}}
	if diff := cmp.Diff(want, got.InitialMetadata); diff != "" {
		t.Errorf("Parse() InitialMetadata mismatch (-want +got):\n%s", diff)
	}
}
