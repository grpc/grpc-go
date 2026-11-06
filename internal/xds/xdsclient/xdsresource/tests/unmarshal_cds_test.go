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

// Package tests_test contains test cases for unmarshalling of CDS resources.
package tests_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/balancer/autosharding"
	"google.golang.org/grpc/balancer/leastrequest"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	iringhash "google.golang.org/grpc/internal/ringhash"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/xdsclient/xdslbregistry"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3aggregateclusterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/aggregate/v3"
	v3autoshardingpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/autosharding/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3roundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"

	_ "google.golang.org/grpc/balancer/roundrobin" // To register round_robin load balancer.
	_ "google.golang.org/grpc/xds"                 // Register the xDS LB Registry Converters.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	clusterName = "clusterName"
	serviceName = "service"
)

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

func wrrLocalityAny(t *testing.T, m proto.Message) *anypb.Any {
	return testutils.MarshalAny(t, wrrLocality(t, m))
}

func toAnyProto(t *testing.T, m proto.Message) *xdsclient.AnyProto {
	a := testutils.MarshalAny(t, m)
	return &xdsclient.AnyProto{
		TypeURL: a.GetTypeUrl(),
		Value:   a.GetValue(),
	}
}

type customLBConfig struct {
	serviceconfig.LoadBalancingConfig
}

// We have this test in a separate test package in order to not take a
// dependency on the internal xDS balancer packages within the xDS Client.
func (s) TestValidateCluster_Success(t *testing.T) {
	const customLBPolicyName = "myorg.MyCustomLeastRequestPolicy"
	stub.Register(customLBPolicyName, stub.BalancerFuncs{
		ParseConfig: func(json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return customLBConfig{}, nil
		},
	})
	serverCfg, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: "test-server"})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}

	tests := []struct {
		name         string
		cluster      *v3clusterpb.Cluster
		serverCfg    *bootstrap.ServerConfig
		wantUpdate   xdsresource.ClusterUpdate
		wantLBConfig *iserviceconfig.BalancerConfig
	}{
		{
			name: "happy-case-logical-dns",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_LOGICAL_DNS},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
				LoadAssignment: &v3endpointpb.ClusterLoadAssignment{
					Endpoints: []*v3endpointpb.LocalityLbEndpoints{{
						LbEndpoints: []*v3endpointpb.LbEndpoint{{
							HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
								Endpoint: &v3endpointpb.Endpoint{
									Address: &v3corepb.Address{
										Address: &v3corepb.Address_SocketAddress{
											SocketAddress: &v3corepb.SocketAddress{
												Address: "dns_host",
												PortSpecifier: &v3corepb.SocketAddress_PortValue{
													PortValue: 8080,
												},
											},
										},
									},
								},
							},
						}},
					}},
				},
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				ClusterType:     xdsresource.ClusterTypeLogicalDNS,
				DNSHostName:     "dns_host:8080",
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "happy-case-aggregate-v3",
			cluster: &v3clusterpb.Cluster{
				Name: clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_ClusterType{
					ClusterType: &v3clusterpb.Cluster_CustomClusterType{
						Name: "envoy.clusters.aggregate",
						TypedConfig: testutils.MarshalAny(t, &v3aggregateclusterpb.ClusterConfig{
							Clusters: []string{"a", "b", "c"},
						}),
					},
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:             clusterName,
				ClusterType:             xdsresource.ClusterTypeAggregate,
				PrioritizedClusterNames: []string{"a", "b", "c"},
				TelemetryLabels:         xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name:    "happy-case-no-service-name-no-lrs",
			cluster: e2e.DefaultCluster(clusterName, "", e2e.SecurityLevelNone),
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name:    "happy-case-no-lrs",
			cluster: e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone),
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "happiest-case-with-lrs",
			cluster: e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
				ClusterName: clusterName,
				ServiceName: serviceName,
				EnableLRS:   true,
			}),
			serverCfg: serverCfg,
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				LRSServerConfig: serverCfg,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "happiest-case-with-circuitbreakers",
			cluster: func() *v3clusterpb.Cluster {
				c := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
					ClusterName: clusterName,
					ServiceName: serviceName,
					EnableLRS:   true,
				})
				c.CircuitBreakers = &v3clusterpb.CircuitBreakers{
					Thresholds: []*v3clusterpb.CircuitBreakers_Thresholds{
						{
							Priority:    v3corepb.RoutingPriority_DEFAULT,
							MaxRequests: wrapperspb.UInt32(512),
						},
						{
							Priority:    v3corepb.RoutingPriority_HIGH,
							MaxRequests: nil,
						},
					},
				}
				return c
			}(),
			serverCfg: serverCfg,
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				LRSServerConfig: serverCfg,
				MaxRequests:     func() *uint32 { i := uint32(512); return &i }(),
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "happiest-case-with-ring-hash-lb-policy-with-default-config",
			cluster: func() *v3clusterpb.Cluster {
				c := e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)
				c.LbPolicy = v3clusterpb.Cluster_RING_HASH
				return c
			}(),
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "ring_hash_experimental",
				Config: &iringhash.LBConfig{
					MinRingSize: 1024,
					MaxRingSize: 4096,
				},
			},
		},
		{
			name: "happiest-case-with-least-request-lb-policy-with-default-config",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "least_request_experimental",
				Config: &leastrequest.LBConfig{
					ChoiceCount: 2,
				},
			},
		},
		{
			name: "happiest-case-with-ring-hash-lb-policy-with-none-default-config",
			cluster: func() *v3clusterpb.Cluster {
				c := e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)
				c.LbPolicy = v3clusterpb.Cluster_RING_HASH
				c.LbConfig = &v3clusterpb.Cluster_RingHashLbConfig_{
					RingHashLbConfig: &v3clusterpb.Cluster_RingHashLbConfig{
						MinimumRingSize: wrapperspb.UInt64(10),
						MaximumRingSize: wrapperspb.UInt64(100),
					},
				}
				return c
			}(),
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "ring_hash_experimental",
				Config: &iringhash.LBConfig{
					MinRingSize: 10,
					MaxRingSize: 100,
				},
			},
		},
		{
			name: "happiest-case-with-least-request-lb-policy-with-none-default-config",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
				LbConfig: &v3clusterpb.Cluster_LeastRequestLbConfig_{
					LeastRequestLbConfig: &v3clusterpb.Cluster_LeastRequestLbConfig{
						ChoiceCount: wrapperspb.UInt32(3),
					},
				},
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "least_request_experimental",
				Config: &leastrequest.LBConfig{
					ChoiceCount: 3,
				},
			},
		},
		{
			name: "happiest-case-with-ring-hash-lb-policy-configured-through-LoadBalancingPolicy",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
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
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "ring_hash_experimental",
				Config: &iringhash.LBConfig{
					MinRingSize: 10,
					MaxRingSize: 100,
				},
			},
		},
		{
			name: "happiest-case-with-wrrlocality-rr-child-configured-through-LoadBalancingPolicy",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
					Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
						{
							TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
								TypedConfig: wrrLocalityAny(t, &v3roundrobinpb.RoundRobin{}),
							},
						},
					},
				},
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name: "round_robin",
					},
				},
			},
		},
		{
			name: "happiest-case-with-custom-lb-configured-through-LoadBalancingPolicy",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
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
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: wrrlocality.Name,
				Config: &wrrlocality.LBConfig{
					ChildPolicy: &iserviceconfig.BalancerConfig{
						Name:   "myorg.MyCustomLeastRequestPolicy",
						Config: customLBConfig{},
					},
				},
			},
		},
		{
			name: "load-balancing-policy-takes-precedence-over-lb-policy-and-enum",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_RING_HASH,
				LbConfig: &v3clusterpb.Cluster_RingHashLbConfig_{
					RingHashLbConfig: &v3clusterpb.Cluster_RingHashLbConfig{
						MinimumRingSize: wrapperspb.UInt64(20),
						MaximumRingSize: wrapperspb.UInt64(200),
					},
				},
				LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
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
			},
			wantUpdate: xdsresource.ClusterUpdate{
				ClusterName:     clusterName,
				EDSServiceName:  serviceName,
				TelemetryLabels: xdsinternal.UnknownCSMLabels,
			},
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name: "ring_hash_experimental",
				Config: &iringhash.LBConfig{
					MinRingSize: 10,
					MaxRingSize: 100,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := xdsresource.ValidateClusterAndConstructClusterUpdateForTesting(test.cluster, nil, test.serverCfg)
			if err != nil {
				t.Errorf("validateClusterAndConstructClusterUpdate(%+v) failed: %v", test.cluster, err)
			}
			// Ignore the raw JSON string into the cluster update. JSON bytes
			// are nondeterministic (whitespace etc.) so we cannot reliably
			// compare JSON bytes in a test. Thus, marshal into a Balancer
			// Config struct and compare on that. Only need to test this JSON
			// emission here, as this covers the possible output space.
			if diff := cmp.Diff(update, test.wantUpdate, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(xdsresource.ClusterUpdate{}, "LBPolicy")); diff != "" {
				t.Errorf("validateClusterAndConstructClusterUpdate(%+v) got diff: %v (-got, +want)", test.cluster, diff)
			}
			bc := &iserviceconfig.BalancerConfig{}
			if err := json.Unmarshal(update.LBPolicy, bc); err != nil {
				t.Fatalf("failed to unmarshal JSON: %v", err)
			}
			if diff := cmp.Diff(bc, test.wantLBConfig); diff != "" {
				t.Fatalf("update.LBConfig got unexpected output, diff (-got +want): %v", diff)
			}
		})
	}
}

// Tests that the cluster resource decoder passes the bootstrap and server
// configurations to the xDS LB policy registry when converting the LB policy
// configuration of a cluster, and that the GrpcServices returned by the
// registry are stored in the cluster update.
func (s) TestValidateCluster_LBPolicyInfo(t *testing.T) {
	bootstrapCfg := &bootstrap.Config{}
	serverCfg := &bootstrap.ServerConfig{}
	opts := xdslbregistry.ConverterOptions{BootstrapConfig: bootstrapCfg, ServerConfig: serverCfg}

	// Register a converter for the StringValue proto type, which pretends that
	// the LB policy configuration being converted contains a GrpcService proto
	// whose target URI is the value of the StringValue.
	const typeURL = "type.googleapis.com/google.protobuf.StringValue"
	xdslbregistry.Register(typeURL, func(rawProto []byte, _ int, gotOpts xdslbregistry.ConverterOptions) (*xdslbregistry.ConverterResult, error) {
		if gotOpts != opts {
			return nil, fmt.Errorf("converter invoked with options %+v, want bootstrap config %p and server config %p", gotOpts, bootstrapCfg, serverCfg)
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

	cluster := &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: serviceName,
		},
		LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: wrrLocalityAny(t, wrapperspb.String("target")),
					},
				},
			},
		},
	}

	xdsServerCfg := xdsclient.ServerConfig{}
	decoder := xdsresource.NewClusterResourceTypeDecoder(bootstrapCfg, map[xdsclient.ServerConfig]*bootstrap.ServerConfig{
		xdsServerCfg: serverCfg,
	})
	res, err := decoder.Decode(toAnyProto(t, cluster), xdsclient.DecodeOptions{ServerConfig: &xdsServerCfg})
	if err != nil {
		t.Fatalf("decoder.Decode(%+v) failed: %v", cluster, err)
	}
	update := res.Resource.(*xdsresource.ClusterResourceData).Resource
	wantInfo := xdsresource.LBPolicyInfo{
		GRPCServices: map[string]*grpcservice.Config{"target": {TargetURI: "target"}},
	}
	if diff := cmp.Diff(update.LBPolicyInfo, wantInfo); diff != "" {
		t.Errorf("decoder.Decode(%+v) returned unexpected LBPolicyInfo, diff (-got +want):\n%s", cluster, diff)
	}
}

// newAutoshardingClusterDecoder returns a cluster resource decoder, and the
// options to pass to it when decoding resources. The decoder's bootstrap
// configuration contains allowedTarget in allowed_grpc_services, with insecure
// channel credentials. Resources are decoded as if received from an untrusted
// management server.
func newAutoshardingClusterDecoder(t *testing.T, allowedTarget string) (xdsclient.Decoder, xdsclient.DecodeOptions) {
	t.Helper()
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             json.RawMessage(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:                json.RawMessage(`{"id": "test-node"}`),
		AllowedGRPCServices: json.RawMessage(fmt.Sprintf(`{%q: {"channel_creds": [{"type": "insecure"}]}}`, allowedTarget)),
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	bootstrapCfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}
	serverCfg, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: "xds-server:1234"})
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	xdsServerCfg := xdsclient.ServerConfig{}
	decoder := xdsresource.NewClusterResourceTypeDecoder(bootstrapCfg, map[xdsclient.ServerConfig]*bootstrap.ServerConfig{
		xdsServerCfg: serverCfg,
	})
	return decoder, xdsclient.DecodeOptions{ServerConfig: &xdsServerCfg}
}

// edsClusterWithLBPolicy returns an EDS cluster resource whose
// load_balancing_policy field contains the given LB policy configuration.
func edsClusterWithLBPolicy(lbPolicy *anypb.Any) *v3clusterpb.Cluster {
	return &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: serviceName,
		},
		LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: lbPolicy,
					},
				},
			},
		},
	}
}

// googleGrpcService returns a GrpcService proto configured with the GoogleGrpc
// target specifier set to targetURI.
func googleGrpcService(targetURI string) *v3corepb.GrpcService {
	return &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{TargetUri: targetURI},
		},
	}
}

// Tests that a cluster resource configured with the autosharding LB policy is
// decoded successfully when the autosharding LB policy is enabled, both when
// the policy is used for locality and endpoint picking, and when it is used
// only for endpoint picking. Verifies that the LB policy configuration
// generated by the xDS LB policy registry is accepted by the autosharding LB
// policy, and that the GrpcService parsed from the LB policy configuration is
// stored in the cluster update, keyed by the channel factory key.
func (s) TestValidateCluster_Autosharding(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	decoder, decodeOpts := newAutoshardingClusterDecoder(t, shardingServiceURI)
	asProto := &v3autoshardingpb.Autosharding{
		GrpcService:        googleGrpcService(shardingServiceURI),
		AutoshardingTarget: "my-target-%s",
		KeyHeaderName:      "my-key-header",
	}

	// Since the management server is untrusted, the channel credentials come
	// from the allowed_grpc_services entry in the bootstrap configuration.
	wantGS := &grpcservice.Config{
		TargetURI:          shardingServiceURI,
		ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil),
	}
	wantKey := wantGS.Key()
	wantInfo := xdsresource.LBPolicyInfo{
		GRPCServices: map[string]*grpcservice.Config{wantKey: wantGS},
	}
	// The initial assignment timeout is not set in the xDS configuration, and
	// therefore the default value is set by the autosharding LB policy when
	// parsing its configuration.
	wantAutoshardingLBConfig := &iserviceconfig.BalancerConfig{
		Name: autosharding.Name,
		Config: &autosharding.LBConfig{
			ChannelFactoryKey:        wantKey,
			AutoShardingTarget:       "my-target-%s",
			KeyHeaderName:            "my-key-header",
			InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
		},
	}

	tests := []struct {
		name         string
		lbPolicy     *anypb.Any
		wantLBConfig *iserviceconfig.BalancerConfig
	}{
		{
			name:         "locality_and_endpoint_picking",
			lbPolicy:     testutils.MarshalAny(t, asProto),
			wantLBConfig: wantAutoshardingLBConfig,
		},
		{
			name:     "endpoint_picking_only",
			lbPolicy: wrrLocalityAny(t, asProto),
			wantLBConfig: &iserviceconfig.BalancerConfig{
				Name:   wrrlocality.Name,
				Config: &wrrlocality.LBConfig{ChildPolicy: wantAutoshardingLBConfig},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cluster := edsClusterWithLBPolicy(test.lbPolicy)
			res, err := decoder.Decode(toAnyProto(t, cluster), decodeOpts)
			if err != nil {
				t.Fatalf("decoder.Decode(%+v) failed: %v", cluster, err)
			}
			update := res.Resource.(*xdsresource.ClusterResourceData).Resource
			gotLBConfig := &iserviceconfig.BalancerConfig{}
			if err := json.Unmarshal(update.LBPolicy, gotLBConfig); err != nil {
				t.Fatalf("Failed to unmarshal LB policy configuration %s: %v", update.LBPolicy, err)
			}
			if diff := cmp.Diff(gotLBConfig, test.wantLBConfig); diff != "" {
				t.Errorf("decoder.Decode(%+v) returned unexpected LB policy configuration, diff (-got +want):\n%s", cluster, diff)
			}
			if diff := cmp.Diff(update.LBPolicyInfo, wantInfo); diff != "" {
				t.Errorf("decoder.Decode(%+v) returned unexpected LBPolicyInfo, diff (-got +want):\n%s", cluster, diff)
			}
		})
	}
}

// Tests that a cluster resource configured with the autosharding LB policy is
// NACKed when the LB policy configuration generated by the xDS LB policy
// registry is rejected by the autosharding LB policy.
func (s) TestValidateCluster_Autosharding_InvalidLBConfig(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSAutoshardingEnabled, true)

	const shardingServiceURI = "dns:///sharding-service:443"
	decoder, decodeOpts := newAutoshardingClusterDecoder(t, shardingServiceURI)
	// The autosharding LB policy rejects configurations that do not specify
	// the name of the header containing the sharding key.
	cluster := edsClusterWithLBPolicy(testutils.MarshalAny(t, &v3autoshardingpb.Autosharding{
		GrpcService:        googleGrpcService(shardingServiceURI),
		AutoshardingTarget: "my-target",
	}))
	_, err := decoder.Decode(toAnyProto(t, cluster), decodeOpts)
	const wantErr = "keyHeaderName field is required"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("decoder.Decode(%+v) returned error: %v, want error containing %q", cluster, err, wantErr)
	}
}
