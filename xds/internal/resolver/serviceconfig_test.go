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
	_ "google.golang.org/grpc/xds/internal/balancer/xdsrouting"
	"google.golang.org/grpc/xds/internal/client"
	xdsclient "google.golang.org/grpc/xds/internal/client"
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
	testRoutingJSON = `{"loadBalancingConfig":[{
    "xds_routing_experimental": {
      "action":{
        "cluster_1_cluster_2_0":{
          "childPolicy":[{
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
          }]
        }
      },

      "route":[{
        "path":"/service_1/method_1",
        "action":"cluster_1_cluster_2_0"
      }]
    }
    }]}
`
	testRoutingAllMatchersJSON = `{"loadBalancingConfig":[{
    "xds_routing_experimental": {
      "action":{
        "cds:cluster_1":{
          "childPolicy":[{
            "cds_experimental":{"cluster":"cluster_1"}
          }]
        },
        "cds:cluster_2":{
          "childPolicy":[{
            "cds_experimental":{"cluster":"cluster_2"}
          }]
        },
        "cds:cluster_3":{
          "childPolicy":[{
            "cds_experimental":{"cluster":"cluster_3"}
          }]
        }
      },

      "route":[{
        "path":"/service_1/method_1",
        "action":"cds:cluster_1"
      },
      {
        "prefix":"/service_2/method_1",
        "action":"cds:cluster_1"
      },
      {
        "regex":"^/service_2/method_3$",
        "action":"cds:cluster_1"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "exactMatch":"value-1", "invertMatch":true}],
        "action":"cds:cluster_2"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "regexMatch":"^value-1$"}],
        "action":"cds:cluster_2"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "rangeMatch":{"start":-1, "end":7}}],
        "action":"cds:cluster_3"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "presentMatch":true}],
        "action":"cds:cluster_3"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "prefixMatch":"value-1"}],
        "action":"cds:cluster_2"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "suffixMatch":"value-1"}],
        "action":"cds:cluster_2"
      },
      {
        "prefix":"",
        "matchFraction":{"value": 31415},
        "action":"cds:cluster_3"
      }]
    }
    }]}
`
)

func TestWeightedClusterToJSON(t *testing.T) {
	tests := []struct {
		name     string
		wc       map[string]uint32
		wantJSON string // wantJSON is not to be compared verbatim.
	}{
		{
			name:     "one cluster only",
			wc:       map[string]uint32{testCluster1: 1},
			wantJSON: testClusterOnlyJSON,
		},
		{
			name:     "empty weighted clusters",
			wc:       nil,
			wantJSON: testWeightedCDSNoChildJSON,
		},
		{
			name: "weighted clusters",
			wc: map[string]uint32{
				"cluster_1": 75,
				"cluster_2": 25,
			},
			wantJSON: testWeightedCDSJSON,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := weightedClusterToJSON(tt.wc)
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

func TestRoutesToJSON(t *testing.T) {
	tests := []struct {
		name     string
		routes   []*xdsclient.Route
		wantJSON string
		wantErr  bool
	}{
		{
			name: "one route",
			routes: []*xdsclient.Route{{
				Path:   newStringP("/service_1/method_1"),
				Action: map[string]uint32{"cluster_1": 75, "cluster_2": 25},
			}},
			wantJSON: testRoutingJSON,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := (&xdsResolver{}).routesToJSON(tt.routes)
			if err != nil {
				t.Errorf("routesToJSON returned error: %v", err)
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

func TestServiceUpdateToJSON(t *testing.T) {
	tests := []struct {
		name     string
		su       client.ServiceUpdate
		wantJSON string
		wantErr  bool
	}{
		{
			name: "weighted clusters",
			su: client.ServiceUpdate{WeightedCluster: map[string]uint32{
				"cluster_1": 75,
				"cluster_2": 25,
			}},
			wantJSON: testWeightedCDSJSON,
			wantErr:  false,
		},
		{
			name: "routing",
			su: client.ServiceUpdate{
				Routes: []*xdsclient.Route{{
					Path:   newStringP("/service_1/method_1"),
					Action: map[string]uint32{"cluster_1": 75, "cluster_2": 25},
				}},
			},
			wantJSON: testRoutingJSON,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotJSON, err := (&xdsResolver{}).serviceUpdateToJSON(tt.su)
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

// Two updates to the same resolver, test that action names are reused.
func TestServiceUpdateToJSON_TwoConfig_UpdateActions(t *testing.T) {
}

func newStringP(s string) *string {
	return &s
}
