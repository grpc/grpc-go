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

package client

import (
	"fmt"
	"net"
	"strconv"
	"testing"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v2httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	v2listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/version"
)

func (s) TestValidateCluster(t *testing.T) {
	const (
		clusterName = "clusterName"
		serviceName = "service"
	)
	var (
		emptyUpdate = ClusterUpdate{ServiceName: "", EnableLRS: false}
	)

	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
		wantErr    bool
	}{
		{
			name: "non-eds-cluster-type",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-eds-config",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-ads-config-source",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig:     &v3clusterpb.Cluster_EdsClusterConfig{},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "non-round-robin-lb-policy",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "happy-case-no-service-name-no-lrs",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
		},
		{
			name: "happy-case-no-lrs",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName, EnableLRS: false},
		},
		{
			name: "happiest-case",
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
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				LrsServer: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
						Self: &v3corepb.SelfConfigSource{},
					},
				},
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName, EnableLRS: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := validateCluster(test.cluster)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("validateCluster(%+v) = (%v, %v), wantErr: (%v, %v)", test.cluster, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestUnmarshalCluster(t *testing.T) {
	const (
		v2ClusterName = "v2clusterName"
		v3ClusterName = "v3clusterName"
		v2Service     = "v2Service"
		v3Service     = "v2Service"
	)
	var (
		v2Cluster = &v2xdspb.Cluster{
			Name:                 v2ClusterName,
			ClusterDiscoveryType: &v2xdspb.Cluster_Type{Type: v2xdspb.Cluster_EDS},
			EdsClusterConfig: &v2xdspb.Cluster_EdsClusterConfig{
				EdsConfig: &v2corepb.ConfigSource{
					ConfigSourceSpecifier: &v2corepb.ConfigSource_Ads{
						Ads: &v2corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: v2Service,
			},
			LbPolicy: v2xdspb.Cluster_ROUND_ROBIN,
			LrsServer: &v2corepb.ConfigSource{
				ConfigSourceSpecifier: &v2corepb.ConfigSource_Self{
					Self: &v2corepb.SelfConfigSource{},
				},
			},
		}

		v3Cluster = &v3clusterpb.Cluster{
			Name:                 v3ClusterName,
			ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
			EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
				EdsConfig: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
						Ads: &v3corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: v3Service,
			},
			LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			LrsServer: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
					Self: &v3corepb.SelfConfigSource{},
				},
			},
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ClusterUpdate
		wantErr    bool
	}{
		{
			name:      "non-cluster resource type",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantErr:   true,
		},
		{
			name: "badly marshaled cluster resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value:   []byte{1, 2, 3, 4},
				},
			},
			wantErr: true,
		},
		{
			name: "bad cluster resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						cl := &v3clusterpb.Cluster{
							ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
						}
						mcl, _ := proto.Marshal(cl)
						return mcl
					}(),
				},
			},
			wantErr: true,
		},
		{
			name: "v2 cluster",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						mcl, _ := proto.Marshal(v2Cluster)
						return mcl
					}(),
				},
			},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {ServiceName: v2Service, EnableLRS: true},
			},
		},
		{
			name: "v3 cluster",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						mcl, _ := proto.Marshal(v3Cluster)
						return mcl
					}(),
				},
			},
			wantUpdate: map[string]ClusterUpdate{
				v3ClusterName: {ServiceName: v3Service, EnableLRS: true},
			},
		},
		{
			name: "multiple clusters",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						mcl, _ := proto.Marshal(v2Cluster)
						return mcl
					}(),
				},
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						mcl, _ := proto.Marshal(v3Cluster)
						return mcl
					}(),
				},
			},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {ServiceName: v2Service, EnableLRS: true},
				v3ClusterName: {ServiceName: v3Service, EnableLRS: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := UnmarshalCluster(test.resources, nil)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalCluster(%v) = (%+v, %v) want (%+v, %v)", test.resources, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestEDSParseRespProto(t *testing.T) {
	tests := []struct {
		name    string
		m       *v3endpointpb.ClusterLoadAssignment
		want    EndpointsUpdate
		wantErr bool
	}{
		{
			name: "missing-priority",
			m: func() *v3endpointpb.ClusterLoadAssignment {
				clab0 := newClaBuilder("test", nil)
				clab0.addLocality("locality-1", 1, 0, []string{"addr1:314"}, nil)
				clab0.addLocality("locality-2", 1, 2, []string{"addr2:159"}, nil)
				return clab0.Build()
			}(),
			want:    EndpointsUpdate{},
			wantErr: true,
		},
		{
			name: "missing-locality-ID",
			m: func() *v3endpointpb.ClusterLoadAssignment {
				clab0 := newClaBuilder("test", nil)
				clab0.addLocality("", 1, 0, []string{"addr1:314"}, nil)
				return clab0.Build()
			}(),
			want:    EndpointsUpdate{},
			wantErr: true,
		},
		{
			name: "good",
			m: func() *v3endpointpb.ClusterLoadAssignment {
				clab0 := newClaBuilder("test", nil)
				clab0.addLocality("locality-1", 1, 1, []string{"addr1:314"}, &addLocalityOptions{
					Health: []v3corepb.HealthStatus{v3corepb.HealthStatus_UNHEALTHY},
					Weight: []uint32{271},
				})
				clab0.addLocality("locality-2", 1, 0, []string{"addr2:159"}, &addLocalityOptions{
					Health: []v3corepb.HealthStatus{v3corepb.HealthStatus_DRAINING},
					Weight: []uint32{828},
				})
				return clab0.Build()
			}(),
			want: EndpointsUpdate{
				Drops: nil,
				Localities: []Locality{
					{
						Endpoints: []Endpoint{{
							Address:      "addr1:314",
							HealthStatus: EndpointHealthStatusUnhealthy,
							Weight:       271,
						}},
						ID:       internal.LocalityID{SubZone: "locality-1"},
						Priority: 1,
						Weight:   1,
					},
					{
						Endpoints: []Endpoint{{
							Address:      "addr2:159",
							HealthStatus: EndpointHealthStatusDraining,
							Weight:       828,
						}},
						ID:       internal.LocalityID{SubZone: "locality-2"},
						Priority: 0,
						Weight:   1,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEDSRespProto(tt.m)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseEDSRespProto() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if d := cmp.Diff(got, tt.want); d != "" {
				t.Errorf("parseEDSRespProto() got = %v, want %v, diff: %v", got, tt.want, d)
			}
		})
	}
}

func (s) TestUnmarshalEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]EndpointsUpdate
		wantErr    bool
	}{
		{
			name:      "non-clusterLoadAssignment resource type",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantErr:   true,
		},
		{
			name: "badly marshaled clusterLoadAssignment resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3EndpointsURL,
					Value:   []byte{1, 2, 3, 4},
				},
			},
			wantErr: true,
		},
		{
			name: "bad endpoints resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3EndpointsURL,
					Value: func() []byte {
						clab0 := newClaBuilder("test", nil)
						clab0.addLocality("locality-1", 1, 0, []string{"addr1:314"}, nil)
						clab0.addLocality("locality-2", 1, 2, []string{"addr2:159"}, nil)
						e := clab0.Build()
						me, _ := proto.Marshal(e)
						return me
					}(),
				},
			},
			wantErr: true,
		},
		{
			name: "v3 endpoints",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3EndpointsURL,
					Value: func() []byte {
						clab0 := newClaBuilder("test", nil)
						clab0.addLocality("locality-1", 1, 1, []string{"addr1:314"}, &addLocalityOptions{
							Health: []v3corepb.HealthStatus{v3corepb.HealthStatus_UNHEALTHY},
							Weight: []uint32{271},
						})
						clab0.addLocality("locality-2", 1, 0, []string{"addr2:159"}, &addLocalityOptions{
							Health: []v3corepb.HealthStatus{v3corepb.HealthStatus_DRAINING},
							Weight: []uint32{828},
						})
						e := clab0.Build()
						me, _ := proto.Marshal(e)
						return me
					}(),
				},
			},
			wantUpdate: map[string]EndpointsUpdate{
				"test": {
					Drops: nil,
					Localities: []Locality{
						{
							Endpoints: []Endpoint{{
								Address:      "addr1:314",
								HealthStatus: EndpointHealthStatusUnhealthy,
								Weight:       271,
							}},
							ID:       internal.LocalityID{SubZone: "locality-1"},
							Priority: 1,
							Weight:   1,
						},
						{
							Endpoints: []Endpoint{{
								Address:      "addr2:159",
								HealthStatus: EndpointHealthStatusDraining,
								Weight:       828,
							}},
							ID:       internal.LocalityID{SubZone: "locality-2"},
							Priority: 0,
							Weight:   1,
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := UnmarshalEndpoints(test.resources, nil)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalEndpoints(%v) = (%+v, %v) want (%+v, %v)", test.resources, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestGetRouteConfigFromListener(t *testing.T) {
	const (
		goodLDSTarget       = "lds.target.good:1111"
		goodRouteConfigName = "GoodRouteConfig"
	)

	tests := []struct {
		name      string
		lis       *v3listenerpb.Listener
		wantRoute string
		wantErr   bool
	}{
		{
			name:      "no-apiListener-field",
			lis:       &v3listenerpb.Listener{},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "badly-marshaled-apiListener",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V3HTTPConnManagerURL,
						Value:   []byte{1, 2, 3, 4},
					},
				},
			},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "wrong-type-in-apiListener",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V2ListenerURL,
						Value: func() []byte {
							cm := &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
									Rds: &v3httppb.Rds{
										ConfigSource: &v3corepb.ConfigSource{
											ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
										},
										RouteConfigName: goodRouteConfigName}}}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "empty-httpConnMgr-in-apiListener",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V3HTTPConnManagerURL,
						Value: func() []byte {
							cm := &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
									Rds: &v3httppb.Rds{},
								},
							}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "scopedRoutes-routeConfig-in-apiListener",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V3HTTPConnManagerURL,
						Value: func() []byte {
							cm := &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
							}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "rds.ConfigSource-in-apiListener-is-not-ADS",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V3HTTPConnManagerURL,
						Value: func() []byte {
							cm := &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
									Rds: &v3httppb.Rds{
										ConfigSource: &v3corepb.ConfigSource{
											ConfigSourceSpecifier: &v3corepb.ConfigSource_Path{
												Path: "/some/path",
											},
										},
										RouteConfigName: goodRouteConfigName}}}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: "",
			wantErr:   true,
		},
		{
			name: "goodListener",
			lis: &v3listenerpb.Listener{
				Name: goodLDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: &anypb.Any{
						TypeUrl: version.V3HTTPConnManagerURL,
						Value: func() []byte {
							cm := &v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
									Rds: &v3httppb.Rds{
										ConfigSource: &v3corepb.ConfigSource{
											ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
										},
										RouteConfigName: goodRouteConfigName}}}
							mcm, _ := proto.Marshal(cm)
							return mcm
						}()}}},
			wantRoute: goodRouteConfigName,
			wantErr:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotRoute, err := getRouteConfigNameFromListener(test.lis, nil)
			if (err != nil) != test.wantErr || gotRoute != test.wantRoute {
				t.Errorf("getRouteConfigNameFromListener(%+v) = (%s, %v), want (%s, %v)", test.lis, gotRoute, err, test.wantRoute, test.wantErr)
			}
		})
	}
}

func (s) TestUnmarshalListener(t *testing.T) {
	const (
		v2LDSTarget       = "lds.target.good:2222"
		v3LDSTarget       = "lds.target.good:3333"
		v2RouteConfigName = "v2RouteConfig"
		v3RouteConfigName = "v3RouteConfig"
	)

	var (
		v2Lis = &anypb.Any{
			TypeUrl: version.V2ListenerURL,
			Value: func() []byte {
				cm := &v2httppb.HttpConnectionManager{
					RouteSpecifier: &v2httppb.HttpConnectionManager_Rds{
						Rds: &v2httppb.Rds{
							ConfigSource: &v2corepb.ConfigSource{
								ConfigSourceSpecifier: &v2corepb.ConfigSource_Ads{Ads: &v2corepb.AggregatedConfigSource{}},
							},
							RouteConfigName: v2RouteConfigName,
						},
					},
				}
				mcm, _ := proto.Marshal(cm)
				lis := &v2xdspb.Listener{
					Name: v2LDSTarget,
					ApiListener: &v2listenerpb.ApiListener{
						ApiListener: &anypb.Any{
							TypeUrl: version.V2HTTPConnManagerURL,
							Value:   mcm,
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
		v3Lis = &anypb.Any{
			TypeUrl: version.V3ListenerURL,
			Value: func() []byte {
				cm := &v3httppb.HttpConnectionManager{
					RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
						Rds: &v3httppb.Rds{
							ConfigSource: &v3corepb.ConfigSource{
								ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
							},
							RouteConfigName: v3RouteConfigName,
						},
					},
				}
				mcm, _ := proto.Marshal(cm)
				lis := &v3listenerpb.Listener{
					Name: v3LDSTarget,
					ApiListener: &v3listenerpb.ApiListener{
						ApiListener: &anypb.Any{
							TypeUrl: version.V3HTTPConnManagerURL,
							Value:   mcm,
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantErr    bool
	}{
		{
			name:      "non-listener resource",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantErr:   true,
		},
		{
			name: "badly marshaled listener resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V3HTTPConnManagerURL,
									Value:   []byte{1, 2, 3, 4},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantErr: true,
		},
		{
			name: "empty resource list",
		},
		{
			name:      "v2 listener resource",
			resources: []*anypb.Any{v2Lis},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName},
			},
		},
		{
			name:      "v3 listener resource",
			resources: []*anypb.Any{v3Lis},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {RouteConfigName: v3RouteConfigName},
			},
		},
		{
			name:      "multiple listener resources",
			resources: []*anypb.Any{v2Lis, v3Lis},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName},
				v3LDSTarget: {RouteConfigName: v3RouteConfigName},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := UnmarshalListener(test.resources, nil)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalListener(%v) = (%v, %v) want (%v, %v)", test.resources, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestRDSGenerateRDSUpdateFromRouteConfiguration(t *testing.T) {
	const (
		uninterestingDomain      = "uninteresting.domain"
		uninterestingClusterName = "uninterestingClusterName"
		ldsTarget                = "lds.target.good:1111"
		routeName                = "routeName"
		clusterName              = "clusterName"
	)

	tests := []struct {
		name       string
		rc         *v3routepb.RouteConfiguration
		wantUpdate RouteConfigUpdate
		wantError  bool
	}{
		{
			name:      "no-virtual-hosts-in-rc",
			rc:        &v3routepb.RouteConfiguration{},
			wantError: true,
		},
		{
			name: "no-domains-in-rc",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{{}},
			},
			wantError: true,
		},
		{
			name: "non-matching-domain-in-rc",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{Domains: []string{uninterestingDomain}},
				},
			},
			wantError: true,
		},
		{
			name: "no-routes-in-rc",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{Domains: []string{ldsTarget}},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-match-field-is-nil",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
									},
								},
							},
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-match-field-is-non-nil",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Match:  &v3routepb.RouteMatch{},
								Action: &v3routepb.Route_Route{},
							},
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-routeaction-field-is-nil",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes:  []*v3routepb.Route{{}},
					},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-cluster-field-is-empty",
			rc: &v3routepb.RouteConfiguration{
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_ClusterHeader{},
									},
								},
							},
						},
					},
				},
			},
			wantError: true,
		},
		{
			// default route's match sets case-sensitive to false.
			name: "good-route-config-but-with-casesensitive-false",
			rc: &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsTarget},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
							CaseSensitive: &wrapperspb.BoolValue{Value: false},
						},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
							}}}}}}},
			wantError: true,
		},
		{
			name: "good-route-config-with-empty-string-route",
			rc: &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{uninterestingDomain},
						Routes: []*v3routepb.Route{
							{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
									},
								},
							},
						},
					},
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{WeightedCluster: map[string]uint32{clusterName: 1}},
		},
		{
			// default route's match is not empty string, but "/".
			name: "good-route-config-with-slash-string-route",
			rc: &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{WeightedCluster: map[string]uint32{clusterName: 1}},
		},
		{
			// weights not add up to total-weight.
			name: "route-config-with-weighted_clusters_weights_not_add_up",
			rc: &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
											WeightedClusters: &v3routepb.WeightedCluster{
												Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
													{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
													{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
													{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
												},
												TotalWeight: &wrapperspb.UInt32Value{Value: 30},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "good-route-config-with-weighted_clusters",
			rc: &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*v3routepb.Route{
							{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
								Action: &v3routepb.Route_Route{
									Route: &v3routepb.RouteAction{
										ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
											WeightedClusters: &v3routepb.WeightedCluster{
												Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
													{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
													{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
													{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
												},
												TotalWeight: &wrapperspb.UInt32Value{Value: 10},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{WeightedCluster: map[string]uint32{"a": 2, "b": 3, "c": 5}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, gotError := generateRDSUpdateFromRouteConfiguration(test.rc, ldsTarget)
			if (gotError != nil) != test.wantError || !cmp.Equal(gotUpdate, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("generateRDSUpdateFromRouteConfiguration(%+v, %v) = %v, want %v", test.rc, ldsTarget, gotUpdate, test.wantUpdate)
			}
		})
	}
}

func (s) TestUnmarshalRouteConfig(t *testing.T) {
	const (
		ldsTarget                = "lds.target.good:1111"
		uninterestingDomain      = "uninteresting.domain"
		uninterestingClusterName = "uninterestingClusterName"
		v2RouteConfigName        = "v2RouteConfig"
		v3RouteConfigName        = "v3RouteConfig"
		v2ClusterName            = "v2Cluster"
		v3ClusterName            = "v3Cluster"
	)

	var (
		v2VirtualHost = []*v2routepb.VirtualHost{
			{
				Domains: []string{uninterestingDomain},
				Routes: []*v2routepb.Route{
					{
						Match: &v2routepb.RouteMatch{PathSpecifier: &v2routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v2routepb.Route_Route{
							Route: &v2routepb.RouteAction{
								ClusterSpecifier: &v2routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
							},
						},
					},
				},
			},
			{
				Domains: []string{ldsTarget},
				Routes: []*v2routepb.Route{
					{
						Match: &v2routepb.RouteMatch{PathSpecifier: &v2routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v2routepb.Route_Route{
							Route: &v2routepb.RouteAction{
								ClusterSpecifier: &v2routepb.RouteAction_Cluster{Cluster: v2ClusterName},
							},
						},
					},
				},
			},
		}
		v2RouteConfig = &anypb.Any{
			TypeUrl: version.V2RouteConfigURL,
			Value: func() []byte {
				rc := &v2xdspb.RouteConfiguration{
					Name:         v2RouteConfigName,
					VirtualHosts: v2VirtualHost,
				}
				m, _ := proto.Marshal(rc)
				return m
			}(),
		}
		v3VirtualHost = []*v3routepb.VirtualHost{
			{
				Domains: []string{uninterestingDomain},
				Routes: []*v3routepb.Route{
					{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
							},
						},
					},
				},
			},
			{
				Domains: []string{ldsTarget},
				Routes: []*v3routepb.Route{
					{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: v3ClusterName},
							},
						},
					},
				},
			},
		}
		v3RouteConfig = &anypb.Any{
			TypeUrl: version.V2RouteConfigURL,
			Value: func() []byte {
				rc := &v3routepb.RouteConfiguration{
					Name:         v3RouteConfigName,
					VirtualHosts: v3VirtualHost,
				}
				m, _ := proto.Marshal(rc)
				return m
			}(),
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]RouteConfigUpdate
		wantErr    bool
	}{
		{
			name:      "non-routeConfig resource type",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantErr:   true,
		},
		{
			name: "badly marshaled routeconfig resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3RouteConfigURL,
					Value:   []byte{1, 2, 3, 4},
				},
			},
			wantErr: true,
		},
		{
			name: "bad routeConfig resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3RouteConfigURL,
					Value: func() []byte {
						rc := &v3routepb.RouteConfiguration{
							VirtualHosts: []*v3routepb.VirtualHost{
								{Domains: []string{uninterestingDomain}},
							},
						}
						m, _ := proto.Marshal(rc)
						return m
					}(),
				},
			},
			wantErr: true,
		},
		{
			name: "empty resource list",
		},
		{
			name:      "v2 routeConfig resource",
			resources: []*anypb.Any{v2RouteConfig},
			wantUpdate: map[string]RouteConfigUpdate{
				v2RouteConfigName: {WeightedCluster: map[string]uint32{v2ClusterName: 1}},
			},
		},
		{
			name:      "v3 routeConfig resource",
			resources: []*anypb.Any{v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdate{
				v3RouteConfigName: {WeightedCluster: map[string]uint32{v3ClusterName: 1}},
			},
		},
		{
			name:      "multiple routeConfig resources",
			resources: []*anypb.Any{v2RouteConfig, v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdate{
				v3RouteConfigName: {WeightedCluster: map[string]uint32{v3ClusterName: 1}},
				v2RouteConfigName: {WeightedCluster: map[string]uint32{v2ClusterName: 1}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := UnmarshalRouteConfig(test.resources, ldsTarget, nil)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalRouteConfig(%v, %v) = (%v, %v) want (%v, %v)", test.resources, ldsTarget, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestMatchTypeForDomain(t *testing.T) {
	tests := []struct {
		d    string
		want domainMatchType
	}{
		{d: "", want: domainMatchTypeInvalid},
		{d: "*", want: domainMatchTypeUniversal},
		{d: "bar.*", want: domainMatchTypePrefix},
		{d: "*.abc.com", want: domainMatchTypeSuffix},
		{d: "foo.bar.com", want: domainMatchTypeExact},
		{d: "foo.*.com", want: domainMatchTypeInvalid},
	}
	for _, tt := range tests {
		if got := matchTypeForDomain(tt.d); got != tt.want {
			t.Errorf("matchTypeForDomain(%q) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func (s) TestMatch(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		host        string
		wantTyp     domainMatchType
		wantMatched bool
	}{
		{name: "invalid-empty", domain: "", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "invalid", domain: "a.*.b", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "universal", domain: "*", host: "abc.com", wantTyp: domainMatchTypeUniversal, wantMatched: true},
		{name: "prefix-match", domain: "abc.*", host: "abc.123", wantTyp: domainMatchTypePrefix, wantMatched: true},
		{name: "prefix-no-match", domain: "abc.*", host: "abcd.123", wantTyp: domainMatchTypePrefix, wantMatched: false},
		{name: "suffix-match", domain: "*.123", host: "abc.123", wantTyp: domainMatchTypeSuffix, wantMatched: true},
		{name: "suffix-no-match", domain: "*.123", host: "abc.1234", wantTyp: domainMatchTypeSuffix, wantMatched: false},
		{name: "exact-match", domain: "foo.bar", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: true},
		{name: "exact-no-match", domain: "foo.bar.com", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotTyp, gotMatched := match(tt.domain, tt.host); gotTyp != tt.wantTyp || gotMatched != tt.wantMatched {
				t.Errorf("match() = %v, %v, want %v, %v", gotTyp, gotMatched, tt.wantTyp, tt.wantMatched)
			}
		})
	}
}

func (s) TestFindBestMatchingVirtualHost(t *testing.T) {
	var (
		oneExactMatch = &v3routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"foo.bar.com"},
		}
		oneSuffixMatch = &v3routepb.VirtualHost{
			Name:    "one-suffix-match",
			Domains: []string{"*.bar.com"},
		}
		onePrefixMatch = &v3routepb.VirtualHost{
			Name:    "one-prefix-match",
			Domains: []string{"foo.bar.*"},
		}
		oneUniversalMatch = &v3routepb.VirtualHost{
			Name:    "one-universal-match",
			Domains: []string{"*"},
		}
		longExactMatch = &v3routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"v2.foo.bar.com"},
		}
		multipleMatch = &v3routepb.VirtualHost{
			Name:    "multiple-match",
			Domains: []string{"pi.foo.bar.com", "314.*", "*.159"},
		}
		vhs = []*v3routepb.VirtualHost{oneExactMatch, oneSuffixMatch, onePrefixMatch, oneUniversalMatch, longExactMatch, multipleMatch}
	)

	tests := []struct {
		name   string
		host   string
		vHosts []*v3routepb.VirtualHost
		want   *v3routepb.VirtualHost
	}{
		{name: "exact-match", host: "foo.bar.com", vHosts: vhs, want: oneExactMatch},
		{name: "suffix-match", host: "123.bar.com", vHosts: vhs, want: oneSuffixMatch},
		{name: "prefix-match", host: "foo.bar.org", vHosts: vhs, want: onePrefixMatch},
		{name: "universal-match", host: "abc.123", vHosts: vhs, want: oneUniversalMatch},
		{name: "long-exact-match", host: "v2.foo.bar.com", vHosts: vhs, want: longExactMatch},
		// Matches suffix "*.bar.com" and exact "pi.foo.bar.com". Takes exact.
		{name: "multiple-match-exact", host: "pi.foo.bar.com", vHosts: vhs, want: multipleMatch},
		// Matches suffix "*.159" and prefix "foo.bar.*". Takes suffix.
		{name: "multiple-match-suffix", host: "foo.bar.159", vHosts: vhs, want: multipleMatch},
		// Matches suffix "*.bar.com" and prefix "314.*". Takes suffix.
		{name: "multiple-match-prefix", host: "314.bar.com", vHosts: vhs, want: oneSuffixMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findBestMatchingVirtualHost(tt.host, tt.vHosts); !cmp.Equal(got, tt.want, cmp.Comparer(proto.Equal)) {
				t.Errorf("findBestMatchingVirtualHost() = %v, want %v", got, tt.want)
			}
		})
	}
}

func (s) TestWeightedClustersProtoToMap(t *testing.T) {
	tests := []struct {
		name    string
		wc      *v3routepb.WeightedCluster
		want    map[string]uint32
		wantErr bool
	}{
		{
			name: "weight not add up to non default total",
			wc: &v3routepb.WeightedCluster{
				Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
					{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 1}},
					{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 1}},
					{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 1}},
				},
				TotalWeight: &wrapperspb.UInt32Value{Value: 10},
			},
			wantErr: true,
		},
		{
			name: "weight not add up to default total",
			wc: &v3routepb.WeightedCluster{
				Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
					{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
					{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
					{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
				},
				TotalWeight: nil,
			},
			wantErr: true,
		},
		{
			name: "ok non default total weight",
			wc: &v3routepb.WeightedCluster{
				Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
					{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
					{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
					{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
				},
				TotalWeight: &wrapperspb.UInt32Value{Value: 10},
			},
			want: map[string]uint32{"a": 2, "b": 3, "c": 5},
		},
		{
			name: "ok default total weight is 100",
			wc: &v3routepb.WeightedCluster{
				Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
					{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 20}},
					{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 30}},
					{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 50}},
				},
				TotalWeight: nil,
			},
			want: map[string]uint32{"a": 20, "b": 30, "c": 50},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := weightedClustersProtoToMap(tt.wc)
			if (err != nil) != tt.wantErr {
				t.Errorf("weightedClustersProtoToMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf("weightedClustersProtoToMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

// claBuilder builds a ClusterLoadAssignment, aka EDS
// response.
type claBuilder struct {
	v *v3endpointpb.ClusterLoadAssignment
}

// newClaBuilder creates a claBuilder.
func newClaBuilder(clusterName string, dropPercents []uint32) *claBuilder {
	var drops []*v3endpointpb.ClusterLoadAssignment_Policy_DropOverload
	for i, d := range dropPercents {
		drops = append(drops, &v3endpointpb.ClusterLoadAssignment_Policy_DropOverload{
			Category: fmt.Sprintf("test-drop-%d", i),
			DropPercentage: &v3typepb.FractionalPercent{
				Numerator:   d,
				Denominator: v3typepb.FractionalPercent_HUNDRED,
			},
		})
	}

	return &claBuilder{
		v: &v3endpointpb.ClusterLoadAssignment{
			ClusterName: clusterName,
			Policy: &v3endpointpb.ClusterLoadAssignment_Policy{
				DropOverloads: drops,
			},
		},
	}
}

// addLocalityOptions contains options when adding locality to the builder.
type addLocalityOptions struct {
	Health []v3corepb.HealthStatus
	Weight []uint32
}

// addLocality adds a locality to the builder.
func (clab *claBuilder) addLocality(subzone string, weight uint32, priority uint32, addrsWithPort []string, opts *addLocalityOptions) {
	var lbEndPoints []*v3endpointpb.LbEndpoint
	for i, a := range addrsWithPort {
		host, portStr, err := net.SplitHostPort(a)
		if err != nil {
			panic("failed to split " + a)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			panic("failed to atoi " + portStr)
		}

		lbe := &v3endpointpb.LbEndpoint{
			HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
				Endpoint: &v3endpointpb.Endpoint{
					Address: &v3corepb.Address{
						Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Protocol: v3corepb.SocketAddress_TCP,
								Address:  host,
								PortSpecifier: &v3corepb.SocketAddress_PortValue{
									PortValue: uint32(port)}}}}}},
		}
		if opts != nil {
			if i < len(opts.Health) {
				lbe.HealthStatus = opts.Health[i]
			}
			if i < len(opts.Weight) {
				lbe.LoadBalancingWeight = &wrapperspb.UInt32Value{Value: opts.Weight[i]}
			}
		}
		lbEndPoints = append(lbEndPoints, lbe)
	}

	var localityID *v3corepb.Locality
	if subzone != "" {
		localityID = &v3corepb.Locality{
			Region:  "",
			Zone:    "",
			SubZone: subzone,
		}
	}

	clab.v.Endpoints = append(clab.v.Endpoints, &v3endpointpb.LocalityLbEndpoints{
		Locality:            localityID,
		LbEndpoints:         lbEndPoints,
		LoadBalancingWeight: &wrapperspb.UInt32Value{Value: weight},
		Priority:            priority,
	})
}

// Build builds ClusterLoadAssignment.
func (clab *claBuilder) Build() *v3endpointpb.ClusterLoadAssignment {
	return clab.v
}
