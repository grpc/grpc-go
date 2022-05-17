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

package outlierdetection

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpctest"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestParseConfig verifies the ParseConfig() method in the CDS balancer.
func (s) TestParseConfig(t *testing.T) {
	bb := balancer.Get(Name)
	if bb == nil {
		t.Fatalf("balancer.Get(%q) returned nil", Name)
	}
	parser, ok := bb.(balancer.ConfigParser)
	if !ok {
		t.Fatalf("balancer %q does not implement the ConfigParser interface", Name)
	}

	tests := []struct {
		name    string
		input   json.RawMessage // s both of these to data -> field inside odConfig
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr bool
	}{
		{
			name: "noop-lb-config",
			input: json.RawMessage(`{
				"odConfig": {
					"interval": 9223372036854775807
				}
			}`),
			wantCfg: &LBConfig{ODConfig: &ODConfig{Interval: 1<<63 - 1}},
		},
		{
			name: "good-lb-config",
			input: json.RawMessage(`{
				"odConfig": {
					"interval": 10000000000,
					"baseEjectionTime": 30000000000,
					"maxEjectionTime": 300000000000,
					"maxEjectionPercent": 10,
					"successRateEjection": {
						"stdevFactor": 1900,
						"enforcementPercentage": 100,
						"minimumHosts": 5,
						"requestVolume": 100
					},
					"failurePercentageEjection": {
						"threshold": 85,
						"enforcementPercentage": 5,
						"minimumHosts": 5,
						"requestVolume": 50
					}
				}
			}`),
			wantCfg: &LBConfig{
				ODConfig: &ODConfig{
					Interval:           10 * time.Second,
					BaseEjectionTime:   30 * time.Second,
					MaxEjectionTime:    300 * time.Second,
					MaxEjectionPercent: 10,
					SuccessRateEjection: &SuccessRateEjection{
						StdevFactor:           1900,
						EnforcementPercentage: 100,
						MinimumHosts:          5,
						RequestVolume:         100,
					},
					FailurePercentageEjection: &FailurePercentageEjection{
						Threshold:             85,
						EnforcementPercentage: 5,
						MinimumHosts:          5,
						RequestVolume:         50,
					},
				},
			},
		},
		{
			name: "interval-is-negative",
			input: json.RawMessage(`{
				"odConfig": { "interval": -10}
			}`),
			wantErr: true,
		},
		{
			name: "base-ejection-time-is-negative",
			input: json.RawMessage(`{
				"odConfig": { "baseEjectionTime": -10}
			}`),
			wantErr: true,
		},
		{
			name: "max-ejection-time-is-negative",
			input: json.RawMessage(`{
				"odConfig": { "maxEjectionTime": -10}
			}`),
			wantErr: true,
		},
		{
			name: "max-ejection-percent-is-greater-than-100",
			input: json.RawMessage(`{
				"odConfig": { "maxEjectionPercent": 150}
			}`),
			wantErr: true,
		},
		{
			name: "enforcing-success-rate-is-greater-than-100",
			input: json.RawMessage(`{
				"odConfig": {
					"successRateEjection": {
						"enforcingSuccessRate": 150,
					}
				}
			}`),
			wantErr: true,
		},
		{
			name: "failure-percentage-threshold-is-greater-than-100",
			input: json.RawMessage(`{
				"odConfig": {
					"failurePercentageEjection": {
						"threshold": 150,
					}
				}
			}`),
			wantErr: true,
		},
		{
			name: "enforcing-failure-percentage-is-greater-than-100",
			input: json.RawMessage(`{
				"odConfig": {
					"enforcingFailurePercentage": {
						"threshold": 150,
					}
				}
			}`),
			wantErr: true,
		},
		{
			name: "child-policy",
			input: json.RawMessage(`{
				"odConfig": { "interval": 9223372036854775807},
				"childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
			]
			}`),
			wantCfg: &LBConfig{
				ODConfig: &ODConfig{Interval: 1<<63 - 1},
				ChildPolicy: &internalserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(test.input)
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", string(test.input), gotErr, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if diff := cmp.Diff(gotCfg, test.wantCfg); diff != "" {
				t.Fatalf("parseConfig(%v) got unexpected output, diff (-got +want): %v", string(test.input), diff)
			}
		})
	}
}

// Equal returns whether the LBConfig is the same with the parameter.
func (lbc *LBConfig) Equal(lbc2 *LBConfig) bool {
	if lbc == nil && lbc2 == nil {
		return true
	}
	if (lbc != nil) != (lbc2 != nil) {
		return false
	}
	if !lbc.ODConfig.Equal(lbc2.ODConfig) {
		return false
	}
	return cmp.Equal(lbc.ChildPolicy, lbc2.ChildPolicy)
}
