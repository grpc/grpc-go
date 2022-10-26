/*
 *
 * Copyright 2022 gRPC authors.
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

package grpcutil

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/internal/envconfig"
)

func TestRegisteredCompressors(t *testing.T) {
	defer func(c []string) { RegisteredCompressorNames = c }(RegisteredCompressorNames)
	defer func(v bool) { envconfig.AdvertiseCompressors = v }(envconfig.AdvertiseCompressors)
	RegisteredCompressorNames = []string{"gzip", "snappy"}
	tests := []struct {
		desc    string
		enabled bool
		want    string
	}{
		{desc: "compressor_ad_disabled", enabled: false, want: ""},
		{desc: "compressor_ad_enabled", enabled: true, want: "gzip,snappy"},
	}
	for _, tt := range tests {
		envconfig.AdvertiseCompressors = tt.enabled
		compressors := RegisteredCompressors()
		if compressors != tt.want {
			t.Fatalf("Unexpected compressors got:%s, want:%s", compressors, tt.want)
		}
	}
}

func TestValidateSendCompressors(t *testing.T) {
	defer func(c []string) { RegisteredCompressorNames = c }(RegisteredCompressorNames)
	RegisteredCompressorNames = []string{"gzip", "snappy"}
	tests := []struct {
		desc                  string
		name                  string
		advertisedCompressors string
		wantErr               error
	}{
		{
			desc:                  "success_when_identity_compressor",
			name:                  "identity",
			advertisedCompressors: "gzip,snappy",
		},
		{
			desc:                  "success_when_compressor_exists",
			name:                  "snappy",
			advertisedCompressors: "testcomp,gzip,snappy",
		},
		{
			desc:                  "failure_when_compressor_not_registered",
			name:                  "testcomp",
			advertisedCompressors: "testcomp,gzip,snappy",
			wantErr:               fmt.Errorf("compressor not registered: testcomp"),
		},
		{
			desc:                  "failure_when_compressor_not_advertised",
			name:                  "gzip",
			advertisedCompressors: "testcomp,snappy",
			wantErr:               fmt.Errorf("client does not support compressor: gzip"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if err := ValidateSendCompressor(tt.name, tt.advertisedCompressors); fmt.Sprint(err) != fmt.Sprint(tt.wantErr) {
				t.Fatalf("Unexpected validation got:%v, want:%v", err, tt.wantErr)
			}
		})
	}
}
