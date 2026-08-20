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

package credsregistry

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/anypb"

	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
)

// Tests that building xds channel credentials fails on malformed configs and
// on missing or unsupported fallback credentials.
func (s) TestXDSCredsBuild_Errors(t *testing.T) {
	missingFallback, err := anypb.New(&xdscredspb.XdsCredentials{})
	if err != nil {
		t.Fatalf("Failed to marshal XdsCredentials: %v", err)
	}
	unsupportedFallback, err := anypb.New(&xdscredspb.XdsCredentials{
		FallbackCredentials: &anypb.Any{TypeUrl: "type.googleapis.com/unknown.Credentials"},
	})
	if err != nil {
		t.Fatalf("Failed to marshal XdsCredentials: %v", err)
	}

	tests := []struct {
		name    string
		config  *anypb.Any
		wantErr string
	}{
		{
			name:    "unmarshal_failure",
			config:  &anypb.Any{TypeUrl: xdsCredsTypeURL, Value: []byte{0xff}},
			wantErr: "failed to unmarshal XdsCredentials",
		},
		{
			name:    "missing_fallback_credentials",
			config:  missingFallback,
			wantErr: "missing required fallback credentials",
		},
		{
			name:    "unsupported_fallback_credentials_type",
			config:  unsupportedFallback,
			wantErr: "unsupported fallback credentials type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := GetChannelCredsBuilder(xdsCredsTypeURL).Build(tt.config, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Build() returned error %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
