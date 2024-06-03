/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

func (s) TestDumpResources(t *testing.T) {
	// Initialize the xDS resources to be used in this test.
	ldsTargets := []string{"lds.target.good:0000", "lds.target.good:1111"}
	rdsTargets := []string{"route-config-0", "route-config-1"}
	cdsTargets := []string{"cluster-0", "cluster-1"}
	edsTargets := []string{"endpoints-0", "endpoints-1"}
	listeners := make([]*v3listenerpb.Listener, len(ldsTargets))
	listenerAnys := make([]*anypb.Any, len(ldsTargets))
	for i := range ldsTargets {
		listeners[i] = e2e.DefaultClientListener(ldsTargets[i], rdsTargets[i])
		listenerAnys[i] = testutils.MarshalAny(t, listeners[i])
	}
	routes := make([]*v3routepb.RouteConfiguration, len(rdsTargets))
	routeAnys := make([]*anypb.Any, len(rdsTargets))
	for i := range rdsTargets {
		routes[i] = e2e.DefaultRouteConfig(rdsTargets[i], ldsTargets[i], cdsTargets[i])
		routeAnys[i] = testutils.MarshalAny(t, routes[i])
	}
	clusters := make([]*v3clusterpb.Cluster, len(cdsTargets))
	clusterAnys := make([]*anypb.Any, len(cdsTargets))
	for i := range cdsTargets {
		clusters[i] = e2e.DefaultCluster(cdsTargets[i], edsTargets[i], e2e.SecurityLevelNone)
		clusterAnys[i] = testutils.MarshalAny(t, clusters[i])
	}
	endpoints := make([]*v3endpointpb.ClusterLoadAssignment, len(edsTargets))
	endpointAnys := make([]*anypb.Any, len(edsTargets))
	ips := []string{"0.0.0.0", "1.1.1.1"}
	ports := []uint32{123, 456}
	for i := range edsTargets {
		endpoints[i] = e2e.DefaultEndpoint(edsTargets[i], ips[i], ports[i:i+1])
		endpointAnys[i] = testutils.MarshalAny(t, endpoints[i])
	}

	// Spin up an xDS management server on a local port.
	mgmtServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

	// Create an xDS client with the above bootstrap contents.
	client, close, err := xdsclient.NewForTesting(xdsclient.OptionsForTesting{Contents: bootstrapContents})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Dump resources and expect empty configs.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := compareUpdateMetadata(ctx, client.DumpResources, nil); err != nil {
		t.Fatal(err)
	}

	// Register watches, dump resources and expect configs in requested state.
	for _, target := range ldsTargets {
		xdsresource.WatchListener(client, target, noopListenerWatcher{})
	}
	for _, target := range rdsTargets {
		xdsresource.WatchRouteConfig(client, target, noopRouteConfigWatcher{})
	}
	for _, target := range cdsTargets {
		xdsresource.WatchCluster(client, target, noopClusterWatcher{})
	}
	for _, target := range edsTargets {
		xdsresource.WatchEndpoints(client, target, noopEndpointsWatcher{})
	}
	want := []*v3statuspb.ClientConfig_GenericXdsConfig{
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
		},
	}
	if err := compareUpdateMetadata(ctx, client.DumpResources, want); err != nil {
		t.Fatal(err)
	}

	// Configure the resources on the management server.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}); err != nil {
		t.Fatal(err)
	}

	// Dump resources and expect ACK configs.
	want = []*v3statuspb.ClientConfig_GenericXdsConfig{
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    listenerAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    listenerAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    routeAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    routeAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    clusterAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    clusterAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    endpointAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "1",
			XdsConfig:    endpointAnys[1],
		},
	}
	if err := compareUpdateMetadata(ctx, client.DumpResources, want); err != nil {
		t.Fatal(err)
	}

	// Update the first resource of each type in the management server to a
	// value which is expected to be NACK'ed by the xDS client.
	const nackResourceIdx = 0
	listeners[nackResourceIdx].ApiListener = &v3listenerpb.ApiListener{}
	routes[nackResourceIdx].VirtualHosts = []*v3routepb.VirtualHost{{Routes: []*v3routepb.Route{{}}}}
	clusters[nackResourceIdx].ClusterDiscoveryType = &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC}
	endpoints[nackResourceIdx].Endpoints = []*v3endpointpb.LocalityLbEndpoints{{}}
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners,
		Routes:         routes,
		Clusters:       clusters,
		Endpoints:      endpoints,
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}

	want = []*v3statuspb.ClientConfig_GenericXdsConfig{
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_NACKED,
			VersionInfo:  "1",
			ErrorState: &v3adminpb.UpdateFailureState{
				VersionInfo: "2",
			},
			XdsConfig: listenerAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.listener.v3.Listener",
			Name:         ldsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "2",
			XdsConfig:    listenerAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_NACKED,
			VersionInfo:  "1",
			ErrorState: &v3adminpb.UpdateFailureState{
				VersionInfo: "2",
			},
			XdsConfig: routeAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
			Name:         rdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "2",
			XdsConfig:    routeAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_NACKED,
			VersionInfo:  "1",
			ErrorState: &v3adminpb.UpdateFailureState{
				VersionInfo: "2",
			},
			XdsConfig: clusterAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			Name:         cdsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "2",
			XdsConfig:    clusterAnys[1],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[0],
			ClientStatus: v3adminpb.ClientResourceStatus_NACKED,
			VersionInfo:  "1",
			ErrorState: &v3adminpb.UpdateFailureState{
				VersionInfo: "2",
			},
			XdsConfig: endpointAnys[0],
		},
		{
			TypeUrl:      "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			Name:         edsTargets[1],
			ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
			VersionInfo:  "2",
			XdsConfig:    endpointAnys[1],
		},
	}
	if err := compareUpdateMetadata(ctx, client.DumpResources, want); err != nil {
		t.Fatal(err)
	}
}
