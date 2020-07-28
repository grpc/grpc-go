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
	v2routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/xds/internal/version"
)

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
			wantUpdate: RouteConfigUpdate{Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{clusterName: 1}}}},
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
			wantUpdate: RouteConfigUpdate{Routes: []*Route{{Prefix: newStringP("/"), Action: map[string]uint32{clusterName: 1}}}},
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
			wantUpdate: RouteConfigUpdate{Routes: []*Route{{Prefix: newStringP("/"), Action: map[string]uint32{"a": 2, "b": 3, "c": 5}}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, gotError := generateRDSUpdateFromRouteConfiguration(test.rc, ldsTarget, nil)
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
				v2RouteConfigName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{v2ClusterName: 1}}}},
			},
		},
		{
			name:      "v3 routeConfig resource",
			resources: []*anypb.Any{v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdate{
				v3RouteConfigName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{v3ClusterName: 1}}}},
			},
		},
		{
			name:      "multiple routeConfig resources",
			resources: []*anypb.Any{v2RouteConfig, v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdate{
				v3RouteConfigName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{v3ClusterName: 1}}}},
				v2RouteConfigName: {Routes: []*Route{{Prefix: newStringP(""), Action: map[string]uint32{v2ClusterName: 1}}}},
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

func (s) TestRoutesProtoToSlice(t *testing.T) {
	tests := []struct {
		name       string
		routes     []*v3routepb.Route
		wantRoutes []*Route
		wantErr    bool
	}{
		{
			name: "no path",
			routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{},
			}},
			wantErr: true,
		},
		{
			name: "case_sensitive is false",
			routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{
					PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					CaseSensitive: &wrapperspb.BoolValue{Value: false},
				},
			}},
			wantErr: true,
		},
		{
			name: "good",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
						Headers: []*v3routepb.HeaderMatcher{
							{
								Name: "th",
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{
									PrefixMatch: "tv",
								},
								InvertMatch: true,
							},
						},
						RuntimeFraction: &v3corepb.RuntimeFractionalPercent{
							DefaultValue: &v3typepb.FractionalPercent{
								Numerator:   1,
								Denominator: v3typepb.FractionalPercent_HUNDRED,
							},
						},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
								WeightedClusters: &v3routepb.WeightedCluster{
									Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 60}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 40}},
									},
									TotalWeight: &wrapperspb.UInt32Value{Value: 100},
								}}}},
				},
			},
			wantRoutes: []*Route{{
				Prefix: newStringP("/a/"),
				Headers: []*HeaderMatcher{
					{
						Name:        "th",
						InvertMatch: newBoolP(true),
						PrefixMatch: newStringP("tv"),
					},
				},
				Fraction: newUInt32P(10000),
				Action:   map[string]uint32{"A": 40, "B": 60},
			}},
			wantErr: false,
		},
		{
			name: "query is ignored",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
								WeightedClusters: &v3routepb.WeightedCluster{
									Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 60}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 40}},
									},
									TotalWeight: &wrapperspb.UInt32Value{Value: 100},
								}}}},
				},
				{
					Name: "with_query",
					Match: &v3routepb.RouteMatch{
						PathSpecifier:   &v3routepb.RouteMatch_Prefix{Prefix: "/b/"},
						QueryParameters: []*v3routepb.QueryParameterMatcher{{Name: "route_will_be_ignored"}},
					},
				},
			},
			// Only one route in the result, because the second one with query
			// parameters is ignored.
			wantRoutes: []*Route{{
				Prefix: newStringP("/a/"),
				Action: map[string]uint32{"A": 40, "B": 60},
			}},
			wantErr: false,
		},
	}

	cmpOpts := []cmp.Option{
		cmp.AllowUnexported(Route{}, HeaderMatcher{}, Int64Range{}),
		cmpopts.EquateEmpty(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := routesProtoToSlice(tt.routes, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("routesProtoToSlice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(got, tt.wantRoutes, cmpOpts...) {
				t.Errorf("routesProtoToSlice() got = %v, want %v, diff: %v", got, tt.wantRoutes, cmp.Diff(got, tt.wantRoutes, cmpOpts...))
			}
		})
	}
}

func newStringP(s string) *string {
	return &s
}

func newUInt32P(i uint32) *uint32 {
	return &i
}

func newBoolP(b bool) *bool {
	return &b
}
