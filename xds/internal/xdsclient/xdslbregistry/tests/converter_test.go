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

// Package tests_test contains test cases for the xDS LB Policy Registry.
package tests_test

import (
	"encoding/json"
	"strings"
	"testing"

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3leastrequestpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/least_request/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3roundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/go-cmp/cmp"

	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"
	"google.golang.org/grpc/xds/internal/xdsclient/xdslbregistry"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type customLBConfig struct {
	serviceconfig.LoadBalancingConfig
}

// We have these tests in a separate test package in order to not take a
// dependency on the internal xDS balancer packages within the xDS Client.
func (s) TestConvertToServiceConfigSuccess(t *testing.T) {
	const customLBPolicyName = "myorg.MyCustomLeastRequestPolicy"
	stub.Register(customLBPolicyName, stub.BalancerFuncs{
		ParseConfig: func(json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return customLBConfig{}, nil
		},
	})

	tests := []struct {
		name       string
		policy     *v3clusterpb.LoadBalancingPolicy
		wantConfig *internalserviceconfig.BalancerConfig
		rhDisabled bool
	}{
		{
			name: "ring_hash",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_XX_HASH,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: "ring_hash_experimental",
				Config: &ringhash.LBConfig{
					MinRingSize: 10,
					MaxRingSize: 100,
				},
			},
		},
		{
			name: "round_robin",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: "round_robin",
			},
		},
		{
			name: "round_robin_ring_hash_use_first_supported",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3roundrobinpb.RoundRobin{}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_XX_HASH,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: "round_robin",
			},
		},
		{
			name: "ring_hash_disabled_rh_rr_use_first_supported",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3ringhashpb.RingHash{
								HashFunction:    v3ringhashpb.RingHash_XX_HASH,
								MinimumRingSize: wrapperspb.UInt64(10),
								MaximumRingSize: wrapperspb.UInt64(100),
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: "round_robin",
			},
			rhDisabled: true,
		},
		{
			name: "custom_lb_type_v3_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// The type not registered in gRPC Policy registry.
							// Should fallback to next policy in list.
							TypedConfig: testutils.MarshalAny(&v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
								Value:   &structpb.Struct{},
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name:   "myorg.MyCustomLeastRequestPolicy",
				Config: customLBConfig{},
			},
		},
		{
			name: "custom_lb_type_v1_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: testutils.MarshalAny(&v1xdsudpatypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name:   "myorg.MyCustomLeastRequestPolicy",
				Config: customLBConfig{},
			},
		},
		{
			name: "wrr_locality_child_round_robin",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(&v3roundrobinpb.RoundRobin{}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "wrr_locality_child_custom_lb_type_v3_struct",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(&v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.MyCustomLeastRequestPolicy",
								Value:   &structpb.Struct{},
							}),
						},
					},
				},
			},
			wantConfig: &internalserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &internalserviceconfig.BalancerConfig{
						Name:   "myorg.MyCustomLeastRequestPolicy",
						Config: customLBConfig{},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.rhDisabled {
				oldRingHashSupport := envconfig.XDSRingHash
				envconfig.XDSRingHash = false
				defer func() {
					envconfig.XDSRingHash = oldRingHashSupport
				}()
			}
			rawJSON, err := xdslbregistry.ConvertToServiceConfig(test.policy)
			if err != nil {
				t.Fatalf("ConvertToServiceConfig(%s) failed: %v", pretty.ToJSON(test.policy), err)
			}
			bc := &internalserviceconfig.BalancerConfig{}
			// The converter registry is not guaranteed to emit json that is
			// valid. It's scope is to simply convert from a proto message to
			// internal gRPC JSON format. Thus, the tests cause valid JSON to
			// eventually be emitted from ConvertToServiceConfig(), but this
			// leaves this test brittle over time in case balancer validations
			// change over time and add more failure cases. The simplicity of
			// using this type (to get rid of non determinism in JSON strings)
			// outweighs this brittleness, and also there are plans on
			// decoupling the unmarshalling and validation step both present in
			// this function in the future. In the future if balancer
			// validations change, any configurations in this test that become
			// invalid will need to be fixed. (need to make sure emissions above
			// are valid configuration). Also, once this Unmarshal call is
			// partitioned into Unmarshal vs. Validation in separate operations,
			// the brittleness of this test will go away.
			if err := json.Unmarshal(rawJSON, bc); err != nil {
				t.Fatalf("failed to unmarshal JSON: %v", err)
			}
			if diff := cmp.Diff(bc, test.wantConfig); diff != "" {
				t.Fatalf("ConvertToServiceConfig() got unexpected output, diff (-got +want): %v", diff)
			}
		})
	}
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
							TypedConfig: testutils.MarshalAny(&v3ringhashpb.RingHash{
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
							TypedConfig: testutils.MarshalAny(&v3xdsxdstypepb.TypedStruct{
								TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
								Value:   &structpb.Struct{},
							}),
						},
					},
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							// Not supported by gRPC-Go.
							TypedConfig: testutils.MarshalAny(&v3leastrequestpb.LeastRequest{}),
						},
					},
				},
			},
			wantErr: "no supported policy found in policy list",
		},
		// TODO: test validity right on the boundary of recursion 16 layers
		// total.
		{
			name: "too much recursion",
			policy: &v3clusterpb.LoadBalancingPolicy{
				Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
					{
						TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
							TypedConfig: wrrLocalityAny(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(wrrLocality(&v3roundrobinpb.RoundRobin{}))))))))))))))))))))))),
						},
					},
				},
			},
			wantErr: "exceeds max depth",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, gotErr := xdslbregistry.ConvertToServiceConfig(test.policy)
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

// wrrLocality is a helper that takes a proto message and returns a
// WrrLocalityProto with the proto message marshaled into a proto.Any as a
// child.
func wrrLocality(m proto.Message) *v3wrrlocalitypb.WrrLocality {
	return &v3wrrlocalitypb.WrrLocality{
		EndpointPickingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: testutils.MarshalAny(m),
					},
				},
			},
		},
	}
}

// wrrLocalityAny takes a proto message and returns a wrr locality proto
// marshaled as an any with an any child set to the marshaled proto message.
func wrrLocalityAny(m proto.Message) *anypb.Any {
	return testutils.MarshalAny(wrrLocality(m))
}
