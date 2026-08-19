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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/metadata"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const target = "dns:///my-service:443"

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

func accessTokenPlugin(t *testing.T, token string) *anypb.Any {
	t.Helper()
	a, err := anypb.New(&accesstokenpb.AccessTokenCredentials{Token: token})
	if err != nil {
		t.Fatalf("Failed to marshal AccessTokenCredentials: %v", err)
	}
	return a
}

func (s) TestParse(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}

	tests := []struct {
		name    string
		gs      *v3corepb.GrpcService
		want    *Config
		wantErr string
	}{
		{
			name: "insecure_channel_creds",
			gs:   googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil),
			want: &Config{TargetURI: target, ChannelCredentials: bootstrap.ChannelCreds{Type: "insecure"}},
		},
		{
			name: "no_channel_creds_left_empty",
			gs:   googleGrpcService(target, nil, nil),
			want: &Config{TargetURI: target},
		},
		{
			name: "unsupported_channel_creds_left_empty",
			gs:   googleGrpcService(target, []*anypb.Any{{TypeUrl: "type.googleapis.com/unsupported.Credentials"}}, nil),
			want: &Config{TargetURI: target},
		},
		{
			name:    "missing_google_grpc",
			gs:      &v3corepb.GrpcService{},
			wantErr: "only google_grpc",
		},
		{
			name:    "empty_target_uri",
			gs:      googleGrpcService("", nil, nil),
			wantErr: "target_uri must be non-empty",
		},
		{
			name:    "zero_timeout_rejected",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, durationpb.New(0)),
			wantErr: "timeout must be strictly positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.gs)
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

func (s) TestParseCallCredentials(t *testing.T) {
	gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: insecureCredsTypeURL}}, nil)
	gs.GetGoogleGrpc().CallCredentialsPlugin = []*anypb.Any{accessTokenPlugin(t, "test-token")}
	got, err := Parse(gs)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	want := []bootstrap.CallCredsConfig{{Type: "access_token", Config: json.RawMessage(`{"token":"test-token"}`)}}
	if diff := cmp.Diff(want, got.CallCredentials); diff != "" {
		t.Errorf("Parse() CallCredentials mismatch (-want +got):\n%s", diff)
	}

	// An empty token must be rejected.
	gs.GetGoogleGrpc().CallCredentialsPlugin = []*anypb.Any{accessTokenPlugin(t, "")}
	if _, err := Parse(gs); err == nil || !strings.Contains(err.Error(), "access token must be non-empty") {
		t.Fatalf("Parse() error = %v, want substring %q", err, "access token must be non-empty")
	}
}

func (s) TestParseInitialMetadata(t *testing.T) {
	gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: insecureCredsTypeURL}}, nil)
	gs.InitialMetadata = []*v3corepb.HeaderValue{
		{Key: "key-b", Value: "b"},
		{Key: "key-a", Value: "legacy", RawValue: []byte("raw-a")},
	}
	got, err := Parse(gs)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	want := metadata.MD{"key-b": []string{"b"}, "key-a": []string{"raw-a"}}
	if diff := cmp.Diff(want, got.InitialMetadata); diff != "" {
		t.Errorf("Parse() InitialMetadata mismatch (-want +got):\n%s", diff)
	}
}
