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
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/golang/protobuf/proto"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
)

func (s) TestRDSGenerateRDSUpdateFromRouteConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		rc         *xdspb.RouteConfiguration
		wantUpdate rdsUpdate
		wantError  bool
	}{
		{
			name:      "no-virtual-hosts-in-rc",
			rc:        emptyRouteConfig,
			wantError: true,
		},
		{
			name:      "no-domains-in-rc",
			rc:        noDomainsInRouteConfig,
			wantError: true,
		},
		{
			name: "non-matching-domain-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{uninterestingDomain}},
				},
			},
			wantError: true,
		},
		{
			name: "no-routes-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{goodLDSTarget1}},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-match-field-is-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Action: &routepb.Route_Route{
									Route: &routepb.RouteAction{
										ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
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
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Match:  &routepb.RouteMatch{},
								Action: &routepb.Route_Route{},
							},
						},
					},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-routeaction-field-is-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes:  []*routepb.Route{{}},
					},
				},
			},
			wantError: true,
		},
		{
			name: "default-route-cluster-field-is-empty",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{
						Domains: []string{goodLDSTarget1},
						Routes: []*routepb.Route{
							{
								Action: &routepb.Route_Route{
									Route: &routepb.RouteAction{
										ClusterSpecifier: &routepb.RouteAction_ClusterHeader{},
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
			rc: &xdspb.RouteConfiguration{
				Name: goodRouteName1,
				VirtualHosts: []*routepb.VirtualHost{{
					Domains: []string{goodLDSTarget1},
					Routes: []*routepb.Route{{
						Match: &routepb.RouteMatch{
							PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/"},
							CaseSensitive: &wrapperspb.BoolValue{Value: false},
						},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
							}}}}}}},
			wantError: true,
		},
		{
			name:       "good-route-config-with-empty-string-route",
			rc:         goodRouteConfig1,
			wantUpdate: rdsUpdate{weightedCluster: map[string]uint32{goodClusterName1: 1}},
		},
		{
			// default route's match is not empty string, but "/".
			name: "good-route-config-with-slash-string-route",
			rc: &xdspb.RouteConfiguration{
				Name: goodRouteName1,
				VirtualHosts: []*routepb.VirtualHost{{
					Domains: []string{goodLDSTarget1},
					Routes: []*routepb.Route{{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
							}}}}}}},
			wantUpdate: rdsUpdate{weightedCluster: map[string]uint32{goodClusterName1: 1}},
		},

		{
			// weights not add up to total-weight.
			name: "route-config-with-weighted_clusters_weights_not_add_up",
			rc: &xdspb.RouteConfiguration{
				Name: goodRouteName1,
				VirtualHosts: []*routepb.VirtualHost{{
					Domains: []string{goodLDSTarget1},
					Routes: []*routepb.Route{{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_WeightedClusters{
									WeightedClusters: &routepb.WeightedCluster{
										Clusters: []*routepb.WeightedCluster_ClusterWeight{
											{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
											{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
											{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
										},
										TotalWeight: &wrapperspb.UInt32Value{Value: 30},
									}}}}}}}}},
			wantError: true,
		},
		{
			name: "good-route-config-with-weighted_clusters",
			rc: &xdspb.RouteConfiguration{
				Name: goodRouteName1,
				VirtualHosts: []*routepb.VirtualHost{{
					Domains: []string{goodLDSTarget1},
					Routes: []*routepb.Route{{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_WeightedClusters{
									WeightedClusters: &routepb.WeightedCluster{
										Clusters: []*routepb.WeightedCluster_ClusterWeight{
											{Name: "a", Weight: &wrapperspb.UInt32Value{Value: 2}},
											{Name: "b", Weight: &wrapperspb.UInt32Value{Value: 3}},
											{Name: "c", Weight: &wrapperspb.UInt32Value{Value: 5}},
										},
										TotalWeight: &wrapperspb.UInt32Value{Value: 10},
									}}}}}}}}},
			wantUpdate: rdsUpdate{weightedCluster: map[string]uint32{"a": 2, "b": 3, "c": 5}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, gotError := generateRDSUpdateFromRouteConfiguration(test.rc, goodLDSTarget1)
			if !cmp.Equal(gotUpdate, test.wantUpdate, cmp.AllowUnexported(rdsUpdate{})) || (gotError != nil) != test.wantError {
				t.Errorf("generateRDSUpdateFromRouteConfiguration(%+v, %v) = %v, want %v", test.rc, goodLDSTarget1, gotUpdate, test.wantUpdate)
			}
		})
	}
}

// doLDS makes a LDS watch, and waits for the response and ack to finish.
//
// This is called by RDS tests to start LDS first, because LDS is a
// pre-requirement for RDS, and RDS handle would fail without an existing LDS
// watch.
func doLDS(t *testing.T, v2c *v2Client, fakeServer *fakeserver.Server) {
	v2c.addWatch(ldsURL, goodLDSTarget1)
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
		rdsResponse   *xdspb.DiscoveryResponse
		wantErr       bool
		wantUpdate    *rdsUpdate
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
			wantUpdate:    &rdsUpdate{weightedCluster: map[string]uint32{goodClusterName1: 1}},
			wantUpdateErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWatchHandle(t, &watchHandleTestcase{
				typeURL:          rdsURL,
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

	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()

	if v2c.handleRDSResponse(goodRDSResponse1) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}
}

// TestRDSHandleResponseWithoutRDSWatch tests the case where the v2Client
// receives an RDS response without a registered RDS watcher.
func (s) TestRDSHandleResponseWithoutRDSWatch(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) {},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	doLDS(t, v2c, fakeServer)

	if v2c.handleRDSResponse(badResourceTypeInRDSResponse) == nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
	}

	if v2c.handleRDSResponse(goodRDSResponse1) != nil {
		t.Fatal("v2c.handleRDSResponse() succeeded, should have failed")
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
		oneExactMatch = &routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"foo.bar.com"},
		}
		oneSuffixMatch = &routepb.VirtualHost{
			Name:    "one-suffix-match",
			Domains: []string{"*.bar.com"},
		}
		onePrefixMatch = &routepb.VirtualHost{
			Name:    "one-prefix-match",
			Domains: []string{"foo.bar.*"},
		}
		oneUniversalMatch = &routepb.VirtualHost{
			Name:    "one-universal-match",
			Domains: []string{"*"},
		}
		longExactMatch = &routepb.VirtualHost{
			Name:    "one-exact-match",
			Domains: []string{"v2.foo.bar.com"},
		}
		multipleMatch = &routepb.VirtualHost{
			Name:    "multiple-match",
			Domains: []string{"pi.foo.bar.com", "314.*", "*.159"},
		}
		vhs = []*routepb.VirtualHost{oneExactMatch, oneSuffixMatch, onePrefixMatch, oneUniversalMatch, longExactMatch, multipleMatch}
	)

	tests := []struct {
		name   string
		host   string
		vHosts []*routepb.VirtualHost
		want   *routepb.VirtualHost
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
		wc      *routepb.WeightedCluster
		want    map[string]uint32
		wantErr bool
	}{
		{
			name: "weight not add up to non default total",
			wc: &routepb.WeightedCluster{
				Clusters: []*routepb.WeightedCluster_ClusterWeight{
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
			wc: &routepb.WeightedCluster{
				Clusters: []*routepb.WeightedCluster_ClusterWeight{
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
			wc: &routepb.WeightedCluster{
				Clusters: []*routepb.WeightedCluster_ClusterWeight{
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
			wc: &routepb.WeightedCluster{
				Clusters: []*routepb.WeightedCluster_ClusterWeight{
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
