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

package lrs

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer/roundrobin"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	xdsinternal "google.golang.org/grpc/xds/internal"
)

const (
	testClusterName   = "test-cluster"
	testServiceName   = "test-eds-service"
	testLRSServerName = "test-lrs-name"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		js      string
		want    *lbConfig
		wantErr bool
	}{
		{
			name: "no cluster name",
			js: `{
  "edsServiceName": "test-eds-service",
  "lrsLoadReportingServerName": "test-lrs-name",
  "locality": {
    "region": "test-region",
    "zone": "test-zone",
    "subZone": "test-sub-zone"
  },
  "childPolicy":[{"round_robin":{}}]
}
			`,
			wantErr: true,
		},
		{
			name: "no LRS server name",
			js: `{
  "clusterName": "test-cluster",
  "edsServiceName": "test-eds-service",
  "locality": {
    "region": "test-region",
    "zone": "test-zone",
    "subZone": "test-sub-zone"
  },
  "childPolicy":[{"round_robin":{}}]
}
			`,
			wantErr: true,
		},
		{
			name: "no locality",
			js: `{
  "clusterName": "test-cluster",
  "edsServiceName": "test-eds-service",
  "lrsLoadReportingServerName": "test-lrs-name",
  "childPolicy":[{"round_robin":{}}]
}
			`,
			wantErr: true,
		},
		{
			name: "good",
			js: `{
  "clusterName": "test-cluster",
  "edsServiceName": "test-eds-service",
  "lrsLoadReportingServerName": "test-lrs-name",
  "locality": {
    "region": "test-region",
    "zone": "test-zone",
    "subZone": "test-sub-zone"
  },
  "childPolicy":[{"round_robin":{}}]
}
			`,
			want: &lbConfig{
				ClusterName:                testClusterName,
				EdsServiceName:             testServiceName,
				LrsLoadReportingServerName: testLRSServerName,
				Locality: &xdsinternal.LocalityID{
					Region:  "test-region",
					Zone:    "test-zone",
					SubZone: "test-sub-zone",
				},
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name:   roundrobin.Name,
					Config: nil,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig([]byte(tt.js))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("parseConfig() got = %v, want %v, diff: %s", got, tt.want, diff)
			}
		})
	}
}
