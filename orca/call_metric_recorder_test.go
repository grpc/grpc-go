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
	"errors"
	"io"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/orca"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 5 * time.Second

// TestE2ECallMetricsUnary tests the injection of custom backend metrics from
// the server application for a unary RPC, and verifies that expected load
// reports are received at the client.
func (s) TestE2ECallMetricsUnary(t *testing.T) {
	tests := []struct {
		desc          string
		injectMetrics bool
		wantProto     *v3orcapb.OrcaLoadReport
		wantErr       error
	}{
		{
			desc:          "with custom backend metrics",
			injectMetrics: true,
			wantProto: &v3orcapb.OrcaLoadReport{
				CpuUtilization: 1.0,
				MemUtilization: 50.0,
				RequestCost:    map[string]float64{"queryCost": 25.0},
				Utilization:    map[string]float64{"queueSize": 75.0},
			},
		},
		{
			desc:          "with no custom backend metrics",
			injectMetrics: false,
			wantErr:       orca.ErrLoadReportMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// A server option to enables reporting of per-call backend metrics.
			callMetricsServerOption := orca.CallMetricsServerOption()

			// An interceptor to injects custom backend metrics, added only when
			// the injectMetrics field in the test is set.
			injectingInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
				recorder := orca.CallMetricRecorderFromContext(ctx)
				if recorder == nil {
					err := errors.New("Failed to retrieve per-RPC custom metrics recorder from the RPC context")
					t.Error(err)
					return nil, err
				}
				recorder.SetCPUUtilization(1.0)
				recorder.SetMemoryUtilization(50.0)
				// This value will be overwritten by a write to the same metric
				// from the server handler.
				recorder.SetUtilization("queueSize", 1.0)
				return handler(ctx, req)
			}

			// A stub server whose unary handler injects custom metrics, if the
			// injectMetrics field in the test is set. It overwrites one of the
			// values injected above, by the interceptor.
			srv := stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
					if !test.injectMetrics {
						return &testpb.Empty{}, nil
					}
					recorder := orca.CallMetricRecorderFromContext(ctx)
					if recorder == nil {
						err := errors.New("Failed to retrieve per-RPC custom metrics recorder from the RPC context")
						t.Error(err)
						return nil, err
					}
					recorder.SetRequestCost("queryCost", 25.0)
					recorder.SetUtilization("queueSize", 75.0)
					return &testpb.Empty{}, nil
				},
			}

			// Start the stub server with the appropriate server options.
			sopts := []grpc.ServerOption{callMetricsServerOption}
			if test.injectMetrics {
				sopts = append(sopts, grpc.ChainUnaryInterceptor(injectingInterceptor))
			}
			if err := srv.StartServer(sopts...); err != nil {
				t.Fatalf("Failed to start server: %v", err)
			}
			defer srv.Stop()

			// Dial the stub server.
			cc, err := grpc.Dial(srv.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial(%s) failed: %v", srv.Address, err)
			}
			defer cc.Close()

			// Make a unary RPC and expect the trailer metadata to contain the custom
			// backend metrics as an ORCA LoadReport protobuf message.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			client := testgrpc.NewTestServiceClient(cc)
			trailer := metadata.MD{}
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Trailer(&trailer)); err != nil {
				t.Fatalf("EmptyCall failed: %v", err)
			}

			gotProto, err := orca.ToLoadReport(trailer)
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("When retrieving load report, got error: %v, want: %v", err, orca.ErrLoadReportMissing)
			}
			if test.wantProto != nil && !cmp.Equal(gotProto, test.wantProto, cmp.Comparer(proto.Equal)) {
				t.Fatalf("Received load report in trailer: %s, want: %s", pretty.ToJSON(gotProto), pretty.ToJSON(test.wantProto))
			}
		})
	}
}

// TestE2ECallMetricsStreaming tests the injection of custom backend metrics
// from the server application for a streaming RPC, and verifies that expected
// load reports are received at the client.
func (s) TestE2ECallMetricsStreaming(t *testing.T) {
	tests := []struct {
		desc          string
		injectMetrics bool
		wantProto     *v3orcapb.OrcaLoadReport
		wantErr       error
	}{
		{
			desc:          "with custom backend metrics",
			injectMetrics: true,
			wantProto: &v3orcapb.OrcaLoadReport{
				CpuUtilization: 1.0,
				MemUtilization: 50.0,
				RequestCost:    map[string]float64{"queryCost": 25.0},
				Utilization:    map[string]float64{"queueSize": 75.0},
			},
		},
		{
			desc:          "with no custom backend metrics",
			injectMetrics: false,
			wantErr:       orca.ErrLoadReportMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// A server option to enables reporting of per-call backend metrics.
			callMetricsServerOption := orca.CallMetricsServerOption()

			// An interceptor which injects custom backend metrics, added only
			// when the injectMetrics field in the test is set.
			injectingInterceptor := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				recorder := orca.CallMetricRecorderFromContext(ss.Context())
				if recorder == nil {
					err := errors.New("Failed to retrieve per-RPC custom metrics recorder from the RPC context")
					t.Error(err)
					return err
				}
				recorder.SetCPUUtilization(1.0)
				recorder.SetMemoryUtilization(50.0)
				// This value will be overwritten by a write to the same metric
				// from the server handler.
				recorder.SetUtilization("queueSize", 1.0)
				return handler(srv, ss)
			}

			// A stub server whose streaming handler injects custom metrics, if
			// the injectMetrics field in the test is set. It overwrites one of
			// the values injected above, by the interceptor.
			srv := stubserver.StubServer{
				FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
					if test.injectMetrics {
						recorder := orca.CallMetricRecorderFromContext(stream.Context())
						if recorder == nil {
							err := errors.New("Failed to retrieve per-RPC custom metrics recorder from the RPC context")
							t.Error(err)
							return err
						}
						recorder.SetRequestCost("queryCost", 25.0)
						recorder.SetUtilization("queueSize", 75.0)
					}

					// Streaming implementation replies with a dummy response until the
					// client closes the stream (in which case it will see an io.EOF),
					// or an error occurs while reading/writing messages.
					for {
						_, err := stream.Recv()
						if err == io.EOF {
							return nil
						}
						if err != nil {
							return err
						}
						payload := &testpb.Payload{Body: make([]byte, 32)}
						if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: payload}); err != nil {
							return err
						}
					}
				},
			}

			// Start the stub server with the appropriate server options.
			sopts := []grpc.ServerOption{callMetricsServerOption}
			if test.injectMetrics {
				sopts = append(sopts, grpc.ChainStreamInterceptor(injectingInterceptor))
			}
			if err := srv.StartServer(sopts...); err != nil {
				t.Fatalf("Failed to start server: %v", err)
			}
			defer srv.Stop()

			// Dial the stub server.
			cc, err := grpc.Dial(srv.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("grpc.Dial(%s) failed: %v", srv.Address, err)
			}
			defer cc.Close()

			// Start the full duplex streaming RPC.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			tc := testgrpc.NewTestServiceClient(cc)
			stream, err := tc.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("FullDuplexCall failed: %v", err)
			}

			// Send one request to the server.
			payload := &testpb.Payload{Type: testpb.PayloadType_RANDOM, Body: make([]byte, 32)}
			req := &testpb.StreamingOutputCallRequest{Payload: payload}
			if err := stream.Send(req); err != nil {
				t.Fatalf("stream.Send() failed: %v", err)
			}
			// Read one reply from the server.
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("stream.Recv() failed: %v", err)
			}
			// Close the sending side.
			if err := stream.CloseSend(); err != nil {
				t.Fatalf("stream.CloseSend() failed: %v", err)
			}
			// Make sure it is safe to read the trailer.
			for {
				if _, err := stream.Recv(); err != nil {
					break
				}
			}

			gotProto, err := orca.ToLoadReport(stream.Trailer())
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("When retrieving load report, got error: %v, want: %v", err, orca.ErrLoadReportMissing)
			}
			if test.wantProto != nil && !cmp.Equal(gotProto, test.wantProto, cmp.Comparer(proto.Equal)) {
				t.Fatalf("Received load report in trailer: %s, want: %s", pretty.ToJSON(gotProto), pretty.ToJSON(test.wantProto))
			}
		})
	}
}
