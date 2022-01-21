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

package observability

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	configpb "google.golang.org/grpc/observability/config"
	"google.golang.org/grpc/observability/grpclogrecord"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	// OpenCensus, once included in binary, will spawn a global goroutine
	// recorder that is not controllable by application.
	// https://github.com/census-instrumentation/opencensus-go/issues/1191
	leakcheck.RegisterIgnoreGoroutine("go.opencensus.io/stats/view.(*worker).start")
	// google-cloud-go leaks HTTP client. They are aware of this:
	// https://github.com/googleapis/google-cloud-go/issues/1183
	leakcheck.RegisterIgnoreGoroutine("internal/poll.runtime_pollWait")
}

var (
	defaultTestTimeout  = 10 * time.Second
	testHeaderMetadata  = metadata.MD{"header": []string{"HeADer"}}
	testTrailerMetadata = metadata.MD{"trailer": []string{"TrAileR"}}
	testOkPayload       = []byte{72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100}
	testErrorPayload    = []byte{77, 97, 114, 116, 104, 97}
)

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *testServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	if err := grpc.SendHeader(ctx, testHeaderMetadata); err != nil {
		return nil, status.Errorf(status.Code(err), "grpc.SendHeader(_, %v) = %v, want <nil>", testHeaderMetadata, err)
	}
	if err := grpc.SetTrailer(ctx, testTrailerMetadata); err != nil {
		return nil, status.Errorf(status.Code(err), "grpc.SetTrailer(_, %v) = %v, want <nil>", testTrailerMetadata, err)
	}

	if bytes.Compare(in.Payload.Body, testErrorPayload) == 0 {
		return nil, fmt.Errorf("test case injected error")
	}

	return &testpb.SimpleResponse{Payload: in.Payload}, nil
}

func (s *testServer) FullDuplexCall(stream testgrpc.TestService_FullDuplexCallServer) error {
	if err := stream.SendHeader(testHeaderMetadata); err != nil {
		return status.Errorf(status.Code(err), "%v.SendHeader(%v) = %v, want %v", stream, testHeaderMetadata, err, nil)
	}
	stream.SetTrailer(testTrailerMetadata)
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			return err
		}

		if bytes.Compare(in.Payload.Body, testErrorPayload) == 0 {
			return fmt.Errorf("test case injected error")
		}

		if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: in.Payload}); err != nil {
			return err
		}
	}
}

type fakeLoggingExporter struct {
	t        *testing.T
	observed []*grpclogrecord.GrpcLogRecord
	isClosed bool
	mu       sync.Mutex
}

func (fle *fakeLoggingExporter) EmitGrpcLogRecord(l *grpclogrecord.GrpcLogRecord) {
	fle.mu.Lock()
	defer fle.mu.Unlock()
	fle.observed = append(fle.observed, l)
	eventJSON, _ := protojson.Marshal(l)
	fle.t.Logf("fakeLoggingExporter Emit: %s", eventJSON)
}

func (fle *fakeLoggingExporter) Close() error {
	fle.isClosed = true
	return nil
}

// test is an end-to-end test. It should be created with the newTest
// func, modified as needed, and then started with its startServer method.
// It should be cleaned up with the tearDown method.
type test struct {
	t   *testing.T
	fle *fakeLoggingExporter

	testServer testgrpc.TestServiceServer // nil means none
	// srv and srvAddr are set once startServer is called.
	srv     *grpc.Server
	srvAddr string

	cc *grpc.ClientConn // nil until requested via clientConn
}

func (te *test) tearDown() {
	if te.cc != nil {
		te.cc.Close()
		te.cc = nil
	}
	te.srv.Stop()
	End()

	if !te.fle.isClosed {
		te.t.Fatalf("fakeLoggingExporter not closed!")
	}
}

// newTest returns a new test using the provided testing.T and
// environment.  It is returned with default values. Tests should
// modify it before calling its startServer and clientConn methods.
func newTest(t *testing.T) *test {
	return &test{
		t:   t,
		fle: &fakeLoggingExporter{t: t},
	}
}

// startServer starts a gRPC server listening. Callers should defer a
// call to te.tearDown to clean up.
func (te *test) startServer(ts testgrpc.TestServiceServer) {
	te.testServer = ts
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		te.t.Fatalf("Failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	s := grpc.NewServer(opts...)
	te.srv = s
	if te.testServer != nil {
		testgrpc.RegisterTestServiceServer(s, te.testServer)
	}

	go s.Serve(lis)
	te.srvAddr = lis.Addr().String()
}

func (te *test) clientConn() *grpc.ClientConn {
	if te.cc != nil {
		return te.cc
	}
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
		grpc.WithUserAgent("test/0.0.1"),
	}
	var err error
	te.cc, err = grpc.Dial(te.srvAddr, opts...)
	if err != nil {
		te.t.Fatalf("Dial(%q) = %v", te.srvAddr, err)
	}
	return te.cc
}

func (te *test) enablePluginWithFakeExporters() {
	config := &configpb.ObservabilityConfig{
		Exporter: &configpb.ExporterConfig{
			DisableDefaultTracingExporter: true,
			DisableDefaultMetricsExporter: true,
			DisableDefaultLoggingExporter: true,
		},
	}
	configJSON, err := protojson.Marshal(config)
	if err != nil {
		te.t.Fatalf("failed to convert config to JSON: %v", err)
	}
	os.Setenv(envKeyObservabilityConfig, string(configJSON))
	Start()
	registerLoggingExporter(te.fle)
}

func checkGrpcCallStart(t *testing.T, seen *grpclogrecord.GrpcLogRecord, want *grpclogrecord.GrpcLogRecord) {
	if seen.EventType != grpclogrecord.EventType_GRPC_CALL_START {
		t.Fatalf("got %v, want GRPC_CALL_START", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	// TODO: find the ground truth about authority and IP addresses
}

func checkGrpcCallEnd(t *testing.T, seen *grpclogrecord.GrpcLogRecord, want *grpclogrecord.GrpcLogRecord) {
	if seen.EventType != grpclogrecord.EventType_GRPC_CALL_END {
		t.Fatalf("got %v, want GRPC_CALL_END", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	if seen.StatusCode != want.StatusCode {
		t.Fatalf("l.StatusCode = %v, want %v", seen.StatusCode, want.StatusCode)
	}
	if seen.StatusMessage != want.StatusMessage {
		t.Fatalf("l.StatusMessage = %v, want %v", seen.StatusMessage, want.StatusMessage)
	}
	if !bytes.Equal(seen.StatusDetails, want.StatusDetails) {
		t.Fatalf("l.StatusDetails = %v, want %v", seen.StatusDetails, want.StatusDetails)
	}
}

func (s) TestLoggingForUnaryCall(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	te.enablePluginWithFakeExporters()
	te.startServer(&testServer{})
	tc := testgrpc.NewTestServiceClient(te.clientConn())

	var (
		resp *testpb.SimpleResponse
		req  *testpb.SimpleRequest
		err  error
	)
	req = &testpb.SimpleRequest{Payload: &testpb.Payload{Body: testOkPayload}}
	tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	resp, err = tc.UnaryCall(metadata.NewOutgoingContext(tCtx, testHeaderMetadata), req)
	if err != nil {
		t.Fatalf("unary call failed: %v", err)
	}
	t.Logf("unary call passed: %v", resp)

	// Wait for the gRPC transport to gracefully close to ensure no lost event.
	te.cc.Close()
	te.srv.GracefulStop()

	if len(te.fle.observed) != 4 {
		t.Fatalf("expected 4 log entries, got %d", len(te.fle.observed))
	}
	checkGrpcCallStart(te.t, te.fle.observed[0], &grpclogrecord.GrpcLogRecord{
		EventLogger: grpclogrecord.EventLogger_LOGGER_CLIENT,
	})
	checkGrpcCallStart(te.t, te.fle.observed[1], &grpclogrecord.GrpcLogRecord{
		EventLogger: grpclogrecord.EventLogger_LOGGER_SERVER,
	})
	checkGrpcCallEnd(te.t, te.fle.observed[2], &grpclogrecord.GrpcLogRecord{
		EventLogger:   grpclogrecord.EventLogger_LOGGER_SERVER,
		StatusCode:    0,
		StatusMessage: "OK",
	})
	checkGrpcCallEnd(te.t, te.fle.observed[3], &grpclogrecord.GrpcLogRecord{
		EventLogger:   grpclogrecord.EventLogger_LOGGER_CLIENT,
		StatusCode:    0,
		StatusMessage: "OK",
	})
}
