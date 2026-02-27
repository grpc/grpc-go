/*
 *
 * Copyright 2026 gRPC authors.
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

package interop

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/resolver"
	"google.golang.org/protobuf/testing/protocmp"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 30 * time.Second

type orcaServer struct {
	*stubserver.StubServer
	metricsRecorder orca.ServerMetricsRecorder
}

func startORCAServer(t *testing.T) *orcaServer {
	t.Helper()

	smr := orca.NewServerMetricsRecorder()
	ts := NewTestServer(NewTestServerOptions{MetricsRecorder: smr})

	stub := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return ts.UnaryCall(ctx, in)
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			return ts.FullDuplexCall(stream)
		},
	}

	sopts := []grpc.ServerOption{orca.CallMetricsServerOption(nil)}

	oso := orca.ServiceOptions{
		ServerMetricsProvider: smr,
		MinReportingInterval:  time.Second,
	}
	internal.ORCAAllowAnyMinReportingInterval.(func(so *orca.ServiceOptions))(&oso)
	sopts = append(sopts, stubserver.RegisterServiceServerOption(func(s grpc.ServiceRegistrar) {
		if err := orca.Register(s, oso); err != nil {
			t.Fatalf("Failed to register ORCA service: %v", err)
		}
	}))

	if err := stub.StartServer(sopts...); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	t.Cleanup(stub.Stop)

	return &orcaServer{
		StubServer:      stub,
		metricsRecorder: smr,
	}
}

func orcaSvcConfig() string {
	return `{"loadBalancingConfig": [{"test_backend_metrics_load_balancer": {}}]}`
}

// TestORCAPerRPCReport verifies that per-call ORCA load reports flow from the
// server through the orcaPicker's Done callback into the context.
func (s) TestORCAPerRPCReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startORCAServer(t)
	if err := srv.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	orcaRes := &v3orcapb.OrcaLoadReport{}
	_, err := srv.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{
		OrcaPerQueryReport: &testpb.TestOrcaReport{
			CpuUtilization:    0.8210,
			MemoryUtilization: 0.5847,
			RequestCost:       map[string]float64{"cost": 3456.32},
			Utilization:       map[string]float64{"util": 0.30499},
		},
	})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}

	want := &v3orcapb.OrcaLoadReport{
		CpuUtilization: 0.8210,
		MemUtilization: 0.5847,
		RequestCost:    map[string]float64{"cost": 3456.32},
		Utilization:    map[string]float64{"util": 0.30499},
	}
	if diff := cmp.Diff(want, orcaRes, protocmp.Transform()); diff != "" {
		t.Fatalf("Per-RPC ORCA load report mismatch (-want +got):\n%s", diff)
	}
}

// TestORCAOOBReport verifies that OOB ORCA load reports flow through
// OnLoadReport and are returned by the picker when no per-call report is present.
func (s) TestORCAOOBReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startORCAServer(t)
	if err := srv.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	stream, err := srv.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		OrcaOobReport: &testpb.TestOrcaReport{
			CpuUtilization:    0.8210,
			MemoryUtilization: 0.5847,
			Utilization:       map[string]float64{"util": 0.30499},
		},
		ResponseParameters: []*testpb.ResponseParameters{{Size: 1}},
	}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}

	want := &v3orcapb.OrcaLoadReport{
		CpuUtilization: 0.8210,
		MemUtilization: 0.5847,
		Utilization:    map[string]float64{"util": 0.30499},
	}

	pollORCAResult(ctx, t, srv.Client, want)

	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		OrcaOobReport: &testpb.TestOrcaReport{
			CpuUtilization:    0.29309,
			MemoryUtilization: 0.2,
			Utilization:       map[string]float64{"util": 0.2039},
		},
		ResponseParameters: []*testpb.ResponseParameters{{Size: 1}},
	}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}

	want = &v3orcapb.OrcaLoadReport{
		CpuUtilization: 0.29309,
		MemUtilization: 0.2,
		Utilization:    map[string]float64{"util": 0.2039},
	}
	pollORCAResult(ctx, t, srv.Client, want)
}

// TestORCAOOBFallback verifies the fallback behavior: when a per-call report
// is present but has all zero fields, the picker uses the most recent OOB
// report instead.
func (s) TestORCAOOBFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startORCAServer(t)
	if err := srv.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	stream, err := srv.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		OrcaOobReport: &testpb.TestOrcaReport{
			CpuUtilization:    0.73,
			MemoryUtilization: 0.55,
			Utilization:       map[string]float64{"util": 0.42},
		},
		ResponseParameters: []*testpb.ResponseParameters{{Size: 1}},
	}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}

	oobWant := &v3orcapb.OrcaLoadReport{
		CpuUtilization: 0.73,
		MemUtilization: 0.55,
		Utilization:    map[string]float64{"util": 0.42},
	}
	pollORCAResult(ctx, t, srv.Client, oobWant)

	// OrcaPerQueryReport is nil: per-call report has all-zero fields,
	// triggering the OOB fallback path in orcaPicker.
	orcaRes := &v3orcapb.OrcaLoadReport{}
	_, err = srv.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{})
	if err != nil {
		t.Fatalf("UnaryCall failed: %v", err)
	}
	if diff := cmp.Diff(oobWant, orcaRes, protocmp.Transform()); diff != "" {
		t.Fatalf("ORCA load report with zero per-call mismatch (-want +got):\n%s", diff)
	}
}

// TestEndpoints_MultipleAddresses verifies client behavior when an endpoint has multiple addresses
// and the first is unreachable. It validates that the pick_first falls through the address list and
// connects via the first reachable address, so per-call ORCA reports flow correctly.
func (s) TestEndpoints_MultipleAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srv := startORCAServer(t)
	if err := srv.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	// First address is unreachable; pick_first falls through to the good one.
	srv.R.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{
				{Addr: "bad-address"},
				{Addr: srv.Address},
			}},
		},
	})

	// Transient errors while pick_first probes the bad address are expected.
	want := &v3orcapb.OrcaLoadReport{CpuUtilization: 0.42, MemUtilization: 0.21}
	for ctx.Err() == nil {
		orcaRes := &v3orcapb.OrcaLoadReport{}
		_, err := srv.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{
			OrcaPerQueryReport: &testpb.TestOrcaReport{
				CpuUtilization:    0.42,
				MemoryUtilization: 0.21,
			},
		})
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if diff := cmp.Diff(want, orcaRes, protocmp.Transform()); diff != "" {
			t.Fatalf("Per-RPC ORCA mismatch (-want +got):\n%s", diff)
		}
		return
	}
	t.Fatalf("timed out waiting for connection through good address")
}

// TestMultipleEndpoints_OOBListeners verifies that N endpoints produce N
// independent pick_first instances and N independent OOB listeners, each
// receiving reports from its respective server.
func (s) TestMultipleEndpoints_OOBListeners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srvA := startORCAServer(t)
	srvB := startORCAServer(t)

	srvA.metricsRecorder.SetCPUUtilization(0.1)
	srvB.metricsRecorder.SetCPUUtilization(0.9)

	if err := srvA.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	// One endpoint per server: endpointsharding creates two independent
	// pick_first children, each with its own OOB listener.
	srvA.R.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: srvA.Address}}},
			{Addresses: []resolver.Address{{Addr: srvB.Address}}},
		},
	})

	// No per-call report triggers OOB fallback. With two active listeners,
	// b.report alternates between servers; both values must eventually appear.
	wantA := &v3orcapb.OrcaLoadReport{CpuUtilization: 0.1}
	wantB := &v3orcapb.OrcaLoadReport{CpuUtilization: 0.9}
	seenA, seenB := false, false
	for ctx.Err() == nil && (!seenA || !seenB) {
		orcaRes := &v3orcapb.OrcaLoadReport{}
		_, err := srvA.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{})
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if diff := cmp.Diff(wantA, orcaRes, protocmp.Transform()); diff == "" {
			seenA = true
		} else if diff := cmp.Diff(wantB, orcaRes, protocmp.Transform()); diff == "" {
			seenB = true
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("timed out waiting for OOB reports from both endpoints; seenA=%v seenB=%v", seenA, seenB)
	}
}

// TestEndpointUpdate verifies client behavior in response to a resolver update
// that changes an endpoint's address. It ensures that the client disconnects
// from the old address and connects to the new address via pick_first. It
// also validates that the OOB listener for the old connection is stopped,
// and a new OOB listener is registered for the new connection once it becomes
// READY.
func (s) TestEndpointUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srvA := startORCAServer(t)
	srvB := startORCAServer(t)

	if err := srvA.StartClient(grpc.WithDefaultServiceConfig(orcaSvcConfig())); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}

	orcaRes := &v3orcapb.OrcaLoadReport{}
	if _, err := srvA.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{
		OrcaPerQueryReport: &testpb.TestOrcaReport{
			CpuUtilization:    0.11,
			MemoryUtilization: 0.22,
		},
	}); err != nil {
		t.Fatalf("UnaryCall to srvA failed: %v", err)
	}
	wantA := &v3orcapb.OrcaLoadReport{CpuUtilization: 0.11, MemUtilization: 0.22}
	if diff := cmp.Diff(wantA, orcaRes, protocmp.Transform()); diff != "" {
		t.Fatalf("ORCA from srvA mismatch (-want +got):\n%s", diff)
	}

	// Distinctive OOB metric on srvB to confirm the switch via OOB fallback.
	srvB.metricsRecorder.SetCPUUtilization(0.77)

	srvA.R.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: srvB.Address}}},
		},
	})

	// No per-call report forces OOB fallback. Once srvB's metric appears,
	// all three are confirmed: pick_first reconnected, new OOB listener
	// registered for srvB, old one for srvA stopped.
	wantB := &v3orcapb.OrcaLoadReport{CpuUtilization: 0.77}
	for ctx.Err() == nil {
		orcaRes := &v3orcapb.OrcaLoadReport{}
		_, err := srvA.Client.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{})
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if diff := cmp.Diff(wantB, orcaRes, protocmp.Transform()); diff == "" {
			return
		}
		t.Logf("ORCA after endpoint update = %v; want %v; retrying...", orcaRes, wantB)
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for srvB OOB report after endpoint update")
}

func pollORCAResult(ctx context.Context, t *testing.T, tc testgrpc.TestServiceClient, want *v3orcapb.OrcaLoadReport) {
	t.Helper()
	for ctx.Err() == nil {
		orcaRes := &v3orcapb.OrcaLoadReport{}
		if _, err := tc.UnaryCall(contextWithORCAResult(ctx, &orcaRes), &testpb.SimpleRequest{}); err != nil {
			t.Fatalf("UnaryCall failed: %v", err)
		}
		if diff := cmp.Diff(want, orcaRes, protocmp.Transform()); diff == "" {
			return
		}
		t.Logf("ORCA load report = %v; want %v; retrying...", orcaRes, want)
		time.Sleep(time.Second)
	}
	t.Fatalf("Timed out waiting for expected ORCA load report %v", want)
}
