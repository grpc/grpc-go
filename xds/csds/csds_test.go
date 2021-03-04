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

package csds

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"

	_ "google.golang.org/grpc/xds/internal/client/v3"
)

const (
	defaultTestTimeout = 60 * time.Second
)

var cmpOpts = cmp.Options{
	cmpopts.EquateEmpty(),
	cmp.Comparer(func(a, b *timestamppb.Timestamp) bool { return true }),
	protocmp.IgnoreFields(&v3adminpb.UpdateFailureState{}, "last_update_attempt", "details"),
	protocmp.SortRepeated(func(a, b *v3adminpb.ListenersConfigDump_DynamicListener) bool {
		return strings.Compare(a.Name, b.Name) < 0
	}),
	protocmp.SortRepeated(func(a, b *v3adminpb.RoutesConfigDump_DynamicRouteConfig) bool {
		if a.RouteConfig == nil {
			return false
		}
		if b.RouteConfig == nil {
			return true
		}
		var at, bt v3routepb.RouteConfiguration
		if err := ptypes.UnmarshalAny(a.RouteConfig, &at); err != nil {
			panic("1" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.RouteConfig, &bt); err != nil {
			panic("2" + err.Error())
		}
		return strings.Compare(at.Name, bt.Name) < 0
	}),
	protocmp.SortRepeated(func(a, b *v3adminpb.ClustersConfigDump_DynamicCluster) bool {
		if a.Cluster == nil {
			return false
		}
		if b.Cluster == nil {
			return true
		}
		var at, bt v3clusterpb.Cluster
		if err := ptypes.UnmarshalAny(a.Cluster, &at); err != nil {
			panic("3" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.Cluster, &bt); err != nil {
			panic("4" + err.Error())
		}
		return strings.Compare(at.Name, bt.Name) < 0
	}),
	protocmp.SortRepeated(func(a, b *v3adminpb.EndpointsConfigDump_DynamicEndpointConfig) bool {
		if a.EndpointConfig == nil {
			return false
		}
		if b.EndpointConfig == nil {
			return true
		}
		var at, bt v3endpointpb.ClusterLoadAssignment
		if err := ptypes.UnmarshalAny(a.EndpointConfig, &at); err != nil {
			panic("5" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.EndpointConfig, &bt); err != nil {
			panic("6" + err.Error())
		}
		return strings.Compare(at.ClusterName, bt.ClusterName) < 0
	}),
	protocmp.IgnoreFields(&v3adminpb.ListenersConfigDump_DynamicListenerState{}, "last_updated"),
	protocmp.IgnoreFields(&v3adminpb.RoutesConfigDump_DynamicRouteConfig{}, "last_updated"),
	protocmp.IgnoreFields(&v3adminpb.ClustersConfigDump_DynamicCluster{}, "last_updated"),
	protocmp.IgnoreFields(&v3adminpb.EndpointsConfigDump_DynamicEndpointConfig{}, "last_updated"),
	protocmp.Transform(),
}

var (
	ldsTargets   = []string{"lds.target.good:0000", "lds.target.good:1111"}
	listeners    = make([]*v3listenerpb.Listener, len(ldsTargets))
	listenerAnys = make([]*anypb.Any, len(ldsTargets))

	rdsTargets = []string{"route-config-0", "route-config-1"}
	routes     = make([]*v3routepb.RouteConfiguration, len(rdsTargets))
	routeAnys  = make([]*anypb.Any, len(rdsTargets))

	cdsTargets  = []string{"cluster-0", "cluster-1"}
	clusters    = make([]*v3clusterpb.Cluster, len(cdsTargets))
	clusterAnys = make([]*anypb.Any, len(cdsTargets))

	edsTargets   = []string{"endpoints-0", "endpoints-1"}
	endpoints    = make([]*v3endpointpb.ClusterLoadAssignment, len(edsTargets))
	endpointAnys = make([]*anypb.Any, len(edsTargets))
	ips          = []string{"0.0.0.0", "1.1.1.1"}
	ports        = []uint32{123, 456}
)

func init() {
	for i := range ldsTargets {
		listeners[i] = &v3listenerpb.Listener{
			Name: ldsTargets[i],
			ApiListener: &v3listenerpb.ApiListener{
				ApiListener: func() *anypb.Any {
					mcm, _ := ptypes.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{
								ConfigSource: &v3corepb.ConfigSource{
									ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
								},
								RouteConfigName: rdsTargets[i],
							},
						},
						CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
							MaxStreamDuration: durationpb.New(time.Second),
						},
					})
					return mcm
				}(),
			},
		}
		listenerAnys[i], _ = ptypes.MarshalAny(listeners[i])
	}
	for i := range rdsTargets {
		routes[i] = &v3routepb.RouteConfiguration{
			Name: rdsTargets[i],
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{ldsTargets[i]},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: ""}},
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: cdsTargets[i]},
						},
					},
				}},
			}},
		}
		routeAnys[i], _ = ptypes.MarshalAny(routes[i])
	}
	for i := range cdsTargets {
		clusters[i] = &v3clusterpb.Cluster{
			Name:                 cdsTargets[i],
			ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
			EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
				EdsConfig: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
						Ads: &v3corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: edsTargets[i],
			},
			LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		}
		clusterAnys[i], _ = ptypes.MarshalAny(clusters[i])
	}
	for i := range edsTargets {
		endpoints[i] = &v3endpointpb.ClusterLoadAssignment{
			ClusterName: edsTargets[i],
			Endpoints: []*v3endpointpb.LocalityLbEndpoints{{
				Locality: &v3corepb.Locality{Region: edsTargets[i]},
				LbEndpoints: []*v3endpointpb.LbEndpoint{{
					HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{Endpoint: &v3endpointpb.Endpoint{
						Address: &v3corepb.Address{Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Protocol:      v3corepb.SocketAddress_TCP,
								Address:       ips[i],
								PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: ports[i]},
							},
						}},
					}},
				}},
				Priority: 0,
			}},
		}
		endpointAnys[i], _ = ptypes.MarshalAny(endpoints[i])
	}
}

func TestCSDS(t *testing.T) {
	xdsC, mgmServer, nodeID, stream, cleanup := commonSetup(t)
	defer cleanup()

	const (
		retryCount = 10
	)

	for _, target := range ldsTargets {
		xdsC.WatchListener(target, func(client.ListenerUpdate, error) {})
	}
	for _, target := range rdsTargets {
		xdsC.WatchRouteConfig(target, func(client.RouteConfigUpdate, error) {})
	}
	for _, target := range cdsTargets {
		xdsC.WatchCluster(target, func(client.ClusterUpdate, error) {})
	}
	for _, target := range edsTargets {
		xdsC.WatchEndpoints(target, func(client.EndpointsUpdate, error) {})
	}
	// time.Sleep(time.Second)

	for i := 0; i < retryCount; i++ {
		// Verify that status is REQUESTED.
		err := checkForRequested(stream)
		if err == nil {
			break
		}
		if i == retryCount-1 {
			t.Fatalf("%v", err)
		}
		time.Sleep(time.Millisecond * 100)
	}

	if err := mgmServer.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}); err != nil {
		t.Error(err)
	}

	// FIXME: how to not sleep? Loop?
	// time.Sleep(time.Second)

	for i := 0; i < retryCount; i++ {
		// Verify that status is REQUESTED.
		err := checkForACKed(stream)
		if err == nil {
			break
		}
		if i == retryCount-1 {
			t.Fatalf("%v", err)
		}
		time.Sleep(time.Millisecond * 100)
	}

	const nackResourceIdx = 0
	if err := mgmServer.Update(e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			{Name: ldsTargets[nackResourceIdx], ApiListener: &v3listenerpb.ApiListener{}}, // 0 will be nacked. 1 will stay the same.
		},
		Routes: []*v3routepb.RouteConfiguration{
			{Name: rdsTargets[nackResourceIdx], VirtualHosts: []*v3routepb.VirtualHost{{
				Routes: []*v3routepb.Route{{}},
			}}},
		},
		Clusters: []*v3clusterpb.Cluster{
			{Name: cdsTargets[nackResourceIdx], ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC}},
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			{ClusterName: edsTargets[nackResourceIdx], Endpoints: []*v3endpointpb.LocalityLbEndpoints{{}}},
		},
	}); err != nil {
		t.Error(err)
	}

	for i := 0; i < retryCount; i++ {
		// Verify that status is REQUESTED.
		err := checkForNACKed(nackResourceIdx, stream)
		if err == nil {
			break
		}
		if i == retryCount-1 {
			t.Fatalf("%v", err)
		}
		time.Sleep(time.Millisecond * 100)
	}

	// err := checkForNACKed(nackResourceIdx, stream)
	// if err != nil {
	// 	t.Fatalf("%v", err)
	// }

}

func commonSetup(t *testing.T) (xdsClientInterface, *e2e.ManagementServer, string, v3statuspb.ClientStatusDiscoveryService_StreamClientStatusClient, func()) {
	t.Helper()

	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	fs, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatal(err)
	}

	// Create a bootstrap file in a temporary directory.
	bootstrapCleanup, err := e2e.SetupBootstrapFile(e2e.BootstrapOptions{
		Version:              e2e.TransportV3,
		NodeID:               nodeID,
		ServerURI:            fs.Address,
		ServerResourceNameID: "grpc/server",
	})
	if err != nil {
		t.Fatal(err)
	}

	xdsC, err := client.New()
	if err != nil {
		t.Fatalf("failed to create xds client: %v", err)
	}

	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) {
		return xdsC, nil
	}

	// Initialize an gRPC server and register CSDS on it.
	server := grpc.NewServer()
	v3statuspb.RegisterClientStatusDiscoveryServiceServer(server, NewClientStatusDiscoveryServer())

	// Create a local listener and pass it to Serve().
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	// Create client.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}

	c := v3statuspb.NewClientStatusDiscoveryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	stream, err := c.StreamClientStatus(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("cannot get ServerReflectionInfo: %v", err)
	}

	return xdsC, fs, nodeID, stream, func() {
		fs.Stop()
		cancel()
		conn.Close()
		server.Stop()
		newXDSClient = oldNewXDSClient
		xdsC.Close()
		bootstrapCleanup()
	}
}

func checkForRequested(stream v3statuspb.ClientStatusDiscoveryService_StreamClientStatusClient) error {
	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		return fmt.Errorf("failed to recv response: %v", err)
	}
	fmt.Println(protoToJSON(r))
	fmt.Println(" ------- ")

	if n := len(r.Config); n != 1 {
		return fmt.Errorf("got %d configs, want 1: %v", n, proto.MarshalTextString(r))
	}
	if n := len(r.Config[0].XdsConfig); n != 4 {
		return fmt.Errorf("got %d xds configs (one for each type), want 4: %v", n, proto.MarshalTextString(r))
	}
	for _, cfg := range r.Config[0].XdsConfig {
		switch config := cfg.PerXdsConfig.(type) {
		case *v3statuspb.PerXdsConfig_ListenerConfig:
			var wantLis []*v3adminpb.ListenersConfigDump_DynamicListener
			for i := range ldsTargets {
				wantLis = append(wantLis, &v3adminpb.ListenersConfigDump_DynamicListener{
					Name:         ldsTargets[i],
					ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
				})
			}
			wantDump := &v3adminpb.ListenersConfigDump{
				DynamicListeners: wantLis,
			}
			if diff := cmp.Diff(config.ListenerConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_RouteConfig:
			var wantRoutes []*v3adminpb.RoutesConfigDump_DynamicRouteConfig
			for range rdsTargets {
				wantRoutes = append(wantRoutes, &v3adminpb.RoutesConfigDump_DynamicRouteConfig{
					ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
				})
			}
			wantDump := &v3adminpb.RoutesConfigDump{
				DynamicRouteConfigs: wantRoutes,
			}
			if diff := cmp.Diff(config.RouteConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_ClusterConfig:
			var wantCluster []*v3adminpb.ClustersConfigDump_DynamicCluster
			for range cdsTargets {
				wantCluster = append(wantCluster, &v3adminpb.ClustersConfigDump_DynamicCluster{
					ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
				})
			}
			wantDump := &v3adminpb.ClustersConfigDump{
				DynamicActiveClusters: wantCluster,
			}
			if diff := cmp.Diff(config.ClusterConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_EndpointConfig:
			var wantEndpoint []*v3adminpb.EndpointsConfigDump_DynamicEndpointConfig
			for range cdsTargets {
				wantEndpoint = append(wantEndpoint, &v3adminpb.EndpointsConfigDump_DynamicEndpointConfig{
					ClientStatus: v3adminpb.ClientResourceStatus_REQUESTED,
				})
			}
			wantDump := &v3adminpb.EndpointsConfigDump{
				DynamicEndpointConfigs: wantEndpoint,
			}
			if diff := cmp.Diff(config.EndpointConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		default:
			return fmt.Errorf("unexpected PerXdsConfig: %+v; %v", cfg.PerXdsConfig, protoToJSON(r))
		}
	}
	return nil
}

func checkForACKed(stream v3statuspb.ClientStatusDiscoveryService_StreamClientStatusClient) error {
	const (
		wantVersion = "1"
	)

	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		return fmt.Errorf("failed to send: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		return fmt.Errorf("failed to recv response: %v", err)
	}
	fmt.Println(protoToJSON(r))
	fmt.Println(" ------- ")

	if n := len(r.Config); n != 1 {
		return fmt.Errorf("got %d configs, want 1: %v", n, proto.MarshalTextString(r))
	}
	if n := len(r.Config[0].XdsConfig); n != 4 {
		return fmt.Errorf("got %d xds configs (one for each type), want 4: %v", n, proto.MarshalTextString(r))
	}
	for _, cfg := range r.Config[0].XdsConfig {

		switch config := cfg.PerXdsConfig.(type) {
		case *v3statuspb.PerXdsConfig_ListenerConfig:
			var wantLis []*v3adminpb.ListenersConfigDump_DynamicListener
			for i := range ldsTargets {
				wantLis = append(wantLis, &v3adminpb.ListenersConfigDump_DynamicListener{
					Name: ldsTargets[i],
					ActiveState: &v3adminpb.ListenersConfigDump_DynamicListenerState{
						VersionInfo: wantVersion,
						Listener:    listenerAnys[i],
						LastUpdated: nil,
					},
					ErrorState:   nil,
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				})
			}
			wantDump := &v3adminpb.ListenersConfigDump{
				VersionInfo:      wantVersion,
				DynamicListeners: wantLis,
			}
			if diff := cmp.Diff(config.ListenerConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_RouteConfig:
			var wantRoutes []*v3adminpb.RoutesConfigDump_DynamicRouteConfig
			for i := range rdsTargets {
				wantRoutes = append(wantRoutes, &v3adminpb.RoutesConfigDump_DynamicRouteConfig{
					VersionInfo:  wantVersion,
					RouteConfig:  routeAnys[i],
					LastUpdated:  nil,
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				})
			}
			wantDump := &v3adminpb.RoutesConfigDump{
				DynamicRouteConfigs: wantRoutes,
			}
			if diff := cmp.Diff(config.RouteConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_ClusterConfig:
			var wantCluster []*v3adminpb.ClustersConfigDump_DynamicCluster
			for i := range cdsTargets {
				wantCluster = append(wantCluster, &v3adminpb.ClustersConfigDump_DynamicCluster{
					VersionInfo:  wantVersion,
					Cluster:      clusterAnys[i],
					LastUpdated:  nil,
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				})
			}
			wantDump := &v3adminpb.ClustersConfigDump{
				VersionInfo:           wantVersion,
				DynamicActiveClusters: wantCluster,
			}
			if diff := cmp.Diff(config.ClusterConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_EndpointConfig:
			var wantEndpoint []*v3adminpb.EndpointsConfigDump_DynamicEndpointConfig
			for i := range cdsTargets {
				wantEndpoint = append(wantEndpoint, &v3adminpb.EndpointsConfigDump_DynamicEndpointConfig{
					VersionInfo:    wantVersion,
					EndpointConfig: endpointAnys[i],
					LastUpdated:    nil,
					ClientStatus:   v3adminpb.ClientResourceStatus_ACKED,
				})
			}
			wantDump := &v3adminpb.EndpointsConfigDump{
				DynamicEndpointConfigs: wantEndpoint,
			}
			if diff := cmp.Diff(config.EndpointConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		default:
			return fmt.Errorf("unexpected PerXdsConfig: %+v; %v", cfg.PerXdsConfig, protoToJSON(r))
		}
	}
	return nil
}

func checkForNACKed(nackResourceIdx int, stream v3statuspb.ClientStatusDiscoveryService_StreamClientStatusClient) error {

	const (
		ackVersion  = "1"
		nackVersion = "2"
	)

	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		return fmt.Errorf("failed to send: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		return fmt.Errorf("failed to recv response: %v", err)
	}
	fmt.Println(protoToJSON(r))
	fmt.Println(" ------- ")

	if n := len(r.Config); n != 1 {
		return fmt.Errorf("got %d configs, want 1: %v", n, proto.MarshalTextString(r))
	}
	if n := len(r.Config[0].XdsConfig); n != 4 {
		return fmt.Errorf("got %d xds configs (one for each type), want 4: %v", n, proto.MarshalTextString(r))
	}
	for _, cfg := range r.Config[0].XdsConfig {
		switch config := cfg.PerXdsConfig.(type) {
		case *v3statuspb.PerXdsConfig_ListenerConfig:
			var wantLis []*v3adminpb.ListenersConfigDump_DynamicListener
			for i := range ldsTargets {
				configDump := &v3adminpb.ListenersConfigDump_DynamicListener{
					Name: ldsTargets[i],
					ActiveState: &v3adminpb.ListenersConfigDump_DynamicListenerState{
						VersionInfo: ackVersion,
						Listener:    listenerAnys[i],
						LastUpdated: nil,
					},
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				}
				if i == nackResourceIdx {
					configDump.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
					configDump.ErrorState = &v3adminpb.UpdateFailureState{
						Details:     "blahblah",
						VersionInfo: nackVersion,
					}
				}
				wantLis = append(wantLis, configDump)
			}
			wantDump := &v3adminpb.ListenersConfigDump{
				VersionInfo:      nackVersion,
				DynamicListeners: wantLis,
			}
			if diff := cmp.Diff(config.ListenerConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_RouteConfig:
			var wantRoutes []*v3adminpb.RoutesConfigDump_DynamicRouteConfig
			for i := range rdsTargets {
				configDump := &v3adminpb.RoutesConfigDump_DynamicRouteConfig{
					VersionInfo:  ackVersion,
					RouteConfig:  routeAnys[i],
					LastUpdated:  nil,
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				}
				if i == nackResourceIdx {
					configDump.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
					configDump.ErrorState = &v3adminpb.UpdateFailureState{
						Details:     "blahblah",
						VersionInfo: nackVersion,
					}
				}
				wantRoutes = append(wantRoutes, configDump)
			}
			wantDump := &v3adminpb.RoutesConfigDump{
				DynamicRouteConfigs: wantRoutes,
			}
			if diff := cmp.Diff(config.RouteConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_ClusterConfig:
			var wantCluster []*v3adminpb.ClustersConfigDump_DynamicCluster
			for i := range cdsTargets {
				configDump := &v3adminpb.ClustersConfigDump_DynamicCluster{
					VersionInfo:  ackVersion,
					Cluster:      clusterAnys[i],
					LastUpdated:  nil,
					ClientStatus: v3adminpb.ClientResourceStatus_ACKED,
				}
				if i == nackResourceIdx {
					configDump.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
					configDump.ErrorState = &v3adminpb.UpdateFailureState{
						Details:     "blahblah",
						VersionInfo: nackVersion,
					}
				}
				wantCluster = append(wantCluster, configDump)
			}
			wantDump := &v3adminpb.ClustersConfigDump{
				VersionInfo:           nackVersion,
				DynamicActiveClusters: wantCluster,
			}
			if diff := cmp.Diff(config.ClusterConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		case *v3statuspb.PerXdsConfig_EndpointConfig:
			var wantEndpoint []*v3adminpb.EndpointsConfigDump_DynamicEndpointConfig
			for i := range cdsTargets {
				configDump := &v3adminpb.EndpointsConfigDump_DynamicEndpointConfig{
					VersionInfo:    ackVersion,
					EndpointConfig: endpointAnys[i],
					LastUpdated:    nil,
					ClientStatus:   v3adminpb.ClientResourceStatus_ACKED,
				}
				if i == nackResourceIdx {
					configDump.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
					configDump.ErrorState = &v3adminpb.UpdateFailureState{
						Details:     "blahblah",
						VersionInfo: nackVersion,
					}
				}
				wantEndpoint = append(wantEndpoint, configDump)
			}
			wantDump := &v3adminpb.EndpointsConfigDump{
				DynamicEndpointConfigs: wantEndpoint,
			}
			if diff := cmp.Diff(config.EndpointConfig, wantDump, cmpOpts); diff != "" {
				return fmt.Errorf(diff)
			}
		default:
			return fmt.Errorf("unexpected PerXdsConfig: %+v; %v", cfg.PerXdsConfig, protoToJSON(r))
		}

	}
	return nil
}

func protoToJSON(p proto.Message) string {
	mm := jsonpb.Marshaler{
		Indent: "  ",
	}
	ret, _ := mm.MarshalToString(p)
	return ret
}
