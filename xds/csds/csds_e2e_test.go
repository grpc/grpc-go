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

package csds_test

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/csds"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	v3statuspbgrpc "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"

	_ "google.golang.org/grpc/xds/internal/httpfilter/router" // Register the router filter
)

const defaultTestTimeout = 5 * time.Second

var cmpOpts = cmp.Options{
	cmp.Transformer("sort", func(in []*v3statuspb.ClientConfig_GenericXdsConfig) []*v3statuspb.ClientConfig_GenericXdsConfig {
		out := append([]*v3statuspb.ClientConfig_GenericXdsConfig(nil), in...)
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if a == nil {
				return true
			}
			if b == nil {
				return false
			}
			if strings.Compare(a.TypeUrl, b.TypeUrl) == 0 {
				return strings.Compare(a.Name, b.Name) < 0
			}
			return strings.Compare(a.TypeUrl, b.TypeUrl) < 0
		})
		return out
	}),
	protocmp.Transform(),
	protocmp.IgnoreFields((*v3statuspb.ClientConfig_GenericXdsConfig)(nil), "last_updated"),
	protocmp.IgnoreFields((*v3adminpb.UpdateFailureState)(nil), "last_update_attempt", "details"),
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// The following watcher implementations are no-ops since we don't really care
// about the callback received by these watchers in the test. We only care
// whether CSDS reports the expected state.

type unimplementedListenerWatcher struct{}

func (unimplementedListenerWatcher) OnUpdate(*xdsresource.ListenerResourceData) {}
func (unimplementedListenerWatcher) OnError(error)                              {}
func (unimplementedListenerWatcher) OnResourceDoesNotExist()                    {}

type unimplementedRouteConfigWatcher struct{}

func (unimplementedRouteConfigWatcher) OnUpdate(*xdsresource.RouteConfigResourceData) {}
func (unimplementedRouteConfigWatcher) OnError(error)                                 {}
func (unimplementedRouteConfigWatcher) OnResourceDoesNotExist()                       {}

type unimplementedClusterWatcher struct{}

func (unimplementedClusterWatcher) OnUpdate(*xdsresource.ClusterResourceData) {}
func (unimplementedClusterWatcher) OnError(error)                             {}
func (unimplementedClusterWatcher) OnResourceDoesNotExist()                   {}

type unimplementedEndpointsWatcher struct{}

func (unimplementedEndpointsWatcher) OnUpdate(*xdsresource.EndpointsResourceData) {}
func (unimplementedEndpointsWatcher) OnError(error)                               {}
func (unimplementedEndpointsWatcher) OnResourceDoesNotExist()                     {}

func (s) TestCSDS(t *testing.T) {
	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Create a bootstrap file in a temporary directory.
	bootstrapCleanup, err := bootstrap.CreateFile(bootstrap.Options{
		NodeID:    nodeID,
		ServerURI: mgmtServer.Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrapCleanup()

	// Create an xDS client. This will end up using the same singleton as used
	// by the CSDS service.
	xdsC, close, err := xdsclient.New()
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Initialize an gRPC server and register CSDS on it.
	server := grpc.NewServer()
	csdss, err := csds.NewClientStatusDiscoveryServer()
	if err != nil {
		t.Fatal(err)
	}
	v3statuspbgrpc.RegisterClientStatusDiscoveryServiceServer(server, csdss)
	defer func() {
		server.Stop()
		csdss.Close()
	}()

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

	// Create a client to the CSDS server.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial CSDS server %q: %v", lis.Addr().String(), err)
	}
	c := v3statuspbgrpc.NewClientStatusDiscoveryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := c.StreamClientStatus(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("Failed to create a stream for CSDS: %v", err)
	}
	defer conn.Close()

	// Verify that the xDS client reports an empty config.
	if err := checkClientStatusResponse(stream, nil); err != nil {
		t.Fatal(err)
	}

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

	// Register watches on the xDS client for two resources of each type.
	for _, target := range ldsTargets {
		xdsresource.WatchListener(xdsC, target, unimplementedListenerWatcher{})
	}
	for _, target := range rdsTargets {
		xdsresource.WatchRouteConfig(xdsC, target, unimplementedRouteConfigWatcher{})
	}
	for _, target := range cdsTargets {
		xdsresource.WatchCluster(xdsC, target, unimplementedClusterWatcher{})
	}
	for _, target := range edsTargets {
		xdsresource.WatchEndpoints(xdsC, target, unimplementedEndpointsWatcher{})
	}

	// Verify that the xDS client reports the resources as being in "Requested"
	// state.
	want := []*v3statuspb.ClientConfig_GenericXdsConfig{}
	for i := range ldsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[i], "", v3adminpb.ClientResourceStatus_REQUESTED, nil))
	}
	for i := range rdsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.route.v3.RouteConfiguration", rdsTargets[i], "", v3adminpb.ClientResourceStatus_REQUESTED, nil))
	}
	for i := range cdsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.cluster.v3.Cluster", cdsTargets[i], "", v3adminpb.ClientResourceStatus_REQUESTED, nil))
	}
	for i := range edsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment", edsTargets[i], "", v3adminpb.ClientResourceStatus_REQUESTED, nil))
	}
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("Timeout when waiting for resources in \"Requested\" state: %v", err)
		}
		if err := checkClientStatusResponse(stream, want); err == nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	// Configure the management server with two resources of each type,
	// corresponding to the watches registered above.
	if err := mgmtServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: listeners,
		Routes:    routes,
		Clusters:  clusters,
		Endpoints: endpoints,
	}); err != nil {
		t.Fatal(err)
	}

	// Verify that the xDS client reports the resources as being in "ACKed"
	// state, and in version "1".
	want = nil
	for i := range ldsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[i], "1", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[i]))
	}
	for i := range rdsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.route.v3.RouteConfiguration", rdsTargets[i], "1", v3adminpb.ClientResourceStatus_ACKED, routeAnys[i]))
	}
	for i := range cdsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.cluster.v3.Cluster", cdsTargets[i], "1", v3adminpb.ClientResourceStatus_ACKED, clusterAnys[i]))
	}
	for i := range edsTargets {
		want = append(want, makeGenericXdsConfig("type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment", edsTargets[i], "1", v3adminpb.ClientResourceStatus_ACKED, endpointAnys[i]))
	}
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("Timeout when waiting for resources in \"ACKed\" state: %v", err)
		}
		err := checkClientStatusResponse(stream, want)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
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

	// Verify that the xDS client reports the first resource of each type as
	// being in "NACKed" state, and the second resource of each type to be in
	// "ACKed" state. The version for the ACKed resource would be "2", while
	// that for the NACKed resource would be "1". In the NACKed resource, the
	// version which is NACKed is stored in the ErrorState field.
	want = nil
	for i := range ldsTargets {
		config := makeGenericXdsConfig("type.googleapis.com/envoy.config.listener.v3.Listener", ldsTargets[i], "2", v3adminpb.ClientResourceStatus_ACKED, listenerAnys[i])
		if i == nackResourceIdx {
			config.VersionInfo = "1"
			config.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
			config.ErrorState = &v3adminpb.UpdateFailureState{VersionInfo: "2"}
		}
		want = append(want, config)
	}
	for i := range rdsTargets {
		config := makeGenericXdsConfig("type.googleapis.com/envoy.config.route.v3.RouteConfiguration", rdsTargets[i], "2", v3adminpb.ClientResourceStatus_ACKED, routeAnys[i])
		if i == nackResourceIdx {
			config.VersionInfo = "1"
			config.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
			config.ErrorState = &v3adminpb.UpdateFailureState{VersionInfo: "2"}
		}
		want = append(want, config)
	}
	for i := range cdsTargets {
		config := makeGenericXdsConfig("type.googleapis.com/envoy.config.cluster.v3.Cluster", cdsTargets[i], "2", v3adminpb.ClientResourceStatus_ACKED, clusterAnys[i])
		if i == nackResourceIdx {
			config.VersionInfo = "1"
			config.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
			config.ErrorState = &v3adminpb.UpdateFailureState{VersionInfo: "2"}
		}
		want = append(want, config)
	}
	for i := range edsTargets {
		config := makeGenericXdsConfig("type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment", edsTargets[i], "2", v3adminpb.ClientResourceStatus_ACKED, endpointAnys[i])
		if i == nackResourceIdx {
			config.VersionInfo = "1"
			config.ClientStatus = v3adminpb.ClientResourceStatus_NACKED
			config.ErrorState = &v3adminpb.UpdateFailureState{VersionInfo: "2"}
		}
		want = append(want, config)
	}
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("Timeout when waiting for resources in \"NACKed\" state: %v", err)
		}
		err := checkClientStatusResponse(stream, want)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}
}

func makeGenericXdsConfig(typeURL, name, version string, status v3adminpb.ClientResourceStatus, config *anypb.Any) *v3statuspb.ClientConfig_GenericXdsConfig {
	return &v3statuspb.ClientConfig_GenericXdsConfig{
		TypeUrl:      typeURL,
		Name:         name,
		VersionInfo:  version,
		ClientStatus: status,
		XdsConfig:    config,
	}
}

func checkClientStatusResponse(stream v3statuspbgrpc.ClientStatusDiscoveryService_StreamClientStatusClient, want []*v3statuspb.ClientConfig_GenericXdsConfig) error {
	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		if err != io.EOF {
			return fmt.Errorf("failed to send ClientStatusRequest: %v", err)
		}
		// If the stream has closed, we call Recv() until it returns a non-nil
		// error to get the actual error on the stream.
		for {
			if _, err := stream.Recv(); err != nil {
				return fmt.Errorf("failed to recv ClientStatusResponse: %v", err)
			}
		}
	}
	resp, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to recv ClientStatusResponse: %v", err)
	}

	if n := len(resp.Config); n != 1 {
		return fmt.Errorf("got %d configs, want 1: %v", n, prototext.Format(resp))
	}

	if diff := cmp.Diff(resp.Config[0].GenericXdsConfigs, want, cmpOpts); diff != "" {
		return fmt.Errorf(diff)
	}
	return nil
}

func (s) TestCSDSNoXDSClient(t *testing.T) {
	// Create a bootstrap file in a temporary directory. Since we pass empty
	// options, it would end up creating a bootstrap file with an empty
	// serverURI which will fail xDS client creation.
	bootstrapCleanup, err := bootstrap.CreateFile(bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bootstrapCleanup() })

	// Initialize an gRPC server and register CSDS on it.
	server := grpc.NewServer()
	csdss, err := csds.NewClientStatusDiscoveryServer()
	if err != nil {
		t.Fatal(err)
	}
	defer csdss.Close()
	v3statuspbgrpc.RegisterClientStatusDiscoveryServiceServer(server, csdss)

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
	defer server.Stop()

	// Create a client to the CSDS server.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial CSDS server %q: %v", lis.Addr().String(), err)
	}
	defer conn.Close()
	c := v3statuspbgrpc.NewClientStatusDiscoveryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := c.StreamClientStatus(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("Failed to create a stream for CSDS: %v", err)
	}

	if err := stream.Send(&v3statuspb.ClientStatusRequest{Node: nil}); err != nil {
		t.Fatalf("Failed to send ClientStatusRequest: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("Failed to recv ClientStatusResponse: %v", err)
	}
	if n := len(r.Config); n != 0 {
		t.Fatalf("got %d configs, want 0: %v", n, prototext.Format(r))
	}
}
