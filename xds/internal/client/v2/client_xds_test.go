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

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/version"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	anypb "github.com/golang/protobuf/ptypes/any"
)

const (
	serviceName1 = "foo-service"
	serviceName2 = "bar-service"
)

// TestCDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers a CDS watcher and tests
// different CDS responses.
func (s) TestCDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		cdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.ClusterUpdate
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
			wantUpdate:    &xdsclient.ClusterUpdate{ServiceName: serviceName1, EnableLRS: true},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:      version.V2ClusterURL,
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

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
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
				TypeUrl: version.V2ClusterURL,
				Value:   marshaledCluster1,
			},
		},
		TypeUrl: version.V2ClusterURL,
	}
	goodCDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2ClusterURL,
				Value:   marshaledCluster2,
			},
		},
		TypeUrl: version.V2ClusterURL,
	}
)

var (
	badlyMarshaledEDSResponse = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2EndpointsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: version.V2EndpointsURL,
	}
	badResourceTypeInEDSResponse = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: version.V2EndpointsURL,
	}
	goodEDSResponse1 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := testutils.NewClusterLoadAssignmentBuilder(goodEDSName, nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: version.V2EndpointsURL,
	}
	goodEDSResponse2 = &v2xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			func() *anypb.Any {
				clab0 := testutils.NewClusterLoadAssignmentBuilder("not-goodEDSName", nil)
				clab0.AddLocality("locality-1", 1, 1, []string{"addr1:314"}, nil)
				clab0.AddLocality("locality-2", 1, 0, []string{"addr2:159"}, nil)
				a, _ := ptypes.MarshalAny(clab0.Build())
				return a
			}(),
		},
		TypeUrl: version.V2EndpointsURL,
	}
)

func (s) TestEDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		edsResponse   *v2xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.EndpointsUpdate
		wantUpdateErr bool
	}{
		// Any in resource is badly marshaled.
		{
			name:          "badly-marshaled_response",
			edsResponse:   badlyMarshaledEDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response doesn't contain resource with the right type.
		{
			name:          "no-config-in-response",
			edsResponse:   badResourceTypeInEDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one uninteresting ClusterLoadAssignment.
		{
			name:          "one-uninterestring-assignment",
			edsResponse:   goodEDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good ClusterLoadAssignment.
		{
			name:        "one-good-assignment",
			edsResponse: goodEDSResponse1,
			wantErr:     false,
			wantUpdate: &xdsclient.EndpointsUpdate{
				Localities: []xdsclient.Locality{
					{
						Endpoints: []xdsclient.Endpoint{{Address: "addr1:314"}},
						ID:        internal.LocalityID{SubZone: "locality-1"},
						Priority:  1,
						Weight:    1,
					},
					{
						Endpoints: []xdsclient.Endpoint{{Address: "addr2:159"}},
						ID:        internal.LocalityID{SubZone: "locality-2"},
						Priority:  0,
						Weight:    1,
					},
				},
			},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:          version.V2EndpointsURL,
				resourceName:     goodEDSName,
				responseToHandle: test.edsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestEDSHandleResponseWithoutWatch tests the case where the v2Client
// receives an EDS response without a registered EDS watcher.
func (s) TestEDSHandleResponseWithoutWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if v2c.handleEDSResponse(badResourceTypeInEDSResponse) == nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}

	if v2c.handleEDSResponse(goodEDSResponse1) != nil {
		t.Fatal("v2c.handleEDSResponse() succeeded, should have failed")
	}
}

// TestLDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a client using it. Then, it registers a watchLDS and tests
// different LDS responses.
func (s) TestLDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		ldsResponse   *v2xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.ListenerUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled LDS response.
		{
			name:          "badly-marshaled-response",
			ldsResponse:   badlyMarshaledLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain Listener proto.
		{
			name:          "no-listener-proto-in-response",
			ldsResponse:   badResourceTypeInLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No APIListener in the response. Just one test case here for a bad
		// ApiListener, since the others are covered in
		// TestGetRouteConfigNameFromListener.
		{
			name:          "no-apiListener-in-response",
			ldsResponse:   noAPIListenerLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one listener and it is good.
		{
			name:          "one-good-listener",
			ldsResponse:   goodLDSResponse1,
			wantErr:       false,
			wantUpdate:    &xdsclient.ListenerUpdate{RouteConfigName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains multiple good listeners, including the one we are
		// interested in.
		{
			name:          "multiple-good-listener",
			ldsResponse:   ldsResponseWithMultipleResources,
			wantErr:       false,
			wantUpdate:    &xdsclient.ListenerUpdate{RouteConfigName: goodRouteName1},
			wantUpdateErr: false,
		},
		// Response contains two good listeners (one interesting and one
		// uninteresting), and one badly marshaled listener. This will cause a
		// nack because the uninteresting listener will still be parsed.
		{
			name:          "good-bad-ugly-listeners",
			ldsResponse:   goodBadUglyLDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one listener, but we are not interested in it.
		{
			name:          "one-uninteresting-listener",
			ldsResponse:   goodLDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response constains no resources. This is the case where the server
		// does not know about the target we are interested in.
		{
			name:          "empty-response",
			ldsResponse:   emptyLDSResponse,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:          version.V2ListenerURL,
				resourceName:     goodLDSTarget1,
				responseToHandle: test.ldsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestLDSHandleResponseWithoutWatch tests the case where the client receives
// an LDS response without a registered watcher.
func (s) TestLDSHandleResponseWithoutWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if v2c.handleLDSResponse(badResourceTypeInLDSResponse) == nil {
		t.Fatal("v2c.handleLDSResponse() succeeded, should have failed")
	}

	if v2c.handleLDSResponse(goodLDSResponse1) != nil {
		t.Fatal("v2c.handleLDSResponse() succeeded, should have failed")
	}
}

// doLDS makes a LDS watch, and waits for the response and ack to finish.
//
// This is called by RDS tests to start LDS first, because LDS is a
// pre-requirement for RDS, and RDS handle would fail without an existing LDS
// watch.
func doLDS(t *testing.T, v2c xdsclient.APIClient, fakeServer *fakeserver.Server) {
	v2c.AddWatch(version.V2ListenerURL, goodLDSTarget1)
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout waiting for LDS request: %v", err)
	}
}

// TestRDSHandleResponse starts a fake xDS server, makes a ClientConn to it,
// and creates a v2Client using it. Then, it registers an LDS and RDS watcher
// and tests different RDS responses.
func (s) TestRDSHandleResponse(t *testing.T) {
	tests := []struct {
		name          string
		rdsResponse   *v2xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *xdsclient.RouteConfigUpdate
		wantUpdateErr bool
	}{
		// Badly marshaled RDS response.
		{
			name:          "badly-marshaled-response",
			rdsResponse:   badlyMarshaledRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response does not contain RouteConfiguration proto.
		{
			name:          "no-route-config-in-response",
			rdsResponse:   badResourceTypeInRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// No VirtualHosts in the response. Just one test case here for a bad
		// RouteConfiguration, since the others are covered in
		// TestGetClusterFromRouteConfiguration.
		{
			name:          "no-virtual-hosts-in-response",
			rdsResponse:   noVirtualHostsInRDSResponse,
			wantErr:       true,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good RouteConfiguration, uninteresting though.
		{
			name:          "one-uninteresting-route-config",
			rdsResponse:   goodRDSResponse2,
			wantErr:       false,
			wantUpdate:    nil,
			wantUpdateErr: false,
		},
		// Response contains one good interesting RouteConfiguration.
		{
			name:          "one-good-route-config",
			rdsResponse:   goodRDSResponse1,
			wantErr:       false,
			wantUpdate:    &xdsclient.RouteConfigUpdate{WeightedCluster: map[string]uint32{goodClusterName1: 1}},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:          version.V2RouteConfigURL,
				resourceName:     goodRouteName1,
				responseToHandle: test.rdsResponse,
				wantHandleErr:    test.wantErr,
				wantUpdate:       test.wantUpdate,
				wantUpdateErr:    test.wantUpdateErr,
			})
		})
	}
}

// TestRDSHandleResponseWithoutLDSWatch tests the case where the v2Client
// receives an RDS response without a registered LDS watcher.
func (s) TestRDSHandleResponseWithoutLDSWatch(t *testing.T) {
	_, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	if v2c.handleRDSResponse(goodRDSResponse1) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}

// TestRDSHandleResponseWithoutRDSWatch tests the case where the v2Client
// receives an RDS response without a registered RDS watcher.
func (s) TestRDSHandleResponseWithoutRDSWatch(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	doLDS(t, v2c, fakeServer)

	if v2c.handleRDSResponse(badResourceTypeInRDSResponse) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}

	if v2c.handleRDSResponse(goodRDSResponse1) != nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}
