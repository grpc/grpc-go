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

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/version/common"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testXDSServer   = "xds-server"
	chanRecvTimeout = 100 * time.Millisecond

	testLDSName = "test-lds"
	testRDSName = "test-rds"
	testCDSName = "test-cds"
	testEDSName = "test-eds"
)

const (
	defaultTestTimeout       = 1 * time.Second
	goodLDSTarget1           = "lds.target.good:1111"
	goodLDSTarget2           = "lds.target.good:2222"
	goodRouteName1           = "GoodRouteConfig1"
	goodRouteName2           = "GoodRouteConfig2"
	goodEDSName              = "GoodClusterAssignment1"
	uninterestingRouteName   = "UninterestingRouteName"
	uninterestingDomain      = "uninteresting.domain"
	goodClusterName1         = "GoodClusterName1"
	goodClusterName2         = "GoodClusterName2"
	uninterestingClusterName = "UninterestingClusterName"
	httpConnManagerURL       = "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
)

var (
	goodHTTPConnManager1 = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				ConfigSource: &basepb.ConfigSource{
					ConfigSourceSpecifier: &basepb.ConfigSource_Ads{Ads: &basepb.AggregatedConfigSource{}},
				},
				RouteConfigName: goodRouteName1,
			},
		},
	}
	marshaledConnMgr1, _ = proto.Marshal(goodHTTPConnManager1)
	goodListener1        = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener1, _ = proto.Marshal(goodListener1)
	goodLDSResponse1      = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: common.V2ListenerURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: common.V2ListenerURL,
	}
	emptyRouteConfig             = &xdspb.RouteConfiguration{}
	marshaledEmptyRouteConfig, _ = proto.Marshal(emptyRouteConfig)
	noVirtualHostsInRDSResponse  = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: common.V2RouteConfigURL,
				Value:   marshaledEmptyRouteConfig,
			},
		},
		TypeUrl: common.V2RouteConfigURL,
	}
	goodRouteConfig1 = &xdspb.RouteConfiguration{
		Name: goodRouteName1,
		VirtualHosts: []*routepb.VirtualHost{
			{
				Domains: []string{uninterestingDomain},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
							},
						},
					},
				},
			},
			{
				Domains: []string{goodLDSTarget1},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
							},
						},
					},
				},
			},
		},
	}
	marshaledGoodRouteConfig1, _ = proto.Marshal(goodRouteConfig1)
	goodRDSResponse1             = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: common.V2RouteConfigURL,
				Value:   marshaledGoodRouteConfig1,
			},
		},
		TypeUrl: common.V2RouteConfigURL,
	}
)

func clientOpts(balancerName string) Options {
	return Options{
		Config: bootstrap.Config{
			BalancerName: balancerName,
			Creds:        grpc.WithInsecure(),
			NodeProto:    testutils.EmptyNodeProtoV2,
		},
	}
}

func (s) TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "empty-opts", opts: Options{}, wantErr: true},
		{
			name: "empty-balancer-name",
			opts: Options{
				Config: bootstrap.Config{
					Creds:     grpc.WithInsecure(),
					NodeProto: testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-dial-creds",
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					NodeProto:    testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-node-proto",
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					Creds:        grpc.WithInsecure(),
				},
			},
			wantErr: true,
		},
		{
			name: "node-proto-version-mismatch",
			opts: Options{
				Config: bootstrap.Config{
					BalancerName: testXDSServer,
					Creds:        grpc.WithInsecure(),
					NodeProto:    testutils.EmptyNodeProtoV3,
					TransportAPI: version.TransportV2,
				},
			},
			wantErr: true,
		},
		// TODO(easwars): Add cases for v3 API client.
		{
			name: "happy-case",
			opts: clientOpts(testXDSServer),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := New(test.opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.opts, err, test.wantErr)
			}
			if c != nil {
				c.Close()
			}
		})
	}
}

type testAPIClient struct {
	r common.UpdateHandler

	addWatches    map[string]*testutils.Channel
	removeWatches map[string]*testutils.Channel
}

func overrideNewAPIClient() (<-chan *testAPIClient, func()) {
	origNewAPIClient := newAPIClient
	ch := make(chan *testAPIClient, 1)
	newAPIClient = func(apiVersion version.TransportAPI, cc *grpc.ClientConn, opts version.BuildOptions) (version.APIClient, error) {
		ret := newTestAPIClient(opts.Parent)
		ch <- ret
		return ret, nil
	}
	return ch, func() { newAPIClient = origNewAPIClient }
}

func newTestAPIClient(r common.UpdateHandler) *testAPIClient {
	addWatches := make(map[string]*testutils.Channel)
	addWatches[common.V2ListenerURL] = testutils.NewChannel()
	addWatches[common.V2RouteConfigURL] = testutils.NewChannel()
	addWatches[common.V2ClusterURL] = testutils.NewChannel()
	addWatches[common.V2EndpointsURL] = testutils.NewChannel()
	removeWatches := make(map[string]*testutils.Channel)
	removeWatches[common.V2ListenerURL] = testutils.NewChannel()
	removeWatches[common.V2RouteConfigURL] = testutils.NewChannel()
	removeWatches[common.V2ClusterURL] = testutils.NewChannel()
	removeWatches[common.V2EndpointsURL] = testutils.NewChannel()
	return &testAPIClient{
		r:             r,
		addWatches:    addWatches,
		removeWatches: removeWatches,
	}
}

func (c *testAPIClient) AddWatch(resourceType, resourceName string) {
	c.addWatches[resourceType].Send(resourceName)
}

func (c *testAPIClient) RemoveWatch(resourceType, resourceName string) {
	c.removeWatches[resourceType].Send(resourceName)
}

func (c *testAPIClient) Close() {}

// TestWatchCallAnotherWatch covers the case where watch() is called inline by a
// callback. It makes sure it doesn't cause a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	v2ClientCh, cleanup := overrideNewAPIClient()
	defer cleanup()

	c, err := New(clientOpts(testXDSServer))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer c.Close()

	v2Client := <-v2ClientCh

	clusterUpdateCh := testutils.NewChannel()
	firstTime := true
	c.WatchCluster(testCDSName, func(update common.ClusterUpdate, err error) {
		clusterUpdateCh.Send(clusterUpdateErr{u: update, err: err})
		// Calls another watch inline, to ensure there's deadlock.
		c.WatchCluster("another-random-name", func(common.ClusterUpdate, error) {})
		if _, err := v2Client.addWatches[common.V2ClusterURL].Receive(); firstTime && err != nil {
			t.Fatalf("want new watch to start, got error %v", err)
		}
		firstTime = false
	})
	if _, err := v2Client.addWatches[common.V2ClusterURL].Receive(); err != nil {
		t.Fatalf("want new watch to start, got error %v", err)
	}

	wantUpdate := common.ClusterUpdate{ServiceName: testEDSName}
	v2Client.r.NewClusters(map[string]common.ClusterUpdate{
		testCDSName: wantUpdate,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}

	wantUpdate2 := common.ClusterUpdate{ServiceName: testEDSName + "2"}
	v2Client.r.NewClusters(map[string]common.ClusterUpdate{
		testCDSName: wantUpdate2,
	})

	if u, err := clusterUpdateCh.Receive(); err != nil || u != (clusterUpdateErr{wantUpdate2, nil}) {
		t.Errorf("unexpected clusterUpdate: %v, error receiving from channel: %v", u, err)
	}
}

// waitForNilErr waits for a nil error value to be received on the
// provided channel.
func waitForNilErr(t *testing.T, ch *testutils.Channel) {
	t.Helper()

	val, err := ch.Receive()
	if err == testutils.ErrRecvTimeout {
		t.Fatalf("Timeout expired when expecting update")
	}
	if val != nil {
		if cbErr := val.(error); cbErr != nil {
			t.Fatal(cbErr)
		}
	}
}
