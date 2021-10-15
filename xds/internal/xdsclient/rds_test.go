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

package xdsclient

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/types/known/durationpb"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
)

func (s) TestRDSGenerateRDSUpdateFromRouteConfiguration(t *testing.T) {
	const (
		uninterestingDomain      = "uninteresting.domain"
		uninterestingClusterName = "uninterestingClusterName"
		ldsTarget                = "lds.target.good:1111"
		routeName                = "routeName"
		clusterName              = "clusterName"
	)

	var (
		goodRouteConfigWithFilterConfigs = func(cfgs map[string]*anypb.Any) *v3routepb.RouteConfiguration {
			return &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsTarget},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName}},
						},
					}},
					TypedPerFilterConfig: cfgs,
				}},
			}
		}
		goodUpdateWithFilterConfigs = func(cfgs map[string]httpfilter.FilterConfig) RouteConfigUpdate {
			return RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{{
					Domains: []string{ldsTarget},
					Routes: []*Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}},
						RouteAction:      RouteActionRoute,
					}},
					HTTPFilterConfigOverride: cfgs,
				}},
			}
		}
		goodRouteConfigWithRetryPolicy = func(vhrp *v3routepb.RetryPolicy, rrp *v3routepb.RetryPolicy) *v3routepb.RouteConfiguration {
			return &v3routepb.RouteConfiguration{
				Name: routeName,
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{ldsTarget},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
						Action: &v3routepb.Route_Route{
							Route: &v3routepb.RouteAction{
								ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
								RetryPolicy:      rrp,
							},
						},
					}},
					RetryPolicy: vhrp,
				}},
			}
		}
		goodUpdateWithRetryPolicy = func(vhrc *RetryConfig, rrc *RetryConfig) RouteConfigUpdate {
			if !env.RetrySupport {
				vhrc = nil
				rrc = nil
			}
			return RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{{
					Domains: []string{ldsTarget},
					Routes: []*Route{{
						Prefix:           newStringP("/"),
						WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}},
						RouteAction:      RouteActionRoute,
						RetryConfig:      rrc,
					}},
					RetryConfig: vhrc,
				}},
			}
		}
		defaultRetryBackoff       = RetryBackoff{BaseInterval: 25 * time.Millisecond, MaxInterval: 250 * time.Millisecond}
		goodUpdateIfRetryDisabled = func() RouteConfigUpdate {
			if env.RetrySupport {
				return RouteConfigUpdate{}
			}
			return goodUpdateWithRetryPolicy(nil, nil)
		}
	)

	tests := []struct {
		name       string
		rc         *v3routepb.RouteConfiguration
		wantUpdate RouteConfigUpdate
		wantError  bool
	}{
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
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{Prefix: newStringP("/"),
							CaseInsensitive:  true,
							WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}},
							RouteAction:      RouteActionRoute}},
					},
				},
			},
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
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{uninterestingDomain},
						Routes: []*Route{{Prefix: newStringP(""),
							WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
							RouteAction:      RouteActionRoute}},
					},
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{Prefix: newStringP(""),
							WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}},
							RouteAction:      RouteActionRoute}},
					},
				},
			},
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
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{Prefix: newStringP("/"),
							WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}},
							RouteAction:      RouteActionRoute}},
					},
				},
			},
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
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{
							Prefix: newStringP("/"),
							WeightedClusters: map[string]WeightedCluster{
								"a": {Weight: 2},
								"b": {Weight: 3},
								"c": {Weight: 5},
							},
							RouteAction: RouteActionRoute,
						}},
					},
				},
			},
		},
		{
			name: "good-route-config-with-max-stream-duration",
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
										ClusterSpecifier:  &v3routepb.RouteAction_Cluster{Cluster: clusterName},
										MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{MaxStreamDuration: durationpb.New(time.Second)},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{
							Prefix:            newStringP("/"),
							WeightedClusters:  map[string]WeightedCluster{clusterName: {Weight: 1}},
							MaxStreamDuration: newDurationP(time.Second),
							RouteAction:       RouteActionRoute,
						}},
					},
				},
			},
		},
		{
			name: "good-route-config-with-grpc-timeout-header-max",
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
										ClusterSpecifier:  &v3routepb.RouteAction_Cluster{Cluster: clusterName},
										MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{GrpcTimeoutHeaderMax: durationpb.New(time.Second)},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{
							Prefix:            newStringP("/"),
							WeightedClusters:  map[string]WeightedCluster{clusterName: {Weight: 1}},
							MaxStreamDuration: newDurationP(time.Second),
							RouteAction:       RouteActionRoute,
						}},
					},
				},
			},
		},
		{
			name: "good-route-config-with-both-timeouts",
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
										ClusterSpecifier:  &v3routepb.RouteAction_Cluster{Cluster: clusterName},
										MaxStreamDuration: &v3routepb.RouteAction_MaxStreamDuration{MaxStreamDuration: durationpb.New(2 * time.Second), GrpcTimeoutHeaderMax: durationpb.New(0)},
									},
								},
							},
						},
					},
				},
			},
			wantUpdate: RouteConfigUpdate{
				VirtualHosts: []*VirtualHost{
					{
						Domains: []string{ldsTarget},
						Routes: []*Route{{
							Prefix:            newStringP("/"),
							WeightedClusters:  map[string]WeightedCluster{clusterName: {Weight: 1}},
							MaxStreamDuration: newDurationP(0),
							RouteAction:       RouteActionRoute,
						}},
					},
				},
			},
		},
		{
			name:       "good-route-config-with-http-filter-config",
			rc:         goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": customFilterConfig}),
			wantUpdate: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterConfig}}),
		},
		{
			name:       "good-route-config-with-http-filter-config-in-old-typed-struct",
			rc:         goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedCustomFilterOldTypedStructConfig}),
			wantUpdate: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterOldTypedStructConfig}}),
		},
		{
			name:       "good-route-config-with-http-filter-config-in-new-typed-struct",
			rc:         goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedCustomFilterNewTypedStructConfig}),
			wantUpdate: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterNewTypedStructConfig}}),
		},
		{
			name:       "good-route-config-with-optional-http-filter-config",
			rc:         goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("custom.filter")}),
			wantUpdate: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterConfig}}),
		},
		{
			name:      "good-route-config-with-http-err-filter-config",
			rc:        goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": errFilterConfig}),
			wantError: true,
		},
		{
			name:      "good-route-config-with-http-optional-err-filter-config",
			rc:        goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("err.custom.filter")}),
			wantError: true,
		},
		{
			name:      "good-route-config-with-http-unknown-filter-config",
			rc:        goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": unknownFilterConfig}),
			wantError: true,
		},
		{
			name:       "good-route-config-with-http-optional-unknown-filter-config",
			rc:         goodRouteConfigWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("unknown.custom.filter")}),
			wantUpdate: goodUpdateWithFilterConfigs(nil),
		},
		{
			name: "good-route-config-with-retry-policy",
			rc: goodRouteConfigWithRetryPolicy(
				&v3routepb.RetryPolicy{RetryOn: "cancelled"},
				&v3routepb.RetryPolicy{RetryOn: "deadline-exceeded,unsupported", NumRetries: &wrapperspb.UInt32Value{Value: 2}}),
			wantUpdate: goodUpdateWithRetryPolicy(
				&RetryConfig{RetryOn: map[codes.Code]bool{codes.Canceled: true}, NumRetries: 1, RetryBackoff: defaultRetryBackoff},
				&RetryConfig{RetryOn: map[codes.Code]bool{codes.DeadlineExceeded: true}, NumRetries: 2, RetryBackoff: defaultRetryBackoff}),
		},
		{
			name: "good-route-config-with-retry-backoff",
			rc: goodRouteConfigWithRetryPolicy(
				&v3routepb.RetryPolicy{RetryOn: "internal", RetryBackOff: &v3routepb.RetryPolicy_RetryBackOff{BaseInterval: durationpb.New(10 * time.Millisecond), MaxInterval: durationpb.New(10 * time.Millisecond)}},
				&v3routepb.RetryPolicy{RetryOn: "resource-exhausted", RetryBackOff: &v3routepb.RetryPolicy_RetryBackOff{BaseInterval: durationpb.New(10 * time.Millisecond)}}),
			wantUpdate: goodUpdateWithRetryPolicy(
				&RetryConfig{RetryOn: map[codes.Code]bool{codes.Internal: true}, NumRetries: 1, RetryBackoff: RetryBackoff{BaseInterval: 10 * time.Millisecond, MaxInterval: 10 * time.Millisecond}},
				&RetryConfig{RetryOn: map[codes.Code]bool{codes.ResourceExhausted: true}, NumRetries: 1, RetryBackoff: RetryBackoff{BaseInterval: 10 * time.Millisecond, MaxInterval: 100 * time.Millisecond}}),
		},
		{
			name:       "bad-retry-policy-0-retries",
			rc:         goodRouteConfigWithRetryPolicy(&v3routepb.RetryPolicy{RetryOn: "cancelled", NumRetries: &wrapperspb.UInt32Value{Value: 0}}, nil),
			wantUpdate: goodUpdateIfRetryDisabled(),
			wantError:  env.RetrySupport,
		},
		{
			name:       "bad-retry-policy-0-base-interval",
			rc:         goodRouteConfigWithRetryPolicy(&v3routepb.RetryPolicy{RetryOn: "cancelled", RetryBackOff: &v3routepb.RetryPolicy_RetryBackOff{BaseInterval: durationpb.New(0)}}, nil),
			wantUpdate: goodUpdateIfRetryDisabled(),
			wantError:  env.RetrySupport,
		},
		{
			name:       "bad-retry-policy-negative-max-interval",
			rc:         goodRouteConfigWithRetryPolicy(&v3routepb.RetryPolicy{RetryOn: "cancelled", RetryBackOff: &v3routepb.RetryPolicy_RetryBackOff{MaxInterval: durationpb.New(-time.Second)}}, nil),
			wantUpdate: goodUpdateIfRetryDisabled(),
			wantError:  env.RetrySupport,
		},
		{
			name:       "bad-retry-policy-negative-max-interval-no-known-retry-on",
			rc:         goodRouteConfigWithRetryPolicy(&v3routepb.RetryPolicy{RetryOn: "something", RetryBackOff: &v3routepb.RetryPolicy_RetryBackOff{MaxInterval: durationpb.New(-time.Second)}}, nil),
			wantUpdate: goodUpdateIfRetryDisabled(),
			wantError:  env.RetrySupport,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, gotError := generateRDSUpdateFromRouteConfiguration(test.rc, nil, false)
			if (gotError != nil) != test.wantError ||
				!cmp.Equal(gotUpdate, test.wantUpdate, cmpopts.EquateEmpty(),
					cmp.Transformer("FilterConfig", func(fc httpfilter.FilterConfig) string {
						return fmt.Sprint(fc)
					})) {
				t.Errorf("generateRDSUpdateFromRouteConfiguration(%+v, %v) returned unexpected, diff (-want +got):\\n%s", test.rc, ldsTarget, cmp.Diff(test.wantUpdate, gotUpdate, cmpopts.EquateEmpty()))
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
		v2RouteConfig = testutils.MarshalAny(&v2xdspb.RouteConfiguration{
			Name:         v2RouteConfigName,
			VirtualHosts: v2VirtualHost,
		})
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
		v3RouteConfig = testutils.MarshalAny(&v3routepb.RouteConfiguration{
			Name:         v3RouteConfigName,
			VirtualHosts: v3VirtualHost,
		})
	)
	const testVersion = "test-version-rds"

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]RouteConfigUpdateErrTuple
		wantMD     UpdateMetadata
		wantErr    bool
	}{
		{
			name:      "non-routeConfig resource type",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     cmpopts.AnyError,
				},
			},
			wantErr: true,
		},
		{
			name: "badly marshaled routeconfig resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3RouteConfigURL,
					Value:   []byte{1, 2, 3, 4},
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     cmpopts.AnyError,
				},
			},
			wantErr: true,
		},
		{
			name: "empty resource list",
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v2 routeConfig resource",
			resources: []*anypb.Any{v2RouteConfig},
			wantUpdate: map[string]RouteConfigUpdateErrTuple{
				v2RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v2ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v2RouteConfig,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 routeConfig resource",
			resources: []*anypb.Any{v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdateErrTuple{
				v3RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v3ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v3RouteConfig,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "multiple routeConfig resources",
			resources: []*anypb.Any{v2RouteConfig, v3RouteConfig},
			wantUpdate: map[string]RouteConfigUpdateErrTuple{
				v3RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v3ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v3RouteConfig,
				}},
				v2RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v2ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v2RouteConfig,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			// To test that unmarshal keeps processing on errors.
			name: "good and bad routeConfig resources",
			resources: []*anypb.Any{
				v2RouteConfig,
				testutils.MarshalAny(&v3routepb.RouteConfiguration{
					Name: "bad",
					VirtualHosts: []*v3routepb.VirtualHost{
						{Domains: []string{ldsTarget},
							Routes: []*v3routepb.Route{{
								Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_ConnectMatcher_{}},
							}}}}}),
				v3RouteConfig,
			},
			wantUpdate: map[string]RouteConfigUpdateErrTuple{
				v3RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v3ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v3RouteConfig,
				}},
				v2RouteConfigName: {Update: RouteConfigUpdate{
					VirtualHosts: []*VirtualHost{
						{
							Domains: []string{uninterestingDomain},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{uninterestingClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
						{
							Domains: []string{ldsTarget},
							Routes: []*Route{{Prefix: newStringP(""),
								WeightedClusters: map[string]WeightedCluster{v2ClusterName: {Weight: 1}},
								RouteAction:      RouteActionRoute}},
						},
					},
					Raw: v2RouteConfig,
				}},
				"bad": {Err: cmpopts.AnyError},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     cmpopts.AnyError,
				},
			},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := &UnmarshalOptions{
				Version:   testVersion,
				Resources: test.resources,
			}
			update, md, err := UnmarshalRouteConfig(opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("UnmarshalRouteConfig(%+v), got err: %v, wantErr: %v", opts, err, test.wantErr)
			}
			if diff := cmp.Diff(update, test.wantUpdate, cmpOpts); diff != "" {
				t.Errorf("got unexpected update, diff (-got +want): %v", diff)
			}
			if diff := cmp.Diff(md, test.wantMD, cmpOptsIgnoreDetails); diff != "" {
				t.Errorf("got unexpected metadata, diff (-got +want): %v", diff)
			}
		})
	}
}

func (s) TestRoutesProtoToSlice(t *testing.T) {
	var (
		goodRouteWithFilterConfigs = func(cfgs map[string]*anypb.Any) []*v3routepb.Route {
			// Sets per-filter config in cluster "B" and in the route.
			return []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{
					PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					CaseSensitive: &wrapperspb.BoolValue{Value: false},
				},
				Action: &v3routepb.Route_Route{
					Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
							WeightedClusters: &v3routepb.WeightedCluster{
								Clusters: []*v3routepb.WeightedCluster_ClusterWeight{
									{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 60}, TypedPerFilterConfig: cfgs},
									{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 40}},
								},
								TotalWeight: &wrapperspb.UInt32Value{Value: 100},
							}}}},
				TypedPerFilterConfig: cfgs,
			}}
		}
		goodUpdateWithFilterConfigs = func(cfgs map[string]httpfilter.FilterConfig) []*Route {
			// Sets per-filter config in cluster "B" and in the route.
			return []*Route{{
				Prefix:                   newStringP("/"),
				CaseInsensitive:          true,
				WeightedClusters:         map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60, HTTPFilterConfigOverride: cfgs}},
				HTTPFilterConfigOverride: cfgs,
				RouteAction:              RouteActionRoute,
			}}
		}
	)

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
			}},
			wantRoutes: []*Route{{
				Prefix:           newStringP("/"),
				CaseInsensitive:  true,
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				RouteAction:      RouteActionRoute,
			}},
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
				Fraction:         newUInt32P(10000),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				RouteAction:      RouteActionRoute,
			}},
			wantErr: false,
		},
		{
			name: "good with regex matchers",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "/a/"}},
						Headers: []*v3routepb.HeaderMatcher{
							{
								Name:                 "th",
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "tv"}},
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
				Regex: func() *regexp.Regexp { return regexp.MustCompile("/a/") }(),
				Headers: []*HeaderMatcher{
					{
						Name:        "th",
						InvertMatch: newBoolP(false),
						RegexMatch:  func() *regexp.Regexp { return regexp.MustCompile("tv") }(),
					},
				},
				Fraction:         newUInt32P(10000),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				RouteAction:      RouteActionRoute,
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
				Prefix:           newStringP("/a/"),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				RouteAction:      RouteActionRoute,
			}},
			wantErr: false,
		},
		{
			name: "unrecognized path specifier",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_ConnectMatcher_{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "bad regex in path specifier",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "??"}},
						Headers: []*v3routepb.HeaderMatcher{
							{
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "tv"},
							},
						},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "bad regex in header specifier",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
						Headers: []*v3routepb.HeaderMatcher{
							{
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "??"}},
							},
						},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "unrecognized header match specifier",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
						Headers: []*v3routepb.HeaderMatcher{
							{
								Name:                 "th",
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_StringMatch{},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no cluster in weighted clusters action",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_WeightedClusters{
								WeightedClusters: &v3routepb.WeightedCluster{}}}},
				},
			},
			wantErr: true,
		},
		{
			name: "all 0-weight clusters in weighted clusters action",
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
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 0}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 0}},
									},
									TotalWeight: &wrapperspb.UInt32Value{Value: 0},
								}}}},
				},
			},
			wantErr: true,
		},
		{
			name: "totalWeight is nil in weighted clusters action",
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
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 20}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 30}},
									},
								}}}},
				},
			},
			wantErr: true,
		},
		{
			name: "The sum of all weighted clusters is not equal totalWeight",
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
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 50}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 20}},
									},
									TotalWeight: &wrapperspb.UInt32Value{Value: 100},
								}}}},
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported cluster specifier",
			routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/a/"},
					},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_ClusterSpecifierPlugin{}}},
				},
			},
			wantErr: true,
		},
		{
			name: "default totalWeight is 100 in weighted clusters action",
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
								}}}},
				},
			},
			wantRoutes: []*Route{{
				Prefix:           newStringP("/a/"),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				RouteAction:      RouteActionRoute,
			}},
			wantErr: false,
		},
		{
			name: "default totalWeight is 100 in weighted clusters action",
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
										{Name: "B", Weight: &wrapperspb.UInt32Value{Value: 30}},
										{Name: "A", Weight: &wrapperspb.UInt32Value{Value: 20}},
									},
									TotalWeight: &wrapperspb.UInt32Value{Value: 50},
								}}}},
				},
			},
			wantRoutes: []*Route{{
				Prefix:           newStringP("/a/"),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 20}, "B": {Weight: 30}},
				RouteAction:      RouteActionRoute,
			}},
			wantErr: false,
		},
		{
			name: "good-with-channel-id-hash-policy",
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
								}},
							HashPolicy: []*v3routepb.RouteAction_HashPolicy{
								{PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{Key: "io.grpc.channel_id"}}},
							},
						}},
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
				Fraction:         newUInt32P(10000),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				HashPolicies: []*HashPolicy{
					{HashPolicyType: HashPolicyTypeChannelID},
				},
				RouteAction: RouteActionRoute,
			}},
			wantErr: false,
		},
		// This tests that policy.Regex ends up being nil if RegexRewrite is not
		// set in xds response.
		{
			name: "good-with-header-hash-policy-no-regex-specified",
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
								}},
							HashPolicy: []*v3routepb.RouteAction_HashPolicy{
								{PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{Header: &v3routepb.RouteAction_HashPolicy_Header{HeaderName: ":path"}}},
							},
						}},
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
				Fraction:         newUInt32P(10000),
				WeightedClusters: map[string]WeightedCluster{"A": {Weight: 40}, "B": {Weight: 60}},
				HashPolicies: []*HashPolicy{
					{HashPolicyType: HashPolicyTypeHeader,
						HeaderName: ":path"},
				},
				RouteAction: RouteActionRoute,
			}},
			wantErr: false,
		},
		{
			name:       "with custom HTTP filter config",
			routes:     goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": customFilterConfig}),
			wantRoutes: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterConfig}}),
		},
		{
			name:       "with custom HTTP filter config in typed struct",
			routes:     goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedCustomFilterOldTypedStructConfig}),
			wantRoutes: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterOldTypedStructConfig}}),
		},
		{
			name:       "with optional custom HTTP filter config",
			routes:     goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("custom.filter")}),
			wantRoutes: goodUpdateWithFilterConfigs(map[string]httpfilter.FilterConfig{"foo": filterConfig{Override: customFilterConfig}}),
		},
		{
			name:    "with erroring custom HTTP filter config",
			routes:  goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": errFilterConfig}),
			wantErr: true,
		},
		{
			name:    "with optional erroring custom HTTP filter config",
			routes:  goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("err.custom.filter")}),
			wantErr: true,
		},
		{
			name:    "with unknown custom HTTP filter config",
			routes:  goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": unknownFilterConfig}),
			wantErr: true,
		},
		{
			name:       "with optional unknown custom HTTP filter config",
			routes:     goodRouteWithFilterConfigs(map[string]*anypb.Any{"foo": wrappedOptionalFilter("unknown.custom.filter")}),
			wantRoutes: goodUpdateWithFilterConfigs(nil),
		},
	}

	cmpOpts := []cmp.Option{
		cmp.AllowUnexported(Route{}, HeaderMatcher{}, Int64Range{}, regexp.Regexp{}),
		cmpopts.EquateEmpty(),
		cmp.Transformer("FilterConfig", func(fc httpfilter.FilterConfig) string {
			return fmt.Sprint(fc)
		}),
	}
	oldRingHashSupport := env.RingHashSupport
	env.RingHashSupport = true
	defer func() { env.RingHashSupport = oldRingHashSupport }()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := routesProtoToSlice(tt.routes, nil, false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("routesProtoToSlice() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.wantRoutes, cmpOpts...); diff != "" {
				t.Fatalf("routesProtoToSlice() returned unexpected diff (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestHashPoliciesProtoToSlice(t *testing.T) {
	tests := []struct {
		name             string
		hashPolicies     []*v3routepb.RouteAction_HashPolicy
		wantHashPolicies []*HashPolicy
		wantErr          bool
	}{
		// header-hash-policy tests a basic hash policy that specifies to hash a
		// certain header.
		{
			name: "header-hash-policy",
			hashPolicies: []*v3routepb.RouteAction_HashPolicy{
				{
					PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
						Header: &v3routepb.RouteAction_HashPolicy_Header{
							HeaderName: ":path",
							RegexRewrite: &v3matcherpb.RegexMatchAndSubstitute{
								Pattern:      &v3matcherpb.RegexMatcher{Regex: "/products"},
								Substitution: "/products",
							},
						},
					},
				},
			},
			wantHashPolicies: []*HashPolicy{
				{
					HashPolicyType:    HashPolicyTypeHeader,
					HeaderName:        ":path",
					Regex:             func() *regexp.Regexp { return regexp.MustCompile("/products") }(),
					RegexSubstitution: "/products",
				},
			},
		},
		// channel-id-hash-policy tests a basic hash policy that specifies to
		// hash a unique identifier of the channel.
		{
			name: "channel-id-hash-policy",
			hashPolicies: []*v3routepb.RouteAction_HashPolicy{
				{PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{Key: "io.grpc.channel_id"}}},
			},
			wantHashPolicies: []*HashPolicy{
				{HashPolicyType: HashPolicyTypeChannelID},
			},
		},
		// unsupported-filter-state-key tests that an unsupported key in the
		// filter state hash policy are treated as a no-op.
		{
			name: "wrong-filter-state-key",
			hashPolicies: []*v3routepb.RouteAction_HashPolicy{
				{PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{Key: "unsupported key"}}},
			},
		},
		// no-op-hash-policy tests that hash policies that are not supported by
		// grpc are treated as a no-op.
		{
			name: "no-op-hash-policy",
			hashPolicies: []*v3routepb.RouteAction_HashPolicy{
				{PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{}},
			},
		},
		// header-and-channel-id-hash-policy test that a list of header and
		// channel id hash policies are successfully converted to an internal
		// struct.
		{
			name: "header-and-channel-id-hash-policy",
			hashPolicies: []*v3routepb.RouteAction_HashPolicy{
				{
					PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
						Header: &v3routepb.RouteAction_HashPolicy_Header{
							HeaderName: ":path",
							RegexRewrite: &v3matcherpb.RegexMatchAndSubstitute{
								Pattern:      &v3matcherpb.RegexMatcher{Regex: "/products"},
								Substitution: "/products",
							},
						},
					},
				},
				{
					PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{Key: "io.grpc.channel_id"}},
					Terminal:        true,
				},
			},
			wantHashPolicies: []*HashPolicy{
				{
					HashPolicyType:    HashPolicyTypeHeader,
					HeaderName:        ":path",
					Regex:             func() *regexp.Regexp { return regexp.MustCompile("/products") }(),
					RegexSubstitution: "/products",
				},
				{
					HashPolicyType: HashPolicyTypeChannelID,
					Terminal:       true,
				},
			},
		},
	}

	oldRingHashSupport := env.RingHashSupport
	env.RingHashSupport = true
	defer func() { env.RingHashSupport = oldRingHashSupport }()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hashPoliciesProtoToSlice(tt.hashPolicies, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("hashPoliciesProtoToSlice() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.wantHashPolicies, cmp.AllowUnexported(regexp.Regexp{})); diff != "" {
				t.Fatalf("hashPoliciesProtoToSlice() returned unexpected diff (-got +want):\n%s", diff)
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

func newDurationP(d time.Duration) *time.Duration {
	return &d
}
