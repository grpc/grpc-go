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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpclogrecordpb "google.golang.org/grpc/gcp/observability/internal/logging"
	"google.golang.org/grpc/internal"
	iblog "google.golang.org/grpc/internal/binarylog"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
	defaultTestTimeout        = 10 * time.Second
	testHeaderMetadata        = metadata.MD{"header": []string{"HeADer"}}
	testTrailerMetadata       = metadata.MD{"trailer": []string{"TrAileR"}}
	testOkPayload             = []byte{72, 101, 108, 108, 111, 32, 87, 111, 114, 108, 100}
	testErrorPayload          = []byte{77, 97, 114, 116, 104, 97}
	testErrorMessage          = "test case injected error"
	infinitySizeBytes   int32 = 1024 * 1024 * 1024
	defaultRequestCount       = 24
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

	if bytes.Equal(in.Payload.Body, testErrorPayload) {
		return nil, fmt.Errorf(testErrorMessage)
	}

	return &testpb.SimpleResponse{Payload: in.Payload}, nil
}

type fakeLoggingExporter struct {
	t            *testing.T
	clientEvents []*grpclogrecordpb.GrpcLogRecord
	serverEvents []*grpclogrecordpb.GrpcLogRecord
	isClosed     bool
	mu           sync.Mutex
}

func (fle *fakeLoggingExporter) EmitGrpcLogRecord(l *grpclogrecordpb.GrpcLogRecord) {
	fle.mu.Lock()
	defer fle.mu.Unlock()
	switch l.EventLogger {
	case grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT:
		fle.clientEvents = append(fle.clientEvents, l)
	case grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER:
		fle.serverEvents = append(fle.serverEvents, l)
	default:
		fle.t.Fatalf("unexpected event logger: %v", l.EventLogger)
	}
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

	if te.fle != nil && !te.fle.isClosed {
		te.t.Fatalf("fakeLoggingExporter not closed!")
	}
}

// newTest returns a new test using the provided testing.T and
// environment.  It is returned with default values. Tests should
// modify it before calling its startServer and clientConn methods.
func newTest(t *testing.T) *test {
	return &test{
		t: t,
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
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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

func (te *test) enablePluginWithConfig(config *config) {
	// Injects the fake exporter for testing purposes
	te.fle = &fakeLoggingExporter{t: te.t}
	defaultLogger = newBinaryLogger(nil)
	iblog.SetLogger(defaultLogger)
	if err := defaultLogger.start(config, te.fle); err != nil {
		te.t.Fatalf("Failed to start plugin: %v", err)
	}
}

func (te *test) enablePluginWithCaptureAll() {
	te.enablePluginWithConfig(&config{
		EnableCloudLogging:   true,
		DestinationProjectID: "fake",
		LogFilters: []logFilter{
			{
				Pattern:      "*",
				HeaderBytes:  infinitySizeBytes,
				MessageBytes: infinitySizeBytes,
			},
		},
	})
}

func (te *test) enableOpenCensus() {
	defaultMetricsReportingInterval = time.Millisecond * 100
	config := &config{
		EnableCloudLogging:      true,
		EnableCloudTrace:        true,
		EnableCloudMonitoring:   true,
		GlobalTraceSamplingRate: 1.0,
	}
	startOpenCensus(config)
}

func checkEventCommon(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord) {
	if seen.RpcId == "" {
		t.Fatalf("expect non-empty RpcId")
	}
	if seen.SequenceId == 0 {
		t.Fatalf("expect non-zero SequenceId")
	}
}

func checkEventRequestHeader(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord, want *grpclogrecordpb.GrpcLogRecord) {
	checkEventCommon(t, seen)
	if seen.EventType != grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_HEADER {
		t.Fatalf("got %v, want GrpcLogRecord_GRPC_CALL_REQUEST_HEADER", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	if want.Authority != "" && seen.Authority != want.Authority {
		t.Fatalf("l.Authority = %v, want %v", seen.Authority, want.Authority)
	}
	if want.ServiceName != "" && seen.ServiceName != want.ServiceName {
		t.Fatalf("l.ServiceName = %v, want %v", seen.ServiceName, want.ServiceName)
	}
	if want.MethodName != "" && seen.MethodName != want.MethodName {
		t.Fatalf("l.MethodName = %v, want %v", seen.MethodName, want.MethodName)
	}
	if len(seen.Metadata.Entry) != 1 {
		t.Fatalf("unexpected header size: %v != 1", len(seen.Metadata.Entry))
	}
	if seen.Metadata.Entry[0].Key != "header" {
		t.Fatalf("unexpected header: %v", seen.Metadata.Entry[0].Key)
	}
	if !bytes.Equal(seen.Metadata.Entry[0].Value, []byte(testHeaderMetadata["header"][0])) {
		t.Fatalf("unexpected header value: %v", seen.Metadata.Entry[0].Value)
	}
}

func checkEventResponseHeader(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord, want *grpclogrecordpb.GrpcLogRecord) {
	checkEventCommon(t, seen)
	if seen.EventType != grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_HEADER {
		t.Fatalf("got %v, want GrpcLogRecord_GRPC_CALL_RESPONSE_HEADER", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	if len(seen.Metadata.Entry) != 1 {
		t.Fatalf("unexpected header size: %v != 1", len(seen.Metadata.Entry))
	}
	if seen.Metadata.Entry[0].Key != "header" {
		t.Fatalf("unexpected header: %v", seen.Metadata.Entry[0].Key)
	}
	if !bytes.Equal(seen.Metadata.Entry[0].Value, []byte(testHeaderMetadata["header"][0])) {
		t.Fatalf("unexpected header value: %v", seen.Metadata.Entry[0].Value)
	}
}

func checkEventRequestMessage(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord, want *grpclogrecordpb.GrpcLogRecord, payload []byte) {
	checkEventCommon(t, seen)
	if seen.EventType != grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_MESSAGE {
		t.Fatalf("got %v, want GrpcLogRecord_GRPC_CALL_REQUEST_MESSAGE", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	msg := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: payload}}
	msgBytes, _ := proto.Marshal(msg)
	if !bytes.Equal(seen.Message, msgBytes) {
		t.Fatalf("unexpected payload: %v != %v", seen.Message, payload)
	}
}

func checkEventResponseMessage(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord, want *grpclogrecordpb.GrpcLogRecord, payload []byte) {
	checkEventCommon(t, seen)
	if seen.EventType != grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_MESSAGE {
		t.Fatalf("got %v, want GrpcLogRecord_GRPC_CALL_RESPONSE_MESSAGE", seen.EventType.String())
	}
	if seen.EventLogger != want.EventLogger {
		t.Fatalf("l.EventLogger = %v, want %v", seen.EventLogger, want.EventLogger)
	}
	msg := &testpb.SimpleResponse{Payload: &testpb.Payload{Body: payload}}
	msgBytes, _ := proto.Marshal(msg)
	if !bytes.Equal(seen.Message, msgBytes) {
		t.Fatalf("unexpected payload: %v != %v", seen.Message, payload)
	}
}

func checkEventTrailer(t *testing.T, seen *grpclogrecordpb.GrpcLogRecord, want *grpclogrecordpb.GrpcLogRecord) {
	checkEventCommon(t, seen)
	if seen.EventType != grpclogrecordpb.GrpcLogRecord_GRPC_CALL_TRAILER {
		t.Fatalf("got %v, want GrpcLogRecord_GRPC_CALL_TRAILER", seen.EventType.String())
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
	if len(seen.Metadata.Entry) != 1 {
		t.Fatalf("unexpected trailer size: %v != 1", len(seen.Metadata.Entry))
	}
	if seen.Metadata.Entry[0].Key != "trailer" {
		t.Fatalf("unexpected trailer: %v", seen.Metadata.Entry[0].Key)
	}
	if !bytes.Equal(seen.Metadata.Entry[0].Value, []byte(testTrailerMetadata["trailer"][0])) {
		t.Fatalf("unexpected trailer value: %v", seen.Metadata.Entry[0].Value)
	}
}

const (
	TypeOpenCensusViewDistribution string = "distribution"
	TypeOpenCensusViewCount               = "count"
	TypeOpenCensusViewSum                 = "sum"
	TypeOpenCensusViewLastValue           = "last_value"
)

type fakeOpenCensusExporter struct {
	// The map of the observed View name and type
	SeenViews map[string]string
	// Number of spans
	SeenSpans int

	t  *testing.T
	mu sync.RWMutex
}

func (fe *fakeOpenCensusExporter) ExportView(vd *view.Data) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	for _, row := range vd.Rows {
		fe.t.Logf("Metrics[%s]", vd.View.Name)
		switch row.Data.(type) {
		case *view.DistributionData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewDistribution
		case *view.CountData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewCount
		case *view.SumData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewSum
		case *view.LastValueData:
			fe.SeenViews[vd.View.Name] = TypeOpenCensusViewLastValue
		}
	}
}

func (fe *fakeOpenCensusExporter) ExportSpan(vd *trace.SpanData) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.SeenSpans++
	fe.t.Logf("Span[%v]", vd.Name)
}

func (s) TestLoggingForOkCall(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	te.enablePluginWithCaptureAll()
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
	// Check size of events
	if len(te.fle.clientEvents) != 5 {
		t.Fatalf("expects 5 client events, got %d", len(te.fle.clientEvents))
	}
	if len(te.fle.serverEvents) != 5 {
		t.Fatalf("expects 5 server events, got %d", len(te.fle.serverEvents))
	}
	// Client events
	checkEventRequestHeader(te.t, te.fle.clientEvents[0], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
		Authority:   te.srvAddr,
		ServiceName: "grpc.testing.TestService",
		MethodName:  "UnaryCall",
	})
	checkEventRequestMessage(te.t, te.fle.clientEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	}, testOkPayload)
	checkEventResponseHeader(te.t, te.fle.clientEvents[2], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	})
	checkEventResponseMessage(te.t, te.fle.clientEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	}, testOkPayload)
	checkEventTrailer(te.t, te.fle.clientEvents[4], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
		StatusCode:  0,
	})
	// Server events
	checkEventRequestHeader(te.t, te.fle.serverEvents[0], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	})
	checkEventRequestMessage(te.t, te.fle.serverEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	}, testOkPayload)
	checkEventResponseHeader(te.t, te.fle.serverEvents[2], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	})
	checkEventResponseMessage(te.t, te.fle.serverEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	}, testOkPayload)
	checkEventTrailer(te.t, te.fle.serverEvents[4], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
		StatusCode:  0,
	})
}

func (s) TestLoggingForErrorCall(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	te.enablePluginWithCaptureAll()
	te.startServer(&testServer{})
	tc := testgrpc.NewTestServiceClient(te.clientConn())

	req := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: testErrorPayload}}
	tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := tc.UnaryCall(metadata.NewOutgoingContext(tCtx, testHeaderMetadata), req)
	if err == nil {
		t.Fatalf("unary call expected to fail, but passed")
	}

	// Wait for the gRPC transport to gracefully close to ensure no lost event.
	te.cc.Close()
	te.srv.GracefulStop()
	// Check size of events
	if len(te.fle.clientEvents) != 4 {
		t.Fatalf("expects 4 client events, got %d", len(te.fle.clientEvents))
	}
	if len(te.fle.serverEvents) != 4 {
		t.Fatalf("expects 4 server events, got %d", len(te.fle.serverEvents))
	}
	// Client events
	checkEventRequestHeader(te.t, te.fle.clientEvents[0], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
		Authority:   te.srvAddr,
		ServiceName: "grpc.testing.TestService",
		MethodName:  "UnaryCall",
	})
	checkEventRequestMessage(te.t, te.fle.clientEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	}, testErrorPayload)
	checkEventResponseHeader(te.t, te.fle.clientEvents[2], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	})
	checkEventTrailer(te.t, te.fle.clientEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger:   grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
		StatusCode:    2,
		StatusMessage: testErrorMessage,
	})
	// Server events
	checkEventRequestHeader(te.t, te.fle.serverEvents[0], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	})
	checkEventRequestMessage(te.t, te.fle.serverEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	}, testErrorPayload)
	checkEventResponseHeader(te.t, te.fle.serverEvents[2], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	})
	checkEventTrailer(te.t, te.fle.serverEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger:   grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
		StatusCode:    2,
		StatusMessage: testErrorMessage,
	})
}

func (s) TestEmptyConfig(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	te.enablePluginWithConfig(&config{})
	te.startServer(&testServer{})
	tc := testgrpc.NewTestServiceClient(te.clientConn())

	req := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: testOkPayload}}
	tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	resp, err := tc.UnaryCall(metadata.NewOutgoingContext(tCtx, testHeaderMetadata), req)
	if err != nil {
		t.Fatalf("unary call failed: %v", err)
	}
	t.Logf("unary call passed: %v", resp)

	// Wait for the gRPC transport to gracefully close to ensure no lost event.
	te.cc.Close()
	te.srv.GracefulStop()
	// Check size of events
	if len(te.fle.clientEvents) != 0 {
		t.Fatalf("expects 0 client events, got %d", len(te.fle.clientEvents))
	}
	if len(te.fle.serverEvents) != 0 {
		t.Fatalf("expects 0 server events, got %d", len(te.fle.serverEvents))
	}
}

func (s) TestOverrideConfig(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	// Setting 3 filters, expected to use the third filter, because it's the
	// most specific one. The third filter allows message payload logging, and
	// others disabling the message payload logging. We should observe this
	// behavior latter.
	te.enablePluginWithConfig(&config{
		EnableCloudLogging:   true,
		DestinationProjectID: "fake",
		LogFilters: []logFilter{
			{
				Pattern:      "wont/match",
				MessageBytes: 0,
			},
			{
				Pattern:      "*",
				MessageBytes: 0,
			},
			{
				Pattern:      "grpc.testing.TestService/*",
				MessageBytes: 4096,
			},
		},
	})
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
	// Check size of events
	if len(te.fle.clientEvents) != 5 {
		t.Fatalf("expects 5 client events, got %d", len(te.fle.clientEvents))
	}
	if len(te.fle.serverEvents) != 5 {
		t.Fatalf("expects 5 server events, got %d", len(te.fle.serverEvents))
	}
	// Check Client message payloads
	checkEventRequestMessage(te.t, te.fle.clientEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	}, testOkPayload)
	checkEventResponseMessage(te.t, te.fle.clientEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	}, testOkPayload)
	// Check Server message payloads
	checkEventRequestMessage(te.t, te.fle.serverEvents[1], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	}, testOkPayload)
	checkEventResponseMessage(te.t, te.fle.serverEvents[3], &grpclogrecordpb.GrpcLogRecord{
		EventLogger: grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
	}, testOkPayload)
}

func (s) TestNoMatch(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	// Setting 3 filters, expected to use the second filter. The second filter
	// allows message payload logging, and others disabling the message payload
	// logging. We should observe this behavior latter.
	te.enablePluginWithConfig(&config{
		EnableCloudLogging:   true,
		DestinationProjectID: "fake",
		LogFilters: []logFilter{
			{
				Pattern:      "wont/match",
				MessageBytes: 0,
			},
		},
	})
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
	// Check size of events
	if len(te.fle.clientEvents) != 0 {
		t.Fatalf("expects 0 client events, got %d", len(te.fle.clientEvents))
	}
	if len(te.fle.serverEvents) != 0 {
		t.Fatalf("expects 0 server events, got %d", len(te.fle.serverEvents))
	}
}

func (s) TestRefuseStartWithInvalidPatterns(t *testing.T) {
	config := &config{
		EnableCloudLogging:   true,
		DestinationProjectID: "fake",
		LogFilters: []logFilter{
			{
				Pattern: ":-)",
			},
			{
				Pattern: "*",
			},
		},
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	os.Setenv(envObservabilityConfigJSON, "")
	os.Setenv(envObservabilityConfig, string(configJSON))
	// If there is at least one invalid pattern, which should not be silently tolerated.
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

// createTmpConfigInFileSystem creates a random observability config at a random
// place in the temporary portion of the file system dependent on system. It
// also sets the environment variable GRPC_CONFIG_OBSERVABILITY_JSON to point to
// this created config.
func createTmpConfigInFileSystem(rawJSON string) (func(), error) {
	configJSONFile, err := ioutil.TempFile(os.TempDir(), "configJSON-")
	if err != nil {
		return nil, fmt.Errorf("cannot create file %v: %v", configJSONFile.Name(), err)
	}
	_, err = configJSONFile.Write(json.RawMessage(rawJSON))
	if err != nil {
		return nil, fmt.Errorf("cannot write marshalled JSON: %v", err)
	}
	os.Setenv(envObservabilityConfigJSON, configJSONFile.Name())
	return func() {
		configJSONFile.Close()
		os.Setenv(envObservabilityConfigJSON, "")
	}, nil
}

// TestJSONEnvVarSet tests a valid observability configuration specified by the
// GRPC_CONFIG_OBSERVABILITY_JSON environment variable, whose value represents a
// file path pointing to a JSON encoded config.
func (s) TestJSONEnvVarSet(t *testing.T) {
	configJSON := `{
		"destination_project_id": "fake",
		"log_filters":[{"pattern":"*","header_bytes":1073741824,"message_bytes":1073741824}]
	}`
	cleanup, err := createTmpConfigInFileSystem(configJSON)
	defer cleanup()

	if err != nil {
		t.Fatalf("failed to create config in file system: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := Start(ctx); err != nil {
		t.Fatalf("error starting observability with valid config through file system: %v", err)
	}
	defer End()
}

// TestBothConfigEnvVarsSet tests the scenario where both configuration
// environment variables are set. The file system environment variable should
// take precedence, and an error should return in the case of the file system
// configuration being invalid, even if the direct configuration environment
// variable is set and valid.
func (s) TestBothConfigEnvVarsSet(t *testing.T) {
	configJSON := `{
		"destination_project_id":"fake",
		"log_filters":[{"pattern":":-)"}, {"pattern_string":"*"}]
	}`
	cleanup, err := createTmpConfigInFileSystem(configJSON)
	defer cleanup()
	if err != nil {
		t.Fatalf("failed to create config in file system: %v", err)
	}
	// This configuration should be ignored, as precedence 2.
	validConfig := &config{
		EnableCloudLogging:   true,
		DestinationProjectID: "fake",
		LogFilters: []logFilter{
			{
				Pattern:      "*",
				HeaderBytes:  infinitySizeBytes,
				MessageBytes: infinitySizeBytes,
			},
		},
	}
	validConfigJSON, err := json.Marshal(validConfig)
	if err != nil {
		t.Fatalf("failed to convert config to JSON: %v", err)
	}
	os.Setenv(envObservabilityConfig, string(validConfigJSON))
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

// TestErrInFileSystemEnvVar tests the scenario where an observability
// configuration is specified with environment variable that specifies a
// location in the file system for configuration, and this location doesn't have
// a file (or valid configuration).
func (s) TestErrInFileSystemEnvVar(t *testing.T) {
	os.Setenv(envObservabilityConfigJSON, "/this-file/does-not-exist")
	defer os.Setenv(envObservabilityConfigJSON, "")
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid file system path not triggering error")
	}
}

func (s) TestNoEnvSet(t *testing.T) {
	os.Setenv(envObservabilityConfigJSON, "")
	os.Setenv(envObservabilityConfig, "")
	// If there is no observability config set at all, the Start should return an error.
	if err := Start(context.Background()); err == nil {
		t.Fatalf("Invalid patterns not triggering error")
	}
}

func (s) TestOpenCensusIntegration(t *testing.T) {
	te := newTest(t)
	defer te.tearDown()
	fe := &fakeOpenCensusExporter{SeenViews: make(map[string]string), t: te.t}

	defer func(ne func(config *config) (tracingMetricsExporter, error)) {
		newExporter = ne
	}(newExporter)

	newExporter = func(config *config) (tracingMetricsExporter, error) {
		return fe, nil
	}

	te.enableOpenCensus()
	te.startServer(&testServer{})
	tc := testgrpc.NewTestServiceClient(te.clientConn())

	for i := 0; i < defaultRequestCount; i++ {
		req := &testpb.SimpleRequest{Payload: &testpb.Payload{Body: testOkPayload}}
		tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		_, err := tc.UnaryCall(metadata.NewOutgoingContext(tCtx, testHeaderMetadata), req)
		if err != nil {
			t.Fatalf("unary call failed: %v", err)
		}
	}
	t.Logf("unary call passed count=%v", defaultRequestCount)

	// Wait for the gRPC transport to gracefully close to ensure no lost event.
	te.cc.Close()
	te.srv.GracefulStop()

	var errs []error
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for ctx.Err() == nil {
		errs = nil
		fe.mu.RLock()
		if value := fe.SeenViews["grpc.io/client/completed_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/client/completed_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}
		if value := fe.SeenViews["grpc.io/server/completed_rpcs"]; value != TypeOpenCensusViewCount {
			errs = append(errs, fmt.Errorf("unexpected type for grpc.io/server/completed_rpcs: %s != %s", value, TypeOpenCensusViewCount))
		}
		if fe.SeenSpans <= 0 {
			errs = append(errs, fmt.Errorf("unexpected number of seen spans: %v <= 0", fe.SeenSpans))
		}
		fe.mu.RUnlock()
		if len(errs) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(errs) != 0 {
		t.Fatalf("Invalid OpenCensus export data: %v", errs)
	}
}

// TestCustomTagsTracingMetrics verifies that the custom tags defined in our
// observability configuration and set to two hardcoded values are passed to the
// function to create an exporter.
func (s) TestCustomTagsTracingMetrics(t *testing.T) {
	defer func(ne func(config *config) (tracingMetricsExporter, error)) {
		newExporter = ne
	}(newExporter)
	fe := &fakeOpenCensusExporter{SeenViews: make(map[string]string), t: t}
	newExporter = func(config *config) (tracingMetricsExporter, error) {
		ct := config.CustomTags
		if len(ct) < 1 {
			t.Fatalf("less than 2 custom tags sent in")
		}
		if val, ok := ct["customtag1"]; !ok || val != "wow" {
			t.Fatalf("incorrect custom tag: got %v, want %v", val, "wow")
		}
		if val, ok := ct["customtag2"]; !ok || val != "nice" {
			t.Fatalf("incorrect custom tag: got %v, want %v", val, "nice")
		}
		return fe, nil
	}

	// This configuration present in file system and it's defined custom tags should make it
	// to the created exporter.
	configJSON := `{
		"destination_project_id": "fake",
		"enable_cloud_trace": true,
		"enable_cloud_monitoring": true,
		"global_trace_sampling_rate": 1.0,
		"custom_tags":{"customtag1":"wow","customtag2":"nice"}
	}`
	cleanup, err := createTmpConfigInFileSystem(configJSON)
	defer cleanup()

	// To clear globally registered tracing and metrics exporters.
	defer func() {
		internal.ClearExtraDialOptions()
		internal.ClearExtraServerOptions()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	err = Start(ctx)
	defer End()
	if err != nil {
		t.Fatalf("Start() failed with err: %v", err)
	}
}
