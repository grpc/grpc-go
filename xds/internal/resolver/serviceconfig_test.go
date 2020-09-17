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
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/serviceconfig"
	_ "google.golang.org/grpc/xds/internal/balancer/weightedtarget"
	_ "google.golang.org/grpc/xds/internal/balancer/xdsrouting"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	testCluster1           = "test-cluster-1"
	testOneClusterOnlyJSON = `{"loadBalancingConfig":[{
    "xds_routing_experimental":{
      "action":{
        "test-cluster-1_0":{
          "childPolicy":[{
            "weighted_target_experimental":{
              "targets":{
                "test-cluster-1":{
                  "weight":1,
                  "childPolicy":[{"cds_experimental":{"cluster":"test-cluster-1"}}]
                }
              }}}]
        }
      },
      "route":[{"prefix":"","action":"test-cluster-1_0"}]
    }}]}`
	testWeightedCDSJSON = `{"loadBalancingConfig":[{
    "xds_routing_experimental":{
      "action":{
        "cluster_1_cluster_2_1":{
          "childPolicy":[{
            "weighted_target_experimental":{
              "targets":{
                "cluster_1":{
                  "weight":75,
                  "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
                },
                "cluster_2":{
                  "weight":25,
                  "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}]
                }
              }}}]
        }
      },
      "route":[{"prefix":"","action":"cluster_1_cluster_2_1"}]
    }}]}`

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
        "cluster_1_0":{
          "childPolicy":[{
			"weighted_target_experimental": {
			  "targets": {
				"cluster_1" : {
				  "weight":1,
				  "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
				}
			  }
			}
          }]
        },
        "cluster_2_0":{
          "childPolicy":[{
			"weighted_target_experimental": {
			  "targets": {
				"cluster_2" : {
				  "weight":1,
				  "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}]
				}
			  }
			}
          }]
        },
        "cluster_3_0":{
          "childPolicy":[{
			"weighted_target_experimental": {
			  "targets": {
				"cluster_3" : {
				  "weight":1,
				  "childPolicy":[{"cds_experimental":{"cluster":"cluster_3"}}]
				}
			  }
			}
          }]
        }
      },

      "route":[{
        "path":"/service_1/method_1",
        "action":"cluster_1_0"
      },
      {
        "prefix":"/service_2/method_1",
        "action":"cluster_1_0"
      },
      {
        "regex":"^/service_2/method_3$",
        "action":"cluster_1_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "exactMatch":"value-1", "invertMatch":true}],
        "action":"cluster_2_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "regexMatch":"^value-1$"}],
        "action":"cluster_2_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "rangeMatch":{"start":-1, "end":7}}],
        "action":"cluster_3_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "presentMatch":true}],
        "action":"cluster_3_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "prefixMatch":"value-1"}],
        "action":"cluster_2_0"
      },
      {
        "prefix":"",
        "headers":[{"name":"header-1", "suffixMatch":"value-1"}],
        "action":"cluster_2_0"
      },
      {
        "prefix":"",
        "matchFraction":{"value": 31415},
        "action":"cluster_3_0"
      }]
    }
    }]}
`
)

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
		{
			name: "all matchers",
			routes: []*xdsclient.Route{
				{
					Path:   newStringP("/service_1/method_1"),
					Action: map[string]uint32{"cluster_1": 1},
				},
				{
					Prefix: newStringP("/service_2/method_1"),
					Action: map[string]uint32{"cluster_1": 1},
				},
				{
					Regex:  newStringP("^/service_2/method_3$"),
					Action: map[string]uint32{"cluster_1": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:        "header-1",
						InvertMatch: newBoolP(true),
						ExactMatch:  newStringP("value-1"),
					}},
					Action: map[string]uint32{"cluster_2": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:       "header-1",
						RegexMatch: newStringP("^value-1$"),
					}},
					Action: map[string]uint32{"cluster_2": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:       "header-1",
						RangeMatch: &xdsclient.Int64Range{Start: -1, End: 7},
					}},
					Action: map[string]uint32{"cluster_3": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:         "header-1",
						PresentMatch: newBoolP(true),
					}},
					Action: map[string]uint32{"cluster_3": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:        "header-1",
						PrefixMatch: newStringP("value-1"),
					}},
					Action: map[string]uint32{"cluster_2": 1},
				},
				{
					Prefix: newStringP(""),
					Headers: []*xdsclient.HeaderMatcher{{
						Name:        "header-1",
						SuffixMatch: newStringP("value-1"),
					}},
					Action: map[string]uint32{"cluster_2": 1},
				},
				{
					Prefix:   newStringP(""),
					Fraction: newUint32P(31415),
					Action:   map[string]uint32{"cluster_3": 1},
				},
			},
			wantJSON: testRoutingAllMatchersJSON,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note this random number function only generates 0. This is
			// because the test doesn't handle action update, and there's only
			// one action for each cluster bundle.
			//
			// This is necessary so the output is deterministic.
			grpcrandInt63n = func(int64) int64 { return 0 }
			defer func() { grpcrandInt63n = grpcrand.Int63n }()

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
		su       xdsclient.ServiceUpdate
		wantJSON string
		wantErr  bool
	}{
		{
			name: "routing",
			su: xdsclient.ServiceUpdate{
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
			defer replaceRandNumGenerator(0)()
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

func newBoolP(b bool) *bool {
	return &b
}

func newUint32P(i uint32) *uint32 {
	return &i
}
