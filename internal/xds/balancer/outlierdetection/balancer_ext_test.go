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

package outlierdetection_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/xds/balancer/clusterimpl"
	"google.golang.org/grpc/internal/xds/balancer/outlierdetection"
	"google.golang.org/grpc/serviceconfig"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestParseConfig verifies the ParseConfig() method in the Outlier Detection
// Balancer.
func (s) TestParseConfig(t *testing.T) {
	const errParseConfigName = "errParseConfigBalancer"
	stub.Register(errParseConfigName, stub.BalancerFuncs{
		ParseConfig: func(json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return nil, errors.New("some error")
		},
	})

	parser := balancer.Get(outlierdetection.Name).(balancer.ConfigParser)
	const (
		defaultInterval                       = iserviceconfig.Duration(10 * time.Second)
		defaultBaseEjectionTime               = iserviceconfig.Duration(30 * time.Second)
		defaultMaxEjectionTime                = iserviceconfig.Duration(300 * time.Second)
		defaultMaxEjectionPercent             = 10
		defaultSuccessRateStdevFactor         = 1900
		defaultEnforcingSuccessRate           = 100
		defaultSuccessRateMinimumHosts        = 5
		defaultSuccessRateRequestVolume       = 100
		defaultFailurePercentageThreshold     = 85
		defaultEnforcingFailurePercentage     = 0
		defaultFailurePercentageMinimumHosts  = 5
		defaultFailurePercentageRequestVolume = 50
	)
	tests := []struct {
		name    string
		input   string
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr string
	}{
		{
			name: "Default",
			input: `{
				"childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},

		{
			name: "TopLevelSet",
			input: `{
				"interval": "15s",
				"maxEjectionTime": "350s",
				"childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			// Should get set fields + defaults for unset fields.
			wantCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(15 * time.Second),
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    iserviceconfig.Duration(350 * time.Second),
				MaxEjectionPercent: defaultMaxEjectionPercent,
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "SuccessRate_Defaults",
			input: `{
				"successRateEjection": {},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			// Should get defaults of success-rate-ejection struct.
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           defaultSuccessRateStdevFactor,
					EnforcementPercentage: defaultEnforcingSuccessRate,
					MinimumHosts:          defaultSuccessRateMinimumHosts,
					RequestVolume:         defaultSuccessRateRequestVolume,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "SuccessRate_Partial",
			input: `{
				"successRateEjection": {
					"stdevFactor": 1000,
					"minimumHosts": 5
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			// Should get set fields + defaults for others in success rate
			// ejection layer.
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1000,
					EnforcementPercentage: defaultEnforcingSuccessRate,
					MinimumHosts:          5,
					RequestVolume:         defaultSuccessRateRequestVolume,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "SuccessRate_Full",
			input: `{
				"successRateEjection": {
					"stdevFactor": 1000,
					"enforcementPercentage": 50,
					"minimumHosts": 5,
					"requestVolume": 50
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1000,
					EnforcementPercentage: 50,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "FailurePercentage_Defaults",
			input: `{
				"failurePercentageEjection": {},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			// Should get defaults of failure percentage ejection layer.
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             defaultFailurePercentageThreshold,
					EnforcementPercentage: defaultEnforcingFailurePercentage,
					MinimumHosts:          defaultFailurePercentageMinimumHosts,
					RequestVolume:         defaultFailurePercentageRequestVolume,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "FailurePercentage_Partial",
			input: `{
				"failurePercentageEjection": {
					"threshold": 80,
					"minimumHosts": 10
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			// Should get set fields + defaults for others in success rate
			// ejection layer.
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             80,
					EnforcementPercentage: defaultEnforcingFailurePercentage,
					MinimumHosts:          10,
					RequestVolume:         defaultFailurePercentageRequestVolume,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "FailurePercentage_Full",
			input: `{
				"failurePercentageEjection": {
					"threshold": 80,
					"enforcementPercentage": 100,
					"minimumHosts": 10,
					"requestVolume": 40
                },
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &outlierdetection.LBConfig{
				Interval:           defaultInterval,
				BaseEjectionTime:   defaultBaseEjectionTime,
				MaxEjectionTime:    defaultMaxEjectionTime,
				MaxEjectionPercent: defaultMaxEjectionPercent,
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             80,
					EnforcementPercentage: 100,
					MinimumHosts:          10,
					RequestVolume:         40,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{ // to make sure zero values aren't overwritten by defaults
			name: "AllFields_Zero",
			input: `{
				"interval": "0s",
				"baseEjectionTime": "0s",
				"maxEjectionTime": "0s",
				"maxEjectionPercent": 0,
				"successRateEjection": {
					"stdevFactor": 0,
					"enforcementPercentage": 0,
					"minimumHosts": 0,
					"requestVolume": 0
				},
				"failurePercentageEjection": {
					"threshold": 0,
					"enforcementPercentage": 0,
					"minimumHosts": 0,
					"requestVolume": 0
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &outlierdetection.LBConfig{
				SuccessRateEjection:       &outlierdetection.SuccessRateEjection{},
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name: "AllFields_Set",
			input: `{
				"interval": "10s",
				"baseEjectionTime": "30s",
				"maxEjectionTime": "300s",
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
				},
                "childPolicy": [
				{
					"xds_cluster_impl_experimental": {
						"cluster": "test_cluster"
					}
				}
				]
			}`,
			wantCfg: &outlierdetection.LBConfig{
				Interval:           iserviceconfig.Duration(10 * time.Second),
				BaseEjectionTime:   iserviceconfig.Duration(30 * time.Second),
				MaxEjectionTime:    iserviceconfig.Duration(300 * time.Second),
				MaxEjectionPercent: 10,
				SuccessRateEjection: &outlierdetection.SuccessRateEjection{
					StdevFactor:           1900,
					EnforcementPercentage: 100,
					MinimumHosts:          5,
					RequestVolume:         100,
				},
				FailurePercentageEjection: &outlierdetection.FailurePercentageEjection{
					Threshold:             85,
					EnforcementPercentage: 5,
					MinimumHosts:          5,
					RequestVolume:         50,
				},
				ChildPolicy: &iserviceconfig.BalancerConfig{
					Name: "xds_cluster_impl_experimental",
					Config: &clusterimpl.LBConfig{
						Cluster: "test_cluster",
					},
				},
			},
		},
		{
			name:    "Interval_Negative",
			input:   `{"interval": "-10s"}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.interval = -10s; must be >= 0",
		},
		{
			name:    "BaseEjectionTime_Negative",
			input:   `{"baseEjectionTime": "-10s"}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.base_ejection_time = -10s; must be >= 0",
		},
		{
			name:    "MaxEjectionTime_Negative",
			input:   `{"maxEjectionTime": "-10s"}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.max_ejection_time = -10s; must be >= 0",
		},
		{
			name:    "MaxEjectionPercent_TooHigh",
			input:   `{"maxEjectionPercent": 150}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.max_ejection_percent = 150; must be <= 100",
		},
		{
			name: "SuccessRate_Enforcement_TooHigh",
			input: `{
				"successRateEjection": {
					"enforcementPercentage": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.SuccessRateEjection.enforcement_percentage = 150; must be <= 100",
		},
		{
			name: "FailurePercentage_Threshold_TooHigh",
			input: `{
				"failurePercentageEjection": {
					"threshold": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.FailurePercentageEjection.threshold = 150; must be <= 100",
		},
		{
			name: "FailurePercentage_Enforcement_TooHigh",
			input: `{
				"failurePercentageEjection": {
					"enforcementPercentage": 150
				}
			}`,
			wantErr: "OutlierDetectionLoadBalancingConfig.FailurePercentageEjection.enforcement_percentage = 150; must be <= 100",
		},
		{
			name: "ChildPolicy_ParseError",
			input: `{
				"childPolicy": [
				{
					"errParseConfigBalancer": {
						"cluster": "test_cluster"
					}
				}
			]
			}`,
			wantErr: "error parsing loadBalancingConfig for policy \"errParseConfigBalancer\"",
		},
		{
			name: "ChildPolicy_NotSupported",
			input: `{
				"childPolicy": [
				{
					"doesNotExistBalancer": {
						"cluster": "test_cluster"
					}
				}
			]
			}`,
			wantErr: "invalid loadBalancingConfig: no supported policies found",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(json.RawMessage(test.input))
			if gotErr != nil && !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if (gotErr != nil) != (test.wantErr != "") {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if test.wantErr != "" {
				return
			}
			if diff := cmp.Diff(gotCfg, test.wantCfg); diff != "" {
				t.Fatalf("parseConfig(%v) got unexpected output, diff (-got +want): %v", string(test.input), diff)
			}
		})
	}
}
