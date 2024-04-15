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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/stats"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/structpb"
)

const serviceNameKey = "service_name"
const serviceNamespaceKey = "service_namespace"
const serviceNameValue = "grpc-service"
const serviceNamespaceValue = "grpc-service-namespace"

// TestTelemetryLabels tests that telemetry labels from CDS make their way to
// the stats handler. The stats handler sets the mutable context value that the
// cluster impl picker will write telemetry labels to, and then the stats
// handler asserts that subsequent HandleRPC calls from the RPC lifecycle
// contain telemetry labels that it can see.
func (s) TestTelemetryLabels(t *testing.T) {
	managementServer, nodeID, _, resolver, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()

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

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", xdsServiceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver), grpc.WithStatsHandler(fsh))
	if err != nil {
		t.Fatalf("failed to create a new client to local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

type fakeStatsHandler struct {
	labels *istats.Labels

	t *testing.T
}

func (fsh *fakeStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (fsh *fakeStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

func (fsh *fakeStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	labels := &istats.Labels{
		TelemetryLabels: make(map[string]string),
	}
	fsh.labels = labels
	ctx = istats.SetLabels(ctx, labels) // ctx passed is immutable, however cluster_impl writes to the map of Telemetry Labels on the heap.
	return ctx
}

func (fsh *fakeStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	switch rs.(type) {
	// stats.Begin won't get Telemetry Labels because happens after picker
	// picks.

	// These three stats callouts trigger all metrics for OpenTelemetry that
	// aren't started. All of these should have access to the desired telemetry
	// labels.
	case *stats.OutPayload, *stats.InPayload, *stats.End:
		if label, ok := fsh.labels.TelemetryLabels[serviceNameKey]; !ok || label != serviceNameValue {
			fsh.t.Fatalf("for telemetry label %v, want: %v, got: %v", serviceNameKey, serviceNameValue, label)
		}
		if label, ok := fsh.labels.TelemetryLabels[serviceNamespaceKey]; !ok || label != serviceNamespaceValue {
			fsh.t.Fatalf("for telemetry label %v, want: %v, got: %v", serviceNamespaceKey, serviceNamespaceValue, label)
		}

	default:
		// Nothing to assert for the other stats.Handler callouts.
	}

}
