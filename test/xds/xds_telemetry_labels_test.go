/*
 *
 * Copyright 2024 gRPC authors.
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
package xds_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/experimental/stats/telemetry"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
	"google.golang.org/protobuf/types/known/structpb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const serviceNameKey = "service_name"
const serviceNameKeyCSM = "csm.service_name"
const serviceNamespaceKey = "service_namespace"
const serviceNamespaceKeyCSM = "csm.service_namespace_name"
const serviceNameValue = "grpc-service"
const serviceNamespaceValue = "grpc-service-namespace"
const backendServiceKey = "grpc.lb.backend_service"
const backendServiceValue = "cluster-my-service-client-side-xds"
const localityKey = "grpc.lb.locality"
const localityValue = `{region="region-1", zone="zone-1", sub_zone="subzone-1"}`

// TestTelemetryLabels tests that telemetry labels from CDS make their way to
// the stats handler. The stats handler sets the mutable context value that the
// cluster impl picker will write telemetry labels to, and then the stats
// handler asserts that subsequent HandleRPC calls from the RPC lifecycle
// contain telemetry labels that it can see.
func (s) TestTelemetryLabels(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const xdsServiceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: xdsServiceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})

	resources.Clusters[0].Metadata = &v3corepb.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"com.google.csm.telemetry_labels": {
				Fields: map[string]*structpb.Value{
					serviceNameKey:      structpb.NewStringValue(serviceNameValue),
					serviceNamespaceKey: structpb.NewStringValue(serviceNamespaceValue),
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	fsh := &fakeStatsHandler{
		t: t,
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", xdsServiceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver), grpc.WithStatsHandler(fsh))
	if err != nil {
		t.Fatalf("failed to create a new client to local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// Tests that telemetry labels for an aggregate cluster hierarchy reflect the
// active leaf cluster receiving traffic, and correctly switch labels when
// failing over to a secondary leaf cluster.
func (s) TestTelemetryLabels_AggregateCluster(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	const (
		numServers     = 2
		xdsServiceName = "my-service-client-side-xds"
		cluster1Name   = "cluster-1"
		cluster2Name   = "cluster-2"

		csmName1 = "service-1"
		csmNs1   = "namespace-1"
		csmName2 = "service-2"
		csmNs2   = "namespace-2"
	)

	servers := make([]*stubserver.StubServer, numServers)
	for i := 0; i < numServers; i++ {
		servers[i] = stubserver.StartTestService(t, nil)
		defer servers[i].Stop()
	}

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(xdsServiceName, "route-"+xdsServiceName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig("route-"+xdsServiceName, xdsServiceName, xdsServiceName)},
		Clusters: []*v3clusterpb.Cluster{
			e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
				ClusterName: xdsServiceName,
				Type:        e2e.ClusterTypeAggregate,
				ChildNames:  []string{cluster1Name, cluster2Name},
			}),
			makeClusterResourceWithMetadata(cluster1Name, csmName1, csmNs1),
			makeClusterResourceWithMetadata(cluster2Name, csmName2, csmNs2),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint(cluster1Name, "localhost", []uint32{uint32(testutils.ParsePort(t, servers[0].Address))}),
			e2e.DefaultEndpoint(cluster2Name, "localhost", []uint32{uint32(testutils.ParsePort(t, servers[1].Address))}),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", xdsServiceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to create a new client to local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Make RPC to primary cluster and verify primary telemetry labels.
	peer := &peer.Peer{}
	var gotLabels map[string]string
	callCtx := telemetry.NewContextWithLabelCallback(ctx, func(l map[string]string) {
		gotLabels = l
	})
	if _, err := client.EmptyCall(callCtx, &testpb.Empty{}, grpc.Peer(peer)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peer.Addr.String(), servers[0].Address; got != want {
		t.Fatalf("EmptyCall() routed to %q, want %q", got, want)
	}

	wantLabels := map[string]string{
		localityKey:       localityValue,
		backendServiceKey: cluster1Name,
	}
	if diff := cmp.Diff(gotLabels, wantLabels); diff != "" {
		t.Fatalf("Telemetry labels for primary cluster (-got +want): %v", diff)
	}

	// Trigger failover to secondary cluster by clearing primary endpoints.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint(cluster1Name, "localhost", nil),
		e2e.DefaultEndpoint(cluster2Name, "localhost", []uint32{uint32(testutils.ParsePort(t, servers[1].Address))}),
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Make RPCs until traffic switches to secondary cluster and capture
	// secondary labels.
	for ctx.Err() == nil {
		callCtx := telemetry.NewContextWithLabelCallback(ctx, func(l map[string]string) {
			gotLabels = l
		})
		if _, err := client.EmptyCall(callCtx, &testpb.Empty{}, grpc.Peer(peer)); err == nil && peer.Addr.String() == servers[1].Address {
			break
		}
		time.Sleep(defaultTestShortTimeout)
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout waiting for RPCs to switch to secondary cluster %q", servers[1].Address)
	}

	wantLabels = map[string]string{
		localityKey:       localityValue,
		backendServiceKey: cluster2Name,
	}
	if diff := cmp.Diff(gotLabels, wantLabels); diff != "" {
		t.Fatalf("Telemetry labels after failover (-got +want): %v", diff)
	}
}

type fakeStatsHandler struct {
	labels map[string]string

	t *testing.T
}

func (fsh *fakeStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (fsh *fakeStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

func (fsh *fakeStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	fsh.labels = make(map[string]string)
	ctx = telemetry.NewContextWithLabelCallback(ctx, func(l map[string]string) {
		for k, v := range l {
			fsh.labels[k] = v
		}
	})
	return ctx
}

func (fsh *fakeStatsHandler) HandleRPC(_ context.Context, rs stats.RPCStats) {
	switch rs.(type) {
	// stats.Begin is called before the picker runs, so it won't have telemetry
	// labels.
	// The following three stats callouts trigger OpenTelemetry metrics and are
	// guaranteed to run after the picker has selected a subchannel. Therefore,
	// they should have access to the desired telemetry labels.
	case *stats.OutPayload, *stats.InPayload, *stats.End:
		want := map[string]string{
			serviceNameKeyCSM:      serviceNameValue,
			serviceNamespaceKeyCSM: serviceNamespaceValue,
			localityKey:            localityValue,
			backendServiceKey:      backendServiceValue,
		}
		if diff := cmp.Diff(fsh.labels, want); diff != "" {
			fsh.t.Fatalf("fsh.labels (-got +want): %v", diff)
		}
	default:
		// Nothing to assert for the other stats.Handler callouts.

	}
}

func makeClusterResourceWithMetadata(clusterName, serviceName, serviceNamespace string) *v3clusterpb.Cluster {
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeEDS,
	})
	cluster.Metadata = &v3corepb.Metadata{
		FilterMetadata: map[string]*structpb.Struct{
			"com.google.csm.telemetry_labels": {
				Fields: map[string]*structpb.Value{
					serviceNameKey:      structpb.NewStringValue(serviceName),
					serviceNamespaceKey: structpb.NewStringValue(serviceNamespace),
				},
			},
		},
	}
	return cluster
}
