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

package autosharding

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

func TestParseConfig_Success(t *testing.T) {
	parser := bb{}
	tests := []struct {
		name    string
		input   string
		wantCfg serviceconfig.LoadBalancingConfig
	}{
		{
			name: "all-fields",
			input: `{
				"channelFactoryKey": "factory-key",
				"autoshardingTarget": "target",
				"keyHeaderName": "header",
				"enableFallback": true,
				"initialAssignmentTimeout": "30s"
			}`,
			wantCfg: &lbConfig{
				ChannelFactoryKey:        "factory-key",
				AutoShardingTarget:       "target",
				KeyHeaderName:            "header",
				EnableFallback:           true,
				InitialAssignmentTimeout: iserviceconfig.Duration(30 * time.Second),
			},
		},
		{
			name: "default-timeout",
			input: `{
				"channelFactoryKey": "factory-key",
				"autoshardingTarget": "target",
				"keyHeaderName": "header"
			}`,
			wantCfg: &lbConfig{
				ChannelFactoryKey:        "factory-key",
				AutoShardingTarget:       "target",
				KeyHeaderName:            "header",
				InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, err := parser.ParseConfig(json.RawMessage(test.input))
			if err != nil {
				t.Fatalf("ParseConfig() error = %v, want nil", err)
			}
			if diff := cmp.Diff(test.wantCfg, gotCfg); diff != "" {
				t.Errorf("ParseConfig() config diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseConfig_Failure(t *testing.T) {
	parser := bb{}
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "invalid-json",
			input: "{{invalidjson{{",
		},
		{
			name: "invalid-duration",
			input: `{
				"channelFactoryKey": "factory-key",
				"autoshardingTarget": "target",
				"keyHeaderName": "header",
				"initialAssignmentTimeout": "invalid"
			}`,
		},
		{
			name: "missing-channel-factory-key",
			input: `{
				"autoshardingTarget": "target",
				"keyHeaderName": "header"
			}`,
		},
		{
			name: "missing-autosharding-target",
			input: `{
				"channelFactoryKey": "factory-key",
				"keyHeaderName": "header"
			}`,
		},
		{
			name: "missing-key-header-name",
			input: `{
				"channelFactoryKey": "factory-key",
				"autoshardingTarget": "target"
			}`,
		},
		{
			name:  "empty-config",
			input: `{}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parser.ParseConfig(json.RawMessage(test.input)); err == nil {
				t.Fatalf("ParseConfig() succeeded, want error")
			}
		})
	}
}
