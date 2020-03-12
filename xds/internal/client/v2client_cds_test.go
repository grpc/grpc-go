/*
 *
 * Copyright 2019 gRPC authors.
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
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
)

const (
	serviceName1 = "foo-service"
	serviceName2 = "bar-service"
)

func (s) TestValidateCluster(t *testing.T) {
	emptyUpdate := ClusterUpdate{ServiceName: "", EnableLRS: false}
	tests := []struct {
		name       string
		cluster    *xdspb.Cluster
		wantUpdate ClusterUpdate
		wantErr    bool
	}{
		{
			name: "non-eds-cluster-type",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_STATIC},
				EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
					EdsConfig: &corepb.ConfigSource{
						ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
							Ads: &corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: xdspb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-eds-config",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
				LbPolicy:             xdspb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-ads-config-source",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
				EdsClusterConfig:     &xdspb.Cluster_EdsClusterConfig{},
				LbPolicy:             xdspb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "non-round-robin-lb-policy",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
				EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
					EdsConfig: &corepb.ConfigSource{
						ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
							Ads: &corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: xdspb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "happy-case-no-service-name-no-lrs",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
				EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
					EdsConfig: &corepb.ConfigSource{
						ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
							Ads: &corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: xdspb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
		},
		{
			name: "happy-case-no-lrs",
			cluster: &xdspb.Cluster{
				ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
				EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
					EdsConfig: &corepb.ConfigSource{
						ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
							Ads: &corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName1,
				},
				LbPolicy: xdspb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName1, EnableLRS: false},
		},
		{
			name:       "happiest-case",
			cluster:    goodCluster1,
			wantUpdate: ClusterUpdate{ServiceName: serviceName1, EnableLRS: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, gotErr := validateCluster(test.cluster)
			if (gotErr != nil) != test.wantErr {
				t.Errorf("validateCluster(%+v) returned error: %v, wantErr: %v", test.cluster, gotErr, test.wantErr)
			}
			if !cmp.Equal(gotUpdate, test.wantUpdate) {
				t.Errorf("validateCluster(%+v) = %v, want: %v", test.cluster, gotUpdate, test.wantUpdate)
			}
		})
	}
}

// TestCDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers a CDS watcher and tests
// different CDS responses.
func (s) TestCDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		cdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *ClusterUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled CDS response.
		{
			name:          "badly-marshaled-response",
			cdsResponse:   badlyMarshaledCDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain Cluster proto.
		{
			name:          "no-cluster-proto-in-response",
			cdsResponse:   badResourceTypeInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains no clusters.
		{
			name:          "no-cluster",
			cdsResponse:   &xdspb.DiscoveryResponse{},
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good cluster we are not interested in.
		{
			name:          "one-uninteresting-cluster",
			cdsResponse:   goodCDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one cluster and it is good.
		{
			name:          "one-good-cluster",
			cdsResponse:   goodCDSResponse1,
			wantErr:       false,
			wantUpdate:    &ClusterUpdate{ServiceName: serviceName1, EnableLRS: true},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:      cdsURL,
				resourceName: goodClusterName1,

				responseToHandle: test.cdsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestCDSHandleResponseWithoutWatch tests the case where the v2Client receives
// a CDS response without a registered watcher.
func (s) TestCDSHandleResponseWithoutWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	if v2c.handleCDSResponse(badResourceTypeInLDSResponse) == nil {
		t.Fatal("v2c.handleCDSResponse() succeeded, should have failed")
	}

	if v2c.handleCDSResponse(goodCDSResponse1) != nil {
		t.Fatal("v2c.handleCDSResponse() succeeded, should have failed")
	}
}

var (
	badlyMarshaledCDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: cdsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: cdsURL,
	}
	goodCluster1 = &xdspb.Cluster{
		Name:                 goodClusterName1,
		ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
		EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
			EdsConfig: &corepb.ConfigSource{
				ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
					Ads: &corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: serviceName1,
		},
		LbPolicy: xdspb.Cluster_ROUND_ROBIN,
		LrsServer: &corepb.ConfigSource{
			ConfigSourceSpecifier: &corepb.ConfigSource_Self{
				Self: &corepb.SelfConfigSource{},
			},
		},
	}
	marshaledCluster1, _ = proto.Marshal(goodCluster1)
	goodCluster2         = &xdspb.Cluster{
		Name:                 goodClusterName2,
		ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
		EdsClusterConfig: &xdspb.Cluster_EdsClusterConfig{
			EdsConfig: &corepb.ConfigSource{
				ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
					Ads: &corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: serviceName2,
		},
		LbPolicy: xdspb.Cluster_ROUND_ROBIN,
	}
	marshaledCluster2, _ = proto.Marshal(goodCluster2)
	goodCDSResponse1     = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: cdsURL,
				Value:   marshaledCluster1,
			},
		},
		TypeUrl: cdsURL,
	}
	goodCDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: cdsURL,
				Value:   marshaledCluster2,
			},
		},
		TypeUrl: cdsURL,
	}
)
