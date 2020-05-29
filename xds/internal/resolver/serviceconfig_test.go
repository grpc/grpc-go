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

package resolver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/serviceconfig"
	_ "google.golang.org/grpc/xds/internal/balancer/weightedtarget"
	"google.golang.org/grpc/xds/internal/client"
)

const (
	testCluster1        = "test-cluster-1"
	testClusterOnlyJSON = `{"loadBalancingConfig":[{
    "weighted_target_experimental": {
	  "targets": { "test-cluster-1" : { "weight":1, "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}] } }
	}
}]}`
	testWeightedCDSJSON = `{"loadBalancingConfig":[{
    "weighted_target_experimental": {
	  "targets": {
		"cluster_1" : {
		  "weight":75,
		  "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
		},
		"cluster_2" : {
		  "weight":25,
		  "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}]
		}
	  }
	}
}]}`
	testWeightedCDSNoChildJSON = `{"loadBalancingConfig":[{
    "weighted_target_experimental": {
	  "targets": {}
	}
}]}`
)

func TestServiceUpdateToJSON(t *testing.T) {
	tests := []struct {
		name     string
		su       client.ServiceUpdate
		wantJSON string // wantJSON is not to be compared verbatim.
	}{
		{
			name:     "one cluster only",
			su:       client.ServiceUpdate{WeightedCluster: map[string]uint32{testCluster1: 1}},
			wantJSON: testClusterOnlyJSON,
		},
		{
			name:     "empty weighted clusters",
			su:       client.ServiceUpdate{WeightedCluster: nil},
			wantJSON: testWeightedCDSNoChildJSON,
		},
		{
			name: "weighted clusters",
			su: client.ServiceUpdate{WeightedCluster: map[string]uint32{
				"cluster_1": 75,
				"cluster_2": 25,
			}},
			wantJSON: testWeightedCDSJSON,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := serviceUpdateToJSON(tt.su)
			if err != nil {
				t.Errorf("serviceUpdateToJSON returned error: %v", err)
				return
			}

			gotParsed := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(gotJSON)
			wantParsed := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(tt.wantJSON)

			if !internal.EqualServiceConfigForTesting(gotParsed.Config, wantParsed.Config) {
				t.Errorf("serviceUpdateToJSON() = %v, want %v", gotJSON, tt.wantJSON)
				t.Error("gotParsed: ", cmp.Diff(nil, gotParsed))
				t.Error("wantParsed: ", cmp.Diff(nil, wantParsed))
			}
		})
	}
}
