// +build go1.12

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

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds"
	_ "google.golang.org/grpc/xds/internal/httpfilter/router"
	xtestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	v3statuspbgrpc "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

const (
	defaultTestTimeout = 10 * time.Second
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
			panic("failed to unmarshal RouteConfig" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.RouteConfig, &bt); err != nil {
			panic("failed to unmarshal RouteConfig" + err.Error())
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
			panic("failed to unmarshal Cluster" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.Cluster, &bt); err != nil {
			panic("failed to unmarshal Cluster" + err.Error())
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
			panic("failed to unmarshal Endpoints" + err.Error())
		}
		if err := ptypes.UnmarshalAny(b.EndpointConfig, &bt); err != nil {
			panic("failed to unmarshal Endpoints" + err.Error())
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
		listeners[i] = e2e.DefaultClientListener(ldsTargets[i], rdsTargets[i])
		listenerAnys[i] = testutils.MarshalAny(listeners[i])
	}
	for i := range rdsTargets {
		routes[i] = e2e.DefaultRouteConfig(rdsTargets[i], ldsTargets[i], cdsTargets[i])
		routeAnys[i] = testutils.MarshalAny(routes[i])
	}
	for i := range cdsTargets {
		clusters[i] = e2e.DefaultCluster(cdsTargets[i], edsTargets[i], e2e.SecurityLevelNone)
		clusterAnys[i] = testutils.MarshalAny(clusters[i])
	}
	for i := range edsTargets {
		endpoints[i] = e2e.DefaultEndpoint(edsTargets[i], ips[i], ports[i])
		endpointAnys[i] = testutils.MarshalAny(endpoints[i])
	}
}

func TestCSDS(t *testing.T) {
	const retryCount = 10

	xdsC, mgmServer, nodeID, stream, cleanup := commonSetup(t)
	defer cleanup()

	for _, target := range ldsTargets {
		xdsC.WatchListener(target, func(xdsclient.ListenerUpdate, error) {})
	}
	for _, target := range rdsTargets {
		xdsC.WatchRouteConfig(target, func(xdsclient.RouteConfigUpdate, error) {})
	}
	for _, target := range cdsTargets {
		xdsC.WatchCluster(target, func(xdsclient.ClusterUpdate, error) {})
	}
	for _, target := range edsTargets {
		xdsC.WatchEndpoints(target, func(xdsclient.EndpointsUpdate, error) {})
	}

	for i := 0; i < retryCount; i++ {
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
		t.Fatal(err)
	}
	for i := 0; i < retryCount; i++ {
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
		SkipValidation: true,
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < retryCount; i++ {
		err := checkForNACKed(nackResourceIdx, stream)
		if err == nil {
			break
		}
		if i == retryCount-1 {
			t.Fatalf("%v", err)
		}
		time.Sleep(time.Millisecond * 100)
	}
}

func commonSetup(t *testing.T) (xdsclient.XDSClient, *e2e.ManagementServer, string, v3statuspbgrpc.ClientStatusDiscoveryService_StreamClientStatusClient, func()) {
	t.Helper()

	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	fs, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatal(err)
	}

	// Create a bootstrap file in a temporary directory.
	bootstrapCleanup, err := xds.SetupBootstrapFile(xds.BootstrapOptions{
		Version:   xds.TransportV3,
		NodeID:    nodeID,
		ServerURI: fs.Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Create xds_client.
	xdsC, err := xdsclient.New()
	if err != nil {
		t.Fatalf("failed to create xds client: %v", err)
	}
	oldNewXDSClient := newXDSClient
	newXDSClient = func() xdsclient.XDSClient { return xdsC }

	// Initialize an gRPC server and register CSDS on it.
	server := grpc.NewServer()
	csdss, err := NewClientStatusDiscoveryServer()
	if err != nil {
		t.Fatal(err)
	}
	v3statuspbgrpc.RegisterClientStatusDiscoveryServiceServer(server, csdss)
	// Create a local listener and pass it to Serve().
	lis, err := xtestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xtestutils.LocalTCPListener() failed: %v", err)
	}
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	// Create CSDS client.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}
	c := v3statuspbgrpc.NewClientStatusDiscoveryServiceClient(conn)
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
		csdss.Close()
		newXDSClient = oldNewXDSClient
		xdsC.Close()
		bootstrapCleanup()
	}
}

func checkForRequested(stream v3statuspbgrpc.ClientStatusDiscoveryService_StreamClientStatusClient) error {
	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		return fmt.Errorf("failed to recv response: %v", err)
	}

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

func checkForACKed(stream v3statuspbgrpc.ClientStatusDiscoveryService_StreamClientStatusClient) error {
	const wantVersion = "1"

	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		return fmt.Errorf("failed to send: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		return fmt.Errorf("failed to recv response: %v", err)
	}

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

func checkForNACKed(nackResourceIdx int, stream v3statuspbgrpc.ClientStatusDiscoveryService_StreamClientStatusClient) error {
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

func TestCSDSNoXDSClient(t *testing.T) {
	oldNewXDSClient := newXDSClient
	newXDSClient = func() xdsclient.XDSClient { return nil }
	defer func() { newXDSClient = oldNewXDSClient }()

	// Initialize an gRPC server and register CSDS on it.
	server := grpc.NewServer()
	csdss, err := NewClientStatusDiscoveryServer()
	if err != nil {
		t.Fatal(err)
	}
	defer csdss.Close()
	v3statuspbgrpc.RegisterClientStatusDiscoveryServiceServer(server, csdss)
	// Create a local listener and pass it to Serve().
	lis, err := xtestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("xtestutils.LocalTCPListener() failed: %v", err)
	}
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()
	defer server.Stop()

	// Create CSDS client.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}
	defer conn.Close()
	c := v3statuspbgrpc.NewClientStatusDiscoveryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := c.StreamClientStatus(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("cannot get ServerReflectionInfo: %v", err)
	}

	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		t.Fatalf("failed to send: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("failed to recv response: %v", err)
	}
	if n := len(r.Config); n != 0 {
		t.Fatalf("got %d configs, want 0: %v", n, proto.MarshalTextString(r))
	}
}

func Test_nodeProtoToV3(t *testing.T) {
	const (
		testID      = "test-id"
		testCluster = "test-cluster"
		testZone    = "test-zone"
	)
	tests := []struct {
		name string
		n    proto.Message
		want *v3corepb.Node
	}{
		{
			name: "v3",
			n: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
			want: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
		},
		{
			name: "v2",
			n: &v2corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v2corepb.Locality{Zone: testZone},
			},
			want: &v3corepb.Node{
				Id:       testID,
				Cluster:  testCluster,
				Locality: &v3corepb.Locality{Zone: testZone},
			},
		},
		{
			name: "not node",
			n:    &v2corepb.Locality{Zone: testZone},
			want: nil, // Input is not a node, should return nil.
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeProtoToV3(tt.n)
			if diff := cmp.Diff(got, tt.want, protocmp.Transform()); diff != "" {
				t.Errorf("nodeProtoToV3() got unexpected result, diff (-got, +want): %v", diff)
			}
		})
	}
}
