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

package gcpauthn

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"

	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	iresolver "google.golang.org/grpc/internal/resolver"
	wrapperspb "google.golang.org/protobuf/types/known/wrapperspb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Test verifies that the filter successfully decodes the raw protobuf
// configuration into the internal config struct.
func (s) TestParseFilterConfig(t *testing.T) {
	testCases := []struct {
		name    string
		config  proto.Message
		wantCfg httpfilter.FilterConfig
		wantErr string
	}{
		{
			name: "valid_config_with_cache_size",
			config: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
				CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
					CacheSize: &wrapperspb.UInt64Value{Value: 50},
				},
			}),
			wantCfg: config{cacheSize: 50},
		},
		{
			name:    "default_cache_size",
			config:  testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{}),
			wantCfg: config{cacheSize: defaultCacheSize},
		},
		{
			name: "zero_cache_size",
			config: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
				CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
					CacheSize: &wrapperspb.UInt64Value{Value: 0},
				},
			}),
			wantErr: "cache_config.cache_size must be greater than zero",
		},
		{
			name:    "invalid_message_type",
			config:  &v3gcpauthnpb.GcpAuthnFilterConfig{},
			wantErr: "gcpauthn: invalid filter config type",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := builder{}.ParseFilterConfig(tc.config)
			if err != nil {
				if tc.wantErr == "" || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseFilterConfig() failed with error = %v; want error  %q", err, tc.wantErr)
				}
				return
			}
			if got.(config).cacheSize != tc.wantCfg.(config).cacheSize {
				t.Fatalf("ParseFilterConfig() got cacheSize = %v, want %v", got.(config).cacheSize, tc.wantCfg.(config).cacheSize)
			}
		})
	}
}

// Test verifies that the filter correctly creates the stream interceptor
// from the parsed config, and correctly handles cache initialization and
// resizing.
func (s) TestBuildClientInterceptor(t *testing.T) {
	type dummyConfig struct {
		httpfilter.FilterConfig
	}
	tests := []struct {
		name          string
		cfg           httpfilter.FilterConfig
		clientFilter  *ClientFilter
		wantErr       string
		wantCacheSize uint64
	}{
		{
			name:         "invalid_config_type",
			cfg:          dummyConfig{},
			clientFilter: &ClientFilter{FilterName: "gcp_authn"},
			wantErr:      "invalid filter config type",
		},
		{
			name: "cache_is_nil",
			cfg:  config{cacheSize: 5},
			clientFilter: &ClientFilter{
				FilterName: "gcp_authn",
				cache:      nil,
			},
			wantCacheSize: 5,
		},
		{
			name: "cache_resize_smaller",
			cfg:  config{cacheSize: 3},
			clientFilter: &ClientFilter{
				FilterName: "gcp_authn",
				cache:      newLRUCache(5),
			},
			wantCacheSize: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i, err := tc.clientFilter.BuildClientInterceptor(tc.cfg, nil)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("BuildClientInterceptor() returned nil, want error %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("BuildClientInterceptor() failed with error = %v, want error %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("BuildClientInterceptor() returned unexpected error: %v", err)
			}

			interceptor := i.(*interceptor)

			if interceptor.filterName != tc.clientFilter.FilterName {
				t.Fatalf("BuildClientInterceptor() returned interceptor with filtername = %q, want %q", interceptor.filterName, tc.clientFilter.FilterName)
			}

			if interceptor.cache == nil || interceptor.cache.cacheSize != tc.wantCacheSize {
				t.Fatalf("BuildClientInterceptor() returned interceptor with cacheSize = %d, want %d", interceptor.cache.cacheSize, tc.wantCacheSize)
			}
		})
	}
}

// Test verifies that interceptor.NewStream returns the expected errors under
// various invalid states, such as a missing XDS config, a cluster not defined
// in the CDS, or a malformed metadata type.
func (s) TestInterceptor_NewStream_Errors(t *testing.T) {
	builder := httpfilter.Get("type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.GcpAuthnFilterConfig")
	cfg := testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
		CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
			CacheSize: &wrapperspb.UInt64Value{Value: 10},
		},
	})
	filterConfig, err := builder.ParseFilterConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to parse filter config: %v", err)
	}
	clientFilterBuilder, ok := builder.(httpfilter.ClientFilterBuilder)
	if !ok {
		t.Fatalf("Filter Builder does not implement ClientFilterBuilder")
	}
	clientFilter := clientFilterBuilder.BuildClientFilter().(*ClientFilter)
	clientFilter.FilterName = "com.google.grpc.gcp_authn"
	interceptor, err := clientFilter.BuildClientInterceptor(filterConfig, nil)
	if err != nil {
		t.Fatalf("Failed to build client interceptor: %v", err)
	}

	validConfig := &xdsresource.XDSConfig{
		Clusters: map[string]*xdsresource.ClusterResult{
			"cluster1": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						Metadata: map[string]any{
							"com.google.grpc.gcp_authn": xdsresource.AudienceMetadataValue{Audience: "https://example.com"},
						},
					},
				},
			},
			"cluster_wrong_type": {
				Config: xdsresource.ClusterConfig{
					Cluster: &xdsresource.ClusterUpdate{
						Metadata: map[string]any{
							"com.google.grpc.gcp_authn": "not-audience-metadata",
						},
					},
				},
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr string
	}{
		{
			name:    "missing_xds_config",
			ctx:     clustermanager.SetPickedCluster(ctx, "cluster1"),
			wantErr: "xDS config not found in context",
		},
		{
			name:    "cluster_not_found_in_CDS",
			ctx:     clustermanager.SetPickedCluster(xdsresource.NewContextWithXDSConfig(ctx, validConfig), "cluster_not_found"),
			wantErr: "not found in xDS config",
		},
		{
			name:    "wrong_metadata_type",
			ctx:     clustermanager.SetPickedCluster(xdsresource.NewContextWithXDSConfig(ctx, validConfig), "cluster_wrong_type"),
			wantErr: "not of type AudienceMetadataValue",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newStream := func(_ context.Context, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, nil
			}
			if _, err := interceptor.NewStream(tc.ctx, iresolver.RPCInfo{}, newStream); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewStream() returned unexpected results, got %q , want error containing %q", err, tc.wantErr)
			}
		})
	}
}
