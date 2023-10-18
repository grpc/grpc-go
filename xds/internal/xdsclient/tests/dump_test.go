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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
)

func compareDump(ctx context.Context, client xdsclient.XDSClient, want map[string]map[string]xdsresource.UpdateWithMD) error {
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Timeout when waiting for expected dump: %v", lastErr)
		}
		cmpOpts := cmp.Options{
			cmpopts.EquateEmpty(),
			cmp.Comparer(func(a, b time.Time) bool { return true }),
			cmpopts.EquateErrors(),
			protocmp.Transform(),
		}
		diff := cmp.Diff(want, client.DumpResources(), cmpOpts)
		if diff == "" {
			return nil
		}
		lastErr = fmt.Errorf("DumpResources() returned unexpected dump, diff (-want +got):\n%s", diff)
		time.Sleep(100 * time.Millisecond)
	}
}

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
	client, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Dump resources and expect empty configs.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := compareDump(ctx, client, nil); err != nil {
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
	want := map[string]map[string]xdsresource.UpdateWithMD{
		"type.googleapis.com/envoy.config.listener.v3.Listener": {
			ldsTargets[0]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
			ldsTargets[1]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
		},
		"type.googleapis.com/envoy.config.route.v3.RouteConfiguration": {
			rdsTargets[0]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
			rdsTargets[1]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
		},
		"type.googleapis.com/envoy.config.cluster.v3.Cluster": {
			cdsTargets[0]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
			cdsTargets[1]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
		},
		"type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment": {
			edsTargets[0]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
			edsTargets[1]: {MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested}},
		},
	}
	if err := compareDump(ctx, client, want); err != nil {
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
	want = map[string]map[string]xdsresource.UpdateWithMD{
		"type.googleapis.com/envoy.config.listener.v3.Listener": {
			ldsTargets[0]: {Raw: listenerAnys[0], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
			ldsTargets[1]: {Raw: listenerAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
		},
		"type.googleapis.com/envoy.config.route.v3.RouteConfiguration": {
			rdsTargets[0]: {Raw: routeAnys[0], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
			rdsTargets[1]: {Raw: routeAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
		},
		"type.googleapis.com/envoy.config.cluster.v3.Cluster": {
			cdsTargets[0]: {Raw: clusterAnys[0], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
			cdsTargets[1]: {Raw: clusterAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
		},
		"type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment": {
			edsTargets[0]: {Raw: endpointAnys[0], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
			edsTargets[1]: {Raw: endpointAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "1"}},
		},
	}
	if err := compareDump(ctx, client, want); err != nil {
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

	// Verify that the xDS client reports the first resource of each type as
	// being in "NACKed" state, and the second resource of each type to be in
	// "ACKed" state. The version for the ACKed resource would be "2", while
	// that for the NACKed resource would be "1". In the NACKed resource, the
	// version which is NACKed is stored in the ErrorState field.
	want = map[string]map[string]xdsresource.UpdateWithMD{
		"type.googleapis.com/envoy.config.listener.v3.Listener": {
			ldsTargets[0]: {
				Raw: listenerAnys[0],
				MD: xdsresource.UpdateMetadata{
					Status:   xdsresource.ServiceStatusNACKed,
					Version:  "1",
					ErrState: &xdsresource.UpdateErrorMetadata{Version: "2", Err: cmpopts.AnyError},
				},
			},
			ldsTargets[1]: {Raw: listenerAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "2"}},
		},
		"type.googleapis.com/envoy.config.route.v3.RouteConfiguration": {
			rdsTargets[0]: {
				Raw: routeAnys[0],
				MD: xdsresource.UpdateMetadata{
					Status:   xdsresource.ServiceStatusNACKed,
					Version:  "1",
					ErrState: &xdsresource.UpdateErrorMetadata{Version: "2", Err: cmpopts.AnyError},
				},
			},
			rdsTargets[1]: {Raw: routeAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "2"}},
		},
		"type.googleapis.com/envoy.config.cluster.v3.Cluster": {
			cdsTargets[0]: {
				Raw: clusterAnys[0],
				MD: xdsresource.UpdateMetadata{
					Status:   xdsresource.ServiceStatusNACKed,
					Version:  "1",
					ErrState: &xdsresource.UpdateErrorMetadata{Version: "2", Err: cmpopts.AnyError},
				},
			},
			cdsTargets[1]: {Raw: clusterAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "2"}},
		},
		"type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment": {
			edsTargets[0]: {
				Raw: endpointAnys[0],
				MD: xdsresource.UpdateMetadata{
					Status:   xdsresource.ServiceStatusNACKed,
					Version:  "1",
					ErrState: &xdsresource.UpdateErrorMetadata{Version: "2", Err: cmpopts.AnyError},
				},
			},
			edsTargets[1]: {Raw: endpointAnys[1], MD: xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusACKed, Version: "2"}},
		},
	}
	if err := compareDump(ctx, client, want); err != nil {
		t.Fatal(err)
	}
}
