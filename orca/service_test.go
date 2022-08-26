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

package orca_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/orca/internal"
	"google.golang.org/protobuf/types/known/durationpb"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	v3orcaservicegrpc "github.com/cncf/xds/go/xds/service/orca/v3"
	v3orcaservicepb "github.com/cncf/xds/go/xds/service/orca/v3"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const requestsMetricKey = "test-service-requests"

// An implementation of grpc_testing.TestService for the purpose of this test.
// We cannot use the StubServer approach here because we need to register the
// OpenRCAService as well on the same gRPC server.
type testServiceImpl struct {
	mu       sync.Mutex
	requests int64

	testgrpc.TestServiceServer
	orcaSrv *orca.Service
}

func (t *testServiceImpl) UnaryCall(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	t.mu.Lock()
	t.requests++
	t.mu.Unlock()

	t.orcaSrv.SetUtilization(requestsMetricKey, float64(t.requests))
	t.orcaSrv.SetCPUUtilization(50.0)
	t.orcaSrv.SetMemoryUtilization(99.0)
	return &testpb.SimpleResponse{}, nil
}

func (t *testServiceImpl) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	t.orcaSrv.DeleteUtilization(requestsMetricKey)
	t.orcaSrv.SetCPUUtilization(0)
	t.orcaSrv.SetMemoryUtilization(0)
	return &testpb.Empty{}, nil
}

// Test_E2E_CustomBackendMetrics_OutOfBand tests the injection of out-of-band
// custom backend metrics from the server application, and verifies that
// expected load reports are received at the client.
//
// TODO: Change this test to use the client API, when ready, to read the
// out-of-band metrics pushed by the server.
func (s) Test_E2E_CustomBackendMetrics_OutOfBand(t *testing.T) {
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatal(err)
	}

	// Override the min reporting interval in the internal package.
	const shortReportingInterval = 100 * time.Millisecond
	opts := orca.ServiceOptions{MinReportingInterval: shortReportingInterval}
	internal.AllowAnyMinReportingInterval.(func(*orca.ServiceOptions))(&opts)

	// Register the OpenRCAService with a very short metrics reporting interval.
	s := grpc.NewServer()
	orcaSrv, err := orca.Register(s, opts)
	if err != nil {
		t.Fatalf("orca.EnableOutOfBandMetricsReportingForTesting() failed: %v", err)
	}

	// Register the test service implementation on the same grpc server, and start serving.
	testpb.RegisterTestServiceServer(s, &testServiceImpl{orcaSrv: orcaSrv})
	go s.Serve(lis)
	defer s.Stop()
	t.Logf("Started gRPC server at %s...", lis.Addr().String())

	// Dial the test server.
	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%s) failed: %v", lis.Addr().String(), err)
	}
	defer cc.Close()

	// Spawn a goroutine which sends 100 unary RPCs to the test server. This
	// will trigger the injection of custom backend metrics from the
	// testServiceImpl.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testStub := testgrpc.NewTestServiceClient(cc)
	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if _, err := testStub.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
				errCh <- fmt.Errorf("UnaryCall failed: %v", err)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		errCh <- nil
	}()

	// Start the server streaming RPC to receive custom backend metrics.
	oobStub := v3orcaservicegrpc.NewOpenRcaServiceClient(cc)
	stream, err := oobStub.StreamCoreMetrics(ctx, &v3orcaservicepb.OrcaLoadReportRequest{ReportInterval: durationpb.New(shortReportingInterval)})
	if err != nil {
		t.Fatalf("Failed to create a stream for out-of-band metrics")
	}

	// Wait for the server to push metrics which indicate the completion of all
	// the unary RPCs made from the above goroutine.
	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for out-of-band custom backend metrics to match expected values")
		case err := <-errCh:
			if err != nil {
				t.Fatal(err)
			}
		default:
		}

		wantProto := &v3orcapb.OrcaLoadReport{
			CpuUtilization: 50.0,
			MemUtilization: 99.0,
			Utilization:    map[string]float64{requestsMetricKey: 100.0},
		}
		gotProto, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv() failed: %v", err)
		}
		if !cmp.Equal(gotProto, wantProto, cmp.Comparer(proto.Equal)) {
			t.Logf("Received load report from stream: %s, want: %s", pretty.ToJSON(gotProto), pretty.ToJSON(wantProto))
			continue
		}
		// This means that we received the metrics which we expected.
		break
	}

	// The EmptyCall RPC is expected to delete earlier injected metrics.
	if _, err := testStub.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}
	// Wait for the server to push empty metrics which indicate the processing
	// of the above EmptyCall RPC.
	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for out-of-band custom backend metrics to match expected values")
		default:
		}

		wantProto := &v3orcapb.OrcaLoadReport{}
		gotProto, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv() failed: %v", err)
		}
		if !cmp.Equal(gotProto, wantProto, cmp.Comparer(proto.Equal)) {
			t.Logf("Received load report from stream: %s, want: %s", pretty.ToJSON(gotProto), pretty.ToJSON(wantProto))
			continue
		}
		// This means that we received the metrics which we expected.
		break
	}
}
