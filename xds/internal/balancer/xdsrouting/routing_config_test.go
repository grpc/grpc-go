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

package xdsrouting

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer"
	_ "google.golang.org/grpc/xds/internal/balancer/weightedtarget"
)

const (
	testJSONConfig = `{
      "action":{
        "cds:cluster_1":{
          "childPolicy":[{
            "cds_experimental":{"cluster":"cluster_1"}
          }]
        },
        "weighted:cluster_1_cluster_2_1":{
          "childPolicy":[{
            "weighted_target_experimental":{
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
        },
        "weighted:cluster_1_cluster_3_1":{
          "childPolicy":[{
            "weighted_target_experimental":{
              "targets": {
                "cluster_1": {
                  "weight":99,
                  "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}]
                },
                "cluster_3": {
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
        "action":"cds:cluster_1"
      },
      {
        "path":"/service_1/method_2",
        "action":"cds:cluster_1"
      },
      {
        "prefix":"/service_2/method_1",
        "action":"weighted:cluster_1_cluster_2_1"
      },
      {
        "prefix":"/service_2",
        "action":"weighted:cluster_1_cluster_2_1"
      },
      {
        "regex":"^/service_2/method_3$",
        "action":"weighted:cluster_1_cluster_3_1"
      }]
    }
`

	testJSONConfigWithAllMatchers = `{
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
`

	cdsName = "cds_experimental"
	wtName  = "weighted_target_experimental"
)

var (
	cdsConfigParser = balancer.Get(cdsName).(balancer.ConfigParser)
	cdsConfigJSON1  = `{"cluster":"cluster_1"}`
	cdsConfig1, _   = cdsConfigParser.ParseConfig([]byte(cdsConfigJSON1))
	cdsConfigJSON2  = `{"cluster":"cluster_2"}`
	cdsConfig2, _   = cdsConfigParser.ParseConfig([]byte(cdsConfigJSON2))
	cdsConfigJSON3  = `{"cluster":"cluster_3"}`
	cdsConfig3, _   = cdsConfigParser.ParseConfig([]byte(cdsConfigJSON3))

	wtConfigParser = balancer.Get(wtName).(balancer.ConfigParser)
	wtConfigJSON1  = `{
	"targets": {
	  "cluster_1" : { "weight":75, "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}] },
	  "cluster_2" : { "weight":25, "childPolicy":[{"cds_experimental":{"cluster":"cluster_2"}}] }
	} }`
	wtConfig1, _  = wtConfigParser.ParseConfig([]byte(wtConfigJSON1))
	wtConfigJSON2 = `{
    "targets": {
      "cluster_1": { "weight":99, "childPolicy":[{"cds_experimental":{"cluster":"cluster_1"}}] },
      "cluster_3": { "weight":1, "childPolicy":[{"cds_experimental":{"cluster":"cluster_3"}}] }
    } }`
	wtConfig2, _ = wtConfigParser.ParseConfig([]byte(wtConfigJSON2))
)

func Test_parseConfig(t *testing.T) {
	tests := []struct {
		name    string
		js      string
		want    *lbConfig
		wantErr bool
	}{
		{
			name:    "empty json",
			js:      "",
			want:    nil,
			wantErr: true,
		},
		{
			name: "more than one path matcher", // Path matcher is oneof, so this is an error.
			js: `{
              "Action":{
                "cds:cluster_1":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]}
              },
              "Route": [{
                "path":"/service_1/method_1",
                "prefix":"/service_1/",
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "no path matcher",
			js: `{
              "Action":{
                "cds:cluster_1":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]}
              },
              "Route": [{
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "route action not found in action list",
			js: `{
              "Action":{},
              "Route": [{
                "path":"/service_1/method_1",
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "action list contains action not used",
			js: `{
              "Action":{
                "cds:cluster_1":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]},
                "cds:cluster_not_used":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]}
              },
              "Route": [{
                "path":"/service_1/method_1",
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},

		{
			name: "no header specifier in header matcher",
			js: `{
              "Action":{
                "cds:cluster_1":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]}
              },
              "Route": [{
                "path":"/service_1/method_1",
                "headers":[{"name":"header-1"}],
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},
		{
			name: "more than one header specifier in header matcher",
			js: `{
              "Action":{
                "cds:cluster_1":{ "childPolicy":[{ "cds_experimental":{"cluster":"cluster_1"} }]}
              },
              "Route": [{
                "path":"/service_1/method_1",
                "headers":[{"name":"header-1", "prefixMatch":"a", "suffixMatch":"b"}],
                "action":"cds:cluster_1"
              }]
            }`,
			want:    nil,
			wantErr: true,
		},

		{
			name: "OK with path matchers only",
			js:   testJSONConfig,
			want: &lbConfig{
				routes: []routeConfig{
					{path: "/service_1/method_1", action: "cds:cluster_1"},
					{path: "/service_1/method_2", action: "cds:cluster_1"},
					{prefix: "/service_2/method_1", action: "weighted:cluster_1_cluster_2_1"},
					{prefix: "/service_2", action: "weighted:cluster_1_cluster_2_1"},
					{regex: "^/service_2/method_3$", action: "weighted:cluster_1_cluster_3_1"},
				},
				actions: map[string]actionConfig{
					"cds:cluster_1": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: cdsName, Config: cdsConfig1},
					},
					"weighted:cluster_1_cluster_2_1": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: wtName, Config: wtConfig1},
					},
					"weighted:cluster_1_cluster_3_1": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: wtName, Config: wtConfig2},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "OK with all matchers",
			js:   testJSONConfigWithAllMatchers,
			want: &lbConfig{
				routes: []routeConfig{
					{path: "/service_1/method_1", action: "cds:cluster_1"},
					{prefix: "/service_2/method_1", action: "cds:cluster_1"},
					{regex: "^/service_2/method_3$", action: "cds:cluster_1"},

					{prefix: "", headers: []headerMatcher{{name: "header-1", exactMatch: "value-1", invertMatch: true}}, action: "cds:cluster_2"},
					{prefix: "", headers: []headerMatcher{{name: "header-1", regexMatch: "^value-1$"}}, action: "cds:cluster_2"},
					{prefix: "", headers: []headerMatcher{{name: "header-1", rangeMatch: &int64Range{start: -1, end: 7}}}, action: "cds:cluster_3"},
					{prefix: "", headers: []headerMatcher{{name: "header-1", presentMatch: true}}, action: "cds:cluster_3"},
					{prefix: "", headers: []headerMatcher{{name: "header-1", prefixMatch: "value-1"}}, action: "cds:cluster_2"},
					{prefix: "", headers: []headerMatcher{{name: "header-1", suffixMatch: "value-1"}}, action: "cds:cluster_2"},
					{prefix: "", fraction: newUInt32P(31415), action: "cds:cluster_3"},
				},
				actions: map[string]actionConfig{
					"cds:cluster_1": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: cdsName, Config: cdsConfig1},
					},
					"cds:cluster_2": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: cdsName, Config: cdsConfig2},
					},
					"cds:cluster_3": {ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: cdsName, Config: cdsConfig3},
					},
				},
			},
			wantErr: false,
		},
	}

	cmpOptions := []cmp.Option{cmp.AllowUnexported(lbConfig{}, routeConfig{}, headerMatcher{}, int64Range{})}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig([]byte(tt.js))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want, cmpOptions...) {
				t.Errorf("parseConfig() got unexpected result, diff: %v", cmp.Diff(got, tt.want, cmpOptions...))
			}
		})
	}
}

func newUInt32P(i uint32) *uint32 {
	return &i
}
