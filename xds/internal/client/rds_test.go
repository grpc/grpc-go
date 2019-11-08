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

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
)

func TestGetClusterFromRouteConfiguration(t *testing.T) {
	const (
		target         = "foo.xyz:9999"
		matchingDomain = "foo.xyz"
	)

	tests := []struct {
		name        string
		rc          *xdspb.RouteConfiguration
		wantCluster string
	}{
		{
			name:        "no-virtual-hosts-in-rc",
			rc:          &xdspb.RouteConfiguration{},
			wantCluster: "",
		},
		{
			name: "no-domains-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{{}},
			},
			wantCluster: "",
		},
		{
			name: "non-matching-domain-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
				},
			},
			wantCluster: "",
		},
		{
			name: "no-routes-in-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
					{Domains: []string{matchingDomain}},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-match-field-is-non-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
					{
						Domains: []string{matchingDomain},
						Routes: []*routepb.Route{
							{
								Match:  &routepb.RouteMatch{},
								Action: &routepb.Route_Route{},
							},
						},
					},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-routeaction-field-is-nil",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
					{
						Domains: []string{matchingDomain},
						Routes:  []*routepb.Route{{}},
					},
				},
			},
			wantCluster: "",
		},
		{
			name: "default-route-cluster-field-is-empty",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
					{
						Domains: []string{matchingDomain},
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
			wantCluster: "",
		},
		{
			name: "good-rc",
			rc: &xdspb.RouteConfiguration{
				VirtualHosts: []*routepb.VirtualHost{
					{Domains: []string{"foo", "xyz"}},
					{
						Domains: []string{matchingDomain},
						Routes: []*routepb.Route{
							{
								Action: &routepb.Route_Route{
									Route: &routepb.RouteAction{
										ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: "cluster"},
									},
								},
							},
						},
					},
				},
			},
			wantCluster: "cluster",
		},
	}

	for _, test := range tests {
		if gotCluster := getClusterFromRouteConfiguration(test.rc, target); gotCluster != test.wantCluster {
			t.Errorf("%s: getClusterFromRouteConfiguration(%+v, %v) = %v, want %v", test.name, test.rc, target, gotCluster, test.wantCluster)
		}
	}
}
