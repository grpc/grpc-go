/*
 *
 * Copyright 2023 gRPC authors.
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

// Package xdslbregistry_test contains test cases for the xDS LB Policy Registry.
package xdslbregistry_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/xdsclient/xdslbregistry"
	_ "google.golang.org/grpc/xds" // Register the xDS LB Registry Converters.
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	v3autoshardingpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/autosharding/v3"
	v3clientsideweightedroundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/client_side_weighted_round_robin/v3"
	v3leastrequestpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/least_request/v3"
	v3maglevpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/maglev/v3"
	v3pickfirstpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/pick_first/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3roundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func wrrLocalityBalancerConfig(childPolicy *internalserviceconfig.BalancerConfig) *internalserviceconfig.BalancerConfig {
	return &internalserviceconfig.BalancerConfig{
		Name: wrrlocality.Name,
		Config: &wrrlocality.LBConfig{
			ChildPolicy: childPolicy,
		},
	}
}

func (s) TestConvertToServiceConfigSuccess(t *testing.T) {
	const customLBPolicyName = "myorg.MyCustomLeastRequestPolicy"
	stub.Register(customLBPolicyName, stub.BalancerFuncs{})

	tests := []struct {
		name       string
		policy     *v3clusterpb.LoadBalancingPolicy
		wantConfig string // JSON config
		lrEnabled  bool
	}{
		{
			name: "ring_hash",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_XX_HASH,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
				},
			},
			wantConfig: `[{"ring_hash_experimental": { "minRingSize": 10, "maxRingSize": 100 }}]`,
		},
		{
			name: "least_request",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3leastrequestpb.LeastRequest{
								ChoiceCount: wrapperspb.UInt32(3),
							}),
						},
					},
				},
			},
			wantConfig: `[{"least_request_experimental": { "choiceCount": 3 }}]`,
			lrEnabled:  true,
		},
		{
			name: "pick_first_shuffle",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{
								ShuffleAddressList: true,
							}),
						},
					},
				},
			},
			wantConfig: `[{"pick_first": { "shuffleAddressList": true }}]`,
		},
		{
			name: "pick_first",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{}),
						},
					},
				},
			},
			wantConfig: `[{"pick_first": {}}]`,
		},
		{
			name: "round_robin",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: `[{"round_robin": {}}]`,
		},
		{
			name: "weighted_round_robin",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3clientsideweightedroundrobinpb.ClientSideWeightedRoundRobin{}),
						},
					},
				},
			},
			wantConfig: `[{"weighted_round_robin": {}}]`,
		},
		{
			name: "weighted_round_robin_populated",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3clientsideweightedroundrobinpb.ClientSideWeightedRoundRobin{
								EnableOobLoadReport:     wrapperspb.Bool(true),
								OobReportingPeriod:      durationpb.New(10 * time.Second),
								BlackoutPeriod:          durationpb.New(5 * time.Second),
								WeightExpirationPeriod:  durationpb.New(3 * time.Minute),
								WeightUpdatePeriod:      durationpb.New(1 * time.Second),
								ErrorUtilizationPenalty: wrapperspb.Float(1.5),
							}),
						},
					},
				},
			},
			wantConfig: `[{"weighted_round_robin": {
				"enableOobLoadReport": true,
				"oobReportingPeriod": "10s",
				"blackoutPeriod": "5s",
				"weightExpirationPeriod": "180s",
				"weightUpdatePeriod": "1s",
				"errorUtilizationPenalty": 1.5
			}}]`,
		},
		{
			name: "round_robin_ring_hash_use_first_supported",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3roundrobinpb.RoundRobin{}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_XX_HASH,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
				},
			},
			wantConfig: `[{"round_robin": {}}]`,
		},
		{
			name: "pf_rr_use_pick_first",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{
								ShuffleAddressList: true,
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: `[{"pick_first": { "shuffleAddressList": true }}]`,
		},
		{
			name: "custom_lb_type_v3_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// The type not registered in gRPC Policy registry.
							// Should fallback to next policy in list.
							TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
								Value:   &structpb.Struct{},
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: `[{"myorg.MyCustomLeastRequestPolicy": {}}]`,
		},
		{
			name: "custom_lb_type_v1_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v1xdsudpatypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: `[{"myorg.MyCustomLeastRequestPolicy": {}}]`,
		},
		{
			name: "wrr_locality_child_round_robin",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, &v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: `[{"xds_wrr_locality_experimental": { "childPolicy": [{"round_robin": {}}] }}]`,
		},
		{
			name: "wrr_locality_child_custom_lb_type_v3_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, &v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: `[{"xds_wrr_locality_experimental": { "childPolicy": [{"myorg.MyCustomLeastRequestPolicy": {}}] }}]`,
		},
		{
			name: "on-the-boundary-of-recursive-limit",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, &v3roundrobinpb.RoundRobin{}))))))))))))))),
						},
					},
				},
			},
			wantConfig: jsonMarshal(t, wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(wrrLocalityBalancerConfig(&internalserviceconfig.BalancerConfig{
				Name: "round_robin",
			})))))))))))))))),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := xdslbregistry.ConvertToServiceConfig(test.policy, 0, xdslbregistry.ConverterOptions{})
			if err != nil {
				t.Fatalf("ConvertToServiceConfig(%s) failed: %v", pretty.ToJSON(test.policy), err)
			}
			if res.GRPCServices != nil {
				t.Fatalf("ConvertToServiceConfig(%s) returned GRPCServices %v, want nil", pretty.ToJSON(test.policy), res.GRPCServices)
			}
			// got and want must be unmarshalled since JSON strings shouldn't
			// generally be directly compared.
			var got []map[string]any
			if err := json.Unmarshal(res.LBConfig, &got); err != nil {
				t.Fatalf("Error unmarshalling rawJSON (%q): %v", res.LBConfig, err)
			}
			var want []map[string]any
			if err := json.Unmarshal(json.RawMessage(test.wantConfig), &want); err != nil {
				t.Fatalf("Error unmarshalling wantConfig (%q): %v", test.wantConfig, err)
			}
			if diff := cmp.Diff(got, want); diff != "" {
				t.Fatalf("ConvertToServiceConfig() got unexpected output, diff (-got +want): %v", diff)
			}
		})
	}
}

func jsonMarshal(t *testing.T, x any) string {
	t.Helper()
	js, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("Error marshalling to JSON (%+v): %v", x, err)
	}
	return string(js)
}

// TestConvertToServiceConfigFailure tests failure cases of the xDS LB registry
// of converting proto configuration to JSON configuration.
func (s) TestConvertToServiceConfigFailure(t *testing.T) {
	tests := []struct {
		name    string
		policy  *v3clusterpb.LoadBalancingPolicy
		wantErr string
	}{
		{
			name: "not xx_hash function",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_MURMUR_HASH_2,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
				},
			},
			wantErr: "unsupported ring_hash hash function",
		},
		{
			name: "no-supported-policy",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// The type not registered in gRPC Policy registry.
							TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
								Value:   &structpb.Struct{},
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// Maglev is not yet supported by gRPC.
							TypedConfig: testutils.MarshalAny(t, &v3maglevpb.Maglev{}),
						},
					},
				},
			},
			wantErr: "no supported policy found in policy list",
		},
		{
			name: "exceeds-boundary-of-recursive-limit-by-1",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, wrrLocality(t, &v3roundrobinpb.RoundRobin{})))))))))))))))),
						},
					},
				},
			},
			wantErr: "exceeds max depth",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := xdslbregistry.ConvertToServiceConfig(test.policy, 0, xdslbregistry.ConverterOptions{})
			// Test the error substring to test the different root causes of
			// errors. This is more brittle over time, but it's important to
			// test the root cause of the errors emitted from the
			// ConvertToServiceConfig function call. Also, this package owns the
			// error strings so breakages won't come unexpectedly.
			if gotErr == nil || !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ConvertToServiceConfig() = %v, wantErr %v", gotErr, test.wantErr)
			}
		})
	}
}

// TestConvertToServiceConfig_GRPCServices tests that the GrpcServices returned
// by converters are returned by the xDS LB policy registry only for the policy
// that is selected, that converters of policies with child policies propagate
// the GrpcServices returned for their children, and that the converter options
// are passed to converters at every level of the LB policy tree.
func (s) TestConvertToServiceConfig_GRPCServices(t *testing.T) {
	bootstrapCfg := &bootstrap.Config{}
	serverCfg := &bootstrap.ServerConfig{}
	opts := xdslbregistry.ConverterOptions{BootstrapConfig: bootstrapCfg, ServerConfig: serverCfg}

	// Register a converter for the StringValue proto type, which pretends that
	// the LB policy configuration being converted contains a GrpcService proto
	// whose target URI is the value of the StringValue.
	const typeURL = "type.googleapis.com/google.protobuf.StringValue"
	xdslbregistry.Register(typeURL, func(rawProto []byte, _ int, gotOpts xdslbregistry.ConverterOptions) (*xdslbregistry.ConverterResult, error) {
		if gotOpts != opts {
			return nil, fmt.Errorf("converter invoked with options %+v, want %+v", gotOpts, opts)
		}
		sv := &wrapperspb.StringValue{}
		if err := proto.Unmarshal(rawProto, sv); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
		}
		return &xdslbregistry.ConverterResult{
			LBConfig:     json.RawMessage(`[{"round_robin": {}}]`),
			GRPCServices: map[string]*grpcservice.Config{sv.GetValue(): {TargetURI: sv.GetValue()}},
		}, nil
	})
	t.Cleanup(func() { xdslbregistry.UnregisterForTesting(typeURL) })

	tests := []struct {
		name             string
		policy           *v3clusterpb.LoadBalancingPolicy
		wantConfig       string
		wantGRPCServices map[string]*grpcservice.Config
	}{
		{
			name: "top-level-policy",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, wrapperspb.String("target-1")),
						},
					},
				},
			},
			wantConfig:       `[{"round_robin": {}}]`,
			wantGRPCServices: map[string]*grpcservice.Config{"target-1": {TargetURI: "target-1"}},
		},
		{
			name: "unsupported-policy-skipped",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// The type not registered in gRPC Policy registry.
							TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
								Value:   &structpb.Struct{},
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, wrapperspb.String("target-2")),
						},
					},
				},
			},
			wantConfig:       `[{"round_robin": {}}]`,
			wantGRPCServices: map[string]*grpcservice.Config{"target-2": {TargetURI: "target-2"}},
		},
		{
			name: "policy-not-selected",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, wrapperspb.String("target-3")),
						},
					},
				},
			},
			wantConfig: `[{"pick_first": {}}]`,
		},
		{
			name: "child-of-wrr-locality",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, wrapperspb.String("target-4")),
						},
					},
				},
			},
			wantConfig:       `[{"xds_wrr_locality_experimental": {"childPolicy": [{"round_robin": {}}]}}]`,
			wantGRPCServices: map[string]*grpcservice.Config{"target-4": {TargetURI: "target-4"}},
		},
		{
			name: "grandchild-of-wrr-locality",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, wrrLocality(t, wrapperspb.String("target-5"))),
						},
					},
				},
			},
			wantConfig:       `[{"xds_wrr_locality_experimental": {"childPolicy": [{"xds_wrr_locality_experimental": {"childPolicy": [{"round_robin": {}}]}}]}}]`,
			wantGRPCServices: map[string]*grpcservice.Config{"target-5": {TargetURI: "target-5"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifyConvertToServiceConfig(t, test.policy, opts, test.wantConfig, test.wantGRPCServices)
		})
	}
}

// verifyConvertToServiceConfig converts the given LB policy proto using opts
// and verifies that the resulting JSON LB configuration and GRPCServices map
// match the expected values. It returns the result of the conversion.
func verifyConvertToServiceConfig(t *testing.T, policy *v3clusterpb.LoadBalancingPolicy, opts xdslbregistry.ConverterOptions, wantConfig string, wantGRPCServices map[string]*grpcservice.Config) *xdslbregistry.ConverterResult {
	t.Helper()
	res, err := xdslbregistry.ConvertToServiceConfig(policy, 0, opts)
	if err != nil {
		t.Fatalf("ConvertToServiceConfig(%s) failed: %v", pretty.ToJSON(policy), err)
	}
	var got, want []map[string]any
	if err := json.Unmarshal(res.LBConfig, &got); err != nil {
		t.Fatalf("Error unmarshalling LB config (%q): %v", res.LBConfig, err)
	}
	if err := json.Unmarshal([]byte(wantConfig), &want); err != nil {
		t.Fatalf("Error unmarshalling wantConfig (%q): %v", wantConfig, err)
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("ConvertToServiceConfig() returned unexpected LB config, diff (-got +want): %v", diff)
	}
	if diff := cmp.Diff(res.GRPCServices, wantGRPCServices); diff != "" {
		t.Errorf("ConvertToServiceConfig() returned unexpected GRPCServices, diff (-got +want): %v", diff)
	}
	return res
}

// closeGRPCServices releases the credentials owned by the GrpcService
// configurations in res.
func closeGRPCServices(res *xdslbregistry.ConverterResult) {
	for _, cfg := range res.GRPCServices {
		cfg.Close()
	}
}

// wrrLocality is a helper that takes a proto message and returns a
// WrrLocalityProto with the proto message marshaled into a proto.Any as a
// child.
func wrrLocality(t *testing.T, m proto.Message) *v3wrrlocalitypb.WrrLocality {
	return &v3wrrlocalitypb.WrrLocality{
		EndpointPickingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: testutils.MarshalAny(t, m),
					},
				},
			},
		},
	}
}

// wrrLocalityAny takes a proto message and returns a wrr locality proto
// marshaled as an any with an any child set to the marshaled proto message.
func wrrLocalityAny(t *testing.T, m proto.Message) *anypb.Any {
	return testutils.MarshalAny(t, wrrLocality(t, m))
}

const insecureCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"

// autoshardingConverterOptions builds ConverterOptions for testing the
// autosharding LB policy converter. If allowedTarget is non-empty, it is added
// to the bootstrap configuration's allowed_grpc_services map with insecure
// channel credentials. If trustedServer is true, the server configuration
// includes the "trusted_xds_server" feature.
func autoshardingConverterOptions(t *testing.T, allowedTarget string, trustedServer bool) xdslbregistry.ConverterOptions {
	t.Helper()
	var allowedJSON json.RawMessage
	if allowedTarget != "" {
		allowed := map[string]json.RawMessage{
			allowedTarget: json.RawMessage(`{"channel_creds": [{"type": "insecure"}]}`),
		}
		var err error
		allowedJSON, err = json.Marshal(allowed)
		if err != nil {
			t.Fatalf("Failed to marshal allowed_grpc_services: %v", err)
		}
	}
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             json.RawMessage(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:                json.RawMessage(`{"id": "test-node"}`),
		AllowedGRPCServices: allowedJSON,
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	bootstrapCfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}
	serverOpts := bootstrap.ServerConfigTestingOptions{URI: "xds-server:1234"}
	if trustedServer {
		serverOpts.ServerFeatures = []string{"trusted_xds_server"}
	}
	serverCfg, err := bootstrap.ServerConfigForTesting(serverOpts)
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	return xdslbregistry.ConverterOptions{
		BootstrapConfig: bootstrapCfg,
		ServerConfig:    serverCfg,
	}
}

// googleGrpcService returns a GrpcService proto configured with the GoogleGrpc
// target specifier set to targetURI and optional channel credential plugins.
func googleGrpcService(targetURI string, channelCreds ...*anypb.Any) *v3corepb.GrpcService {
	return &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                targetURI,
				ChannelCredentialsPlugin: channelCreds,
			},
		},
	}
}

// Tests successful conversion of the autosharding LB policy configuration when
// received from an untrusted xDS management server, verifying that credentials
// from allowed_grpc_services in the bootstrap configuration are used.
func (s) TestConvertToServiceConfig_Autosharding_UntrustedServer(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	opts := autoshardingConverterOptions(t, shardingServiceURI, false)
	xdsWithInsecureFallbackPlugin := testutils.MarshalAny(t, &xdscredspb.XdsCredentials{
		FallbackCredentials: &anypb.Any{TypeUrl: insecureCredsTypeURL},
	})

	wantGS := &grpcservice.Config{
		TargetURI:          shardingServiceURI,
		ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil),
	}
	wantKey := wantGS.Key()
	wantGRPCServices := map[string]*grpcservice.Config{wantKey: wantGS}

	tests := []struct {
		name       string
		policy     *v3clusterpb.LoadBalancingPolicy
		wantConfig string
	}{
		{
			name: "default_fields",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			wantConfig: fmt.Sprintf(`[{"autosharding_experimental": {
				"channelFactoryKey": %q,
				"autoshardingTarget": "my-target",
				"keyHeaderName": "my-key-header"
			}}]`, wantKey),
		},
		{
			name: "all_fields_populated",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:              googleGrpcService(shardingServiceURI),
								AutoshardingTarget:       "my-target-%s",
								KeyHeaderName:            "my-key-header",
								EnableFallback:           true,
								InitialAssignmentTimeout: durationpb.New(30 * time.Second),
							}),
						},
					},
				},
			},
			wantConfig: fmt.Sprintf(`[{"autosharding_experimental": {
				"channelFactoryKey": %q,
				"autoshardingTarget": "my-target-%%s",
				"keyHeaderName": "my-key-header",
				"enableFallback": true,
				"initialAssignmentTimeout": "30s"
			}}]`, wantKey),
		},
		{
			name: "child_of_wrr_locality",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI),
								AutoshardingTarget: "my-target-%s",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			wantConfig: fmt.Sprintf(`[{"xds_wrr_locality_experimental": {
				"childPolicy": [{"autosharding_experimental": {
					"channelFactoryKey": %q,
					"autoshardingTarget": "my-target-%%s",
					"keyHeaderName": "my-key-header"
				}}]
			}}]`, wantKey),
		},
		{
			// When the server is untrusted, channel credentials in the
			// GrpcService proto are ignored in favor of allowed_grpc_services
			// from the bootstrap config.
			name: "ignores_proto_creds_and_uses_allowlist",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI, xdsWithInsecureFallbackPlugin),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			wantConfig: fmt.Sprintf(`[{"autosharding_experimental": {
				"channelFactoryKey": %q,
				"autoshardingTarget": "my-target",
				"keyHeaderName": "my-key-header"
			}}]`, wantKey),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := verifyConvertToServiceConfig(t, test.policy, opts, test.wantConfig, wantGRPCServices)
			t.Cleanup(func() { closeGRPCServices(res) })
		})
	}
}

// Tests successful conversion of the autosharding LB policy configuration when
// received from a trusted xDS management server, verifying that channel
// credentials from the GrpcService proto are used instead of
// allowed_grpc_services from the bootstrap configuration.
func (s) TestConvertToServiceConfig_Autosharding_TrustedServer(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	xdsWithInsecureFallbackPlugin := testutils.MarshalAny(t, &xdscredspb.XdsCredentials{
		FallbackCredentials: insecurePlugin,
	})

	wantInsecureGS := &grpcservice.Config{
		TargetURI: shardingServiceURI,
		ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{
			Type: insecurePlugin.GetTypeUrl(),
			Data: insecurePlugin.GetValue(),
		}, nil),
	}
	wantInsecureKey := wantInsecureGS.Key()

	wantXDSCredsGS := &grpcservice.Config{
		TargetURI: shardingServiceURI,
		ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{
			Type: xdsWithInsecureFallbackPlugin.GetTypeUrl(),
			Data: xdsWithInsecureFallbackPlugin.GetValue(),
		}, nil),
	}
	wantXDSCredsKey := wantXDSCredsGS.Key()

	tests := []struct {
		name             string
		policy           *v3clusterpb.LoadBalancingPolicy
		opts             xdslbregistry.ConverterOptions
		wantConfig       string
		wantGRPCServices map[string]*grpcservice.Config
	}{
		{
			// When the server is trusted, allowed_grpc_services is not
			// required and the channel credentials sent by the management
			// server in the GrpcService proto are used.
			name: "without_allowlist_uses_proto_insecure_creds",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI, insecurePlugin),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			opts: autoshardingConverterOptions(t, "", true),
			wantConfig: fmt.Sprintf(`[{"autosharding_experimental": {
				"channelFactoryKey": %q,
				"autoshardingTarget": "my-target",
				"keyHeaderName": "my-key-header"
			}}]`, wantInsecureKey),
			wantGRPCServices: map[string]*grpcservice.Config{wantInsecureKey: wantInsecureGS},
		},
		{
			// Even when allowed_grpc_services contains an entry for the target
			// URI, a trusted server's GrpcService proto credentials are used
			// instead of the bootstrap allowlist.
			name: "ignores_allowlist_and_uses_proto_xds_creds",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI, xdsWithInsecureFallbackPlugin),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			opts: autoshardingConverterOptions(t, shardingServiceURI, true),
			wantConfig: fmt.Sprintf(`[{"autosharding_experimental": {
				"channelFactoryKey": %q,
				"autoshardingTarget": "my-target",
				"keyHeaderName": "my-key-header"
			}}]`, wantXDSCredsKey),
			wantGRPCServices: map[string]*grpcservice.Config{wantXDSCredsKey: wantXDSCredsGS},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := verifyConvertToServiceConfig(t, test.policy, test.opts, test.wantConfig, test.wantGRPCServices)
			t.Cleanup(func() { closeGRPCServices(res) })
		})
	}
}

// Tests that the autosharding LB policy is treated as unsupported and skipped
// in favor of the next policy in the list when the
// GRPC_XDS_EXPERIMENTAL_ENABLE_AUTOSHARDING_LB environment variable is
// disabled.
func (s) TestConvertToServiceConfig_Autosharding_EnvVarDisabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, false)

	policy := &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
			{
				TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
					TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
						GrpcService:        googleGrpcService("dns:///sharding-service:443"),
						AutoshardingTarget: "my-target",
						KeyHeaderName:      "my-key-header",
					}),
				},
			},
			{
				TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
					TypedConfig: testutils.MarshalAny(t, &v3roundrobinpb.RoundRobin{}),
				},
			},
		},
	}

	verifyConvertToServiceConfig(t, policy, xdslbregistry.ConverterOptions{}, `[{"round_robin": {}}]`, nil)
}

// Tests that converting an invalid autosharding LB policy configuration fails
// with an expected error.
func (s) TestConvertToServiceConfig_Autosharding_Failure(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	untrustedOpts := autoshardingConverterOptions(t, shardingServiceURI, false)
	trustedOptsWithAllowlist := autoshardingConverterOptions(t, shardingServiceURI, true)

	tests := []struct {
		name    string
		policy  *v3clusterpb.LoadBalancingPolicy
		opts    xdslbregistry.ConverterOptions
		wantErr string
	}{
		{
			name: "corrupt_proto",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: &anypb.Any{
								TypeUrl: "type.googleapis.com/envoy.extensions.load_balancing_policies.autosharding.v3.Autosharding",
								Value:   []byte{0xff, 0xff, 0xff},
							},
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "failed to unmarshal autosharding LB config proto",
		},
		{
			name: "missing_grpc_service",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "grpc_service field is required in autosharding LB config",
		},
		{
			name: "untrusted_server_grpc_service_not_allowlisted",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService("dns:///not-allowlisted:443"),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "is not present in allowed_grpc_services",
		},
		{
			// Even when the target is present in allowed_grpc_services, a
			// trusted server ignores allowed_grpc_services and requires channel
			// credentials in the GrpcService proto.
			name: "trusted_server_missing_proto_channel_creds",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:        googleGrpcService(shardingServiceURI),
								AutoshardingTarget: "my-target",
								KeyHeaderName:      "my-key-header",
							}),
						},
					},
				},
			},
			opts:    trustedOptsWithAllowlist,
			wantErr: "no supported channel credentials found",
		},
		{
			name: "invalid_initial_assignment_timeout_proto",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:              googleGrpcService(shardingServiceURI),
								AutoshardingTarget:       "my-target",
								KeyHeaderName:            "my-key-header",
								InitialAssignmentTimeout: &durationpb.Duration{Seconds: 315576000001},
							}),
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "invalid initial_assignment_timeout field in autosharding LB config",
		},
		{
			name: "zero_initial_assignment_timeout",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:              googleGrpcService(shardingServiceURI),
								AutoshardingTarget:       "my-target",
								KeyHeaderName:            "my-key-header",
								InitialAssignmentTimeout: durationpb.New(0),
							}),
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "initial_assignment_timeout field in autosharding LB config must be strictly positive",
		},
		{
			name: "negative_initial_assignment_timeout",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
								GrpcService:              googleGrpcService(shardingServiceURI),
								AutoshardingTarget:       "my-target",
								KeyHeaderName:            "my-key-header",
								InitialAssignmentTimeout: durationpb.New(-5 * time.Second),
							}),
						},
					},
				},
			},
			opts:    untrustedOpts,
			wantErr: "initial_assignment_timeout field in autosharding LB config must be strictly positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := xdslbregistry.ConvertToServiceConfig(test.policy, 0, test.opts)
			if gotErr == nil || !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ConvertToServiceConfig() = %v, wantErr %v", gotErr, test.wantErr)
			}
		})
	}
}
