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
	"testing"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/xds/internal/version"
)

const (
	clusterName = "clusterName"
	serviceName = "service"
)

var emptyUpdate = ClusterUpdate{ServiceName: "", EnableLRS: false}

func (s) TestValidateCluster_Failure(t *testing.T) {
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if update, err := validateCluster(test.cluster); err == nil {
				t.Errorf("validateCluster(%+v) = %v, wanted error", test.cluster, update)
			}
		})
	}
}

func (s) TestValidateCluster_Success(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
	}{
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
			if err != nil {
				t.Errorf("validateCluster(%+v) failed: %v", test.cluster, err)
			}
			if !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("validateCluster(%+v) = %v, want: %v", test.cluster, update, test.wantUpdate)
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
