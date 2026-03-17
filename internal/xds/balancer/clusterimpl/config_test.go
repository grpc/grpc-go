/*
 *
 * Copyright 2020 gRPC authors.
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

package clusterimpl

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	_ "google.golang.org/grpc/balancer/roundrobin"
	_ "google.golang.org/grpc/balancer/weightedtarget"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
)

const (
	testJSONConfig = `{
  "cluster": "test_cluster",
  "edsServiceName": "test-eds",
  "lrsLoadReportingServer": {
    "server_uri": "trafficdirector.googleapis.com:443",
    "channel_creds": [ { "type": "google_default" } ]
  },
  "maxConcurrentRequests": 123,
  "dropCategories": [
    {
      "category": "drop-1",
      "requestsPerMillion": 314
    },
    {
      "category": "drop-2",
      "requestsPerMillion": 159
    }
  ],
  "childPolicy": [
    {
      "weighted_target_experimental": {
        "targets": {
          "wt-child-1": {
            "weight": 75,
            "childPolicy":[{"round_robin":{}}]
          },
          "wt-child-2": {
            "weight": 25,
            "childPolicy":[{"round_robin":{}}]
          }
        }
      }
    }
  ]
}`

	wtName = "weighted_target_experimental"
)

var (
	wtConfigParser = balancer.Get(wtName).(balancer.ConfigParser)
	wtConfigJSON   = `{
  "targets": {
    "wt-child-1": {
      "weight": 75,
      "childPolicy":[{"round_robin":{}}]
    },
    "wt-child-2": {
      "weight": 25,
      "childPolicy":[{"round_robin":{}}]
    }
  }
}`

	wtConfig, _ = wtConfigParser.ParseConfig([]byte(wtConfigJSON))
)

func (s) TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		js      string
		want    *LBConfig
		wantErr bool
	}{
		{
			name:    "empty json",
			js:      "",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "bad json",
			js:      "{",
			want:    nil,
			wantErr: true,
		},
		{
			name: "OK",
			js:   testJSONConfig,
			want: &LBConfig{
				Cluster: "test_cluster",
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name:   wtName,
					Config: wtConfig,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig([]byte(tt.js))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseConfig() got unexpected diff (-want, +got): %v", diff)
			}
		})
	}
}
