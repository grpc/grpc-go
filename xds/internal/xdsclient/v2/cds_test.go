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

package v2

import (
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const (
	serviceName1 = "foo-service"
	serviceName2 = "bar-service"
)

var (
	badlyMarshaledCDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2ClusterURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: version.V2ClusterURL,
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
	marshaledCluster1 = testutils.MarshalAny(goodCluster1)
	goodCluster2      = &xdspb.Cluster{
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
	marshaledCluster2 = testutils.MarshalAny(goodCluster2)
	goodCDSResponse1  = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledCluster1,
		},
		TypeUrl: version.V2ClusterURL,
	}
	goodCDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledCluster2,
		},
		TypeUrl: version.V2ClusterURL,
	}
)

// TestCDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers a CDS watcher and tests
// different CDS responses.
func (s) TestCDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		cdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    map[string]xdsclient.ClusterUpdateErrTuple
		wantUpdateMD  xdsclient.UpdateMetadata
		wantUpdateErr bool
	}{
		// Badly marshaled CDS response.
		{
			name:        "badly-marshaled-response",
			cdsResponse: badlyMarshaledCDSResponse,
			wantErr:     true,
			wantUpdate:  nil,
			wantUpdateMD: xdsclient.UpdateMetadata{
				Status: xdsclient.ServiceStatusNACKed,
				ErrState: &xdsclient.UpdateErrorMetadata{
					Err: cmpopts.AnyError,
				},
			},
			wantUpdateErr: false,
		},
		// Response does not contain Cluster proto.
		{
			name:        "no-cluster-proto-in-response",
			cdsResponse: badResourceTypeInLDSResponse,
			wantErr:     true,
			wantUpdate:  nil,
			wantUpdateMD: xdsclient.UpdateMetadata{
				Status: xdsclient.ServiceStatusNACKed,
				ErrState: &xdsclient.UpdateErrorMetadata{
					Err: cmpopts.AnyError,
				},
			},
			wantUpdateErr: false,
		},
		// Response contains no clusters.
		{
			name:        "no-cluster",
			cdsResponse: &xdspb.DiscoveryResponse{},
			wantErr:     false,
			wantUpdate:  nil,
			wantUpdateMD: xdsclient.UpdateMetadata{
				Status: xdsclient.ServiceStatusACKed,
			},
			wantUpdateErr: false,
		},
		// Response contains one good cluster we are not interested in.
		{
			name:        "one-uninteresting-cluster",
			cdsResponse: goodCDSResponse2,
			wantErr:     false,
			wantUpdate: map[string]xdsclient.ClusterUpdateErrTuple{
				goodClusterName2: {Update: xdsclient.ClusterUpdate{ClusterName: goodClusterName2, EDSServiceName: serviceName2, Raw: marshaledCluster2}},
			},
			wantUpdateMD: xdsclient.UpdateMetadata{
				Status: xdsclient.ServiceStatusACKed,
			},
			wantUpdateErr: false,
		},
		// Response contains one cluster and it is good.
		{
			name:        "one-good-cluster",
			cdsResponse: goodCDSResponse1,
			wantErr:     false,
			wantUpdate: map[string]xdsclient.ClusterUpdateErrTuple{
				goodClusterName1: {Update: xdsclient.ClusterUpdate{ClusterName: goodClusterName1, EDSServiceName: serviceName1, EnableLRS: true, Raw: marshaledCluster1}},
			},
			wantUpdateMD: xdsclient.UpdateMetadata{
				Status: xdsclient.ServiceStatusACKed,
			},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				rType:        xdsclient.ClusterResource,
				resourceName: goodClusterName1,

				responseToHandle: test.cdsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateMD:     test.wantUpdateMD,
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

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(xdsclient.ResourceType, map[string]interface{}, xdsclient.UpdateMetadata) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if v2c.handleCDSResponse(badResourceTypeInLDSResponse) == nil {
		t.Fatal("v2c.handleCDSResponse() succeeded, should have failed")
	}

	if v2c.handleCDSResponse(goodCDSResponse1) != nil {
		t.Fatal("v2c.handleCDSResponse() succeeded, should have failed")
	}
}
