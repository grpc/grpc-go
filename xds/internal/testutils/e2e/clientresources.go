/*
 *
 * Copyright 2021 gRPC authors.
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

package e2e

import (
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
)

func any(m proto.Message) *anypb.Any {
	a, err := ptypes.MarshalAny(m)
	if err != nil {
		panic("error marshalling any: " + err.Error())
	}
	return a
}

// DefaultClientResources returns a set of resources (LDS, RDS, CDS, EDS) for a
// client to generically connect to one server.
func DefaultClientResources(target, nodeID, host string, port uint32) UpdateOptions {
	const routeConfigName = "route"
	const clusterName = "cluster"
	const endpointsName = "endpoints"

	return UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{DefaultListener(target, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{DefaultRouteConfig(routeConfigName, target, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{DefaultCluster(clusterName, endpointsName)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{DefaultEndpoint(endpointsName, host, port)},
	}
}

// DefaultListener returns a basic xds Listener resource.
func DefaultListener(target, routeName string) *v3listenerpb.Listener {
	hcm := any(&v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{Rds: &v3httppb.Rds{
			ConfigSource: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
			},
			RouteConfigName: routeName,
		}},
		HttpFilters: []*v3httppb.HttpFilter{HTTPFilter("router", &v3routerpb.Router{})}, // router fields are unused by grpc
	})
	return &v3listenerpb.Listener{
		Name:        target,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}
}

// HTTPFilter constructs an xds HttpFilter with the provided name and config.
func HTTPFilter(name string, config proto.Message) *v3httppb.HttpFilter {
	return &v3httppb.HttpFilter{
		Name: name,
		ConfigType: &v3httppb.HttpFilter_TypedConfig{
			TypedConfig: any(config),
		},
	}
}

// DefaultRouteConfig returns a basic xds RouteConfig resource.
func DefaultRouteConfig(routeName, ldsTarget, clusterName string) *v3routepb.RouteConfiguration {
	return &v3routepb.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{ldsTarget},
			Routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
				Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
					ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
				}},
			}},
		}},
	}
}

// DefaultCluster returns a basic xds Cluster resource.
func DefaultCluster(clusterName, edsServiceName string) *v3clusterpb.Cluster {
	return &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: edsServiceName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
	}
}

// DefaultEndpoint returns a basic xds Endpoint resource.
func DefaultEndpoint(clusterName string, host string, port uint32) *v3endpointpb.ClusterLoadAssignment {
	return &v3endpointpb.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints: []*v3endpointpb.LocalityLbEndpoints{{
			Locality: &v3corepb.Locality{SubZone: "subzone"},
			LbEndpoints: []*v3endpointpb.LbEndpoint{{
				HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{Endpoint: &v3endpointpb.Endpoint{
					Address: &v3corepb.Address{Address: &v3corepb.Address_SocketAddress{
						SocketAddress: &v3corepb.SocketAddress{
							Protocol:      v3corepb.SocketAddress_TCP,
							Address:       host,
							PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: uint32(port)}},
					}},
				}},
			}},
			LoadBalancingWeight: &wrapperspb.UInt32Value{Value: 1},
			Priority:            0,
		}},
	}
}
