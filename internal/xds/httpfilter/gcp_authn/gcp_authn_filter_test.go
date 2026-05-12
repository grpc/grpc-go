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
	"strings"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	wrapperspb "google.golang.org/protobuf/types/known/wrapperspb"

	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestParseFilterConfig verifies that the filter successfully decodes the
// raw protobuf configuration into the internal config struct.
func (s) TestParseFilterConfig(t *testing.T) {
	b := builder{}
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
			name:    "empty_config",
			config:  nil,
			wantErr: "gcpauthn: empty filter config",
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
			name: "overflow_cache_size",
			config: testutils.MarshalAny(t, &v3gcpauthnpb.GcpAuthnFilterConfig{
				CacheConfig: &v3gcpauthnpb.TokenCacheConfig{
					CacheSize: &wrapperspb.UInt64Value{Value: 1 << 32},
				},
			}),
			wantCfg: config{cacheSize: 1<<31 - 2},
		},
		{
			name:    "invalid_message_type",
			config:  &v3gcpauthnpb.GcpAuthnFilterConfig{},
			wantErr: "gcpauthn: invalid filter config type",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := b.ParseFilterConfig(tc.config)
			if err != nil {
				if tc.wantErr == "" || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseFilterConfig() failed with error = %v; want error  %q", err, tc.wantErr)
				}
				return
			}
			if got.(config).cacheSize != tc.wantCfg.(config).cacheSize {
				t.Fatalf("cacheSize = %v, want %v", got.(config).cacheSize, tc.wantCfg.(config).cacheSize)
			}
		})
	}
}

// TestBuildClientInterceptor verifies that the filter correctly creates
// the stream interceptor from the parsed config, and correctly handles
// cache initialization and resizing.
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
			name:         "empty_config",
			cfg:          nil,
			clientFilter: &ClientFilter{FilterName: "gcp_authn"},
			wantErr:      "empty filter config",
		},
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
				t.Errorf("BuildClientInterceptor() returned interceptor with filtername = %q, want %q", interceptor.filterName, tc.clientFilter.FilterName)
			}

			if interceptor.cache == nil || interceptor.cache.cacheSize != tc.wantCacheSize {
				t.Errorf("BuildClientInterceptor() returned interceptor with cacheSize = %d, want %d", interceptor.cache.cacheSize, tc.wantCacheSize)
			}
		})
	}
}
