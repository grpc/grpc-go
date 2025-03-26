/*
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
 */

package opentelemetry_test

import (
	"context"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3clientsideweightedroundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/client_side_weighted_round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	experimental "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	itestutils "google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	setup "google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/stats/opentelemetry"
	"google.golang.org/grpc/stats/opentelemetry/internal/testutils"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// traceSpanInfo is the information received about the trace span. It contains
// subset of information that is needed to verify if correct trace is being
// attributed to the rpc.
type traceSpanInfo struct {
	spanKind   string
	name       string
	events     []trace.Event
	attributes []attribute.KeyValue
}

// traceSpanInfoMapKey is the key struct for constructing a map of trace spans
// retrievable by span name and span kind
type traceSpanInfoMapKey struct {
	spanName string
	spanKind string
}

// defaultMetricsOptions creates default metrics options
func defaultMetricsOptions(_ *testing.T, methodAttributeFilter func(string) bool) (*opentelemetry.MetricsOptions, *metric.ManualReader) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	metricsOptions := &opentelemetry.MetricsOptions{
		MeterProvider:         provider,
		Metrics:               opentelemetry.DefaultMetrics(),
		MethodAttributeFilter: methodAttributeFilter,
	}
	return metricsOptions, reader
}

// defaultTraceOptions function to create default trace options
func defaultTraceOptions(_ *testing.T) (*experimental.TraceOptions, *tracetest.InMemoryExporter) {
	spanExporter := tracetest.NewInMemoryExporter()
	spanProcessor := trace.NewSimpleSpanProcessor(spanExporter)
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanProcessor))
	textMapPropagator := propagation.NewCompositeTextMapPropagator(opentelemetry.GRPCTraceBinPropagator{})
	traceOptions := &experimental.TraceOptions{
		TracerProvider:    tracerProvider,
		TextMapPropagator: textMapPropagator,
	}
	return traceOptions, spanExporter
}

// setupStubServer creates a stub server with OpenTelemetry component configured on client
// and server side and returns the server.
func setupStubServer(t *testing.T, metricsOptions *opentelemetry.MetricsOptions, traceOptions *experimental.TraceOptions) *stubserver.StubServer {
	ss := &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: &testpb.Payload{
				Body: make([]byte, len(in.GetPayload().GetBody())),
			}}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}

	otelOptions := opentelemetry.Options{}
	if metricsOptions != nil {
		otelOptions.MetricsOptions = *metricsOptions
	}
	if traceOptions != nil {
		otelOptions.TraceOptions = *traceOptions
	}

	if err := ss.Start([]grpc.ServerOption{opentelemetry.ServerOption(otelOptions)},
		opentelemetry.DialOption(otelOptions)); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	return ss
}

// waitForTraceSpans waits until the in-memory span exporter has received the
// expected trace spans based on span name and kind. It polls the exporter at a
// short interval until the desired spans are available or the context is
// cancelled.
//
// Returns the collected spans or an error if the context deadline is exceeded
// before the expected spans are exported.
func waitForTraceSpans(ctx context.Context, exporter *tracetest.InMemoryExporter, wantSpans []traceSpanInfo) (tracetest.SpanStubs, error) {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		spans := exporter.GetSpans()
		missingAnySpan := false
		for _, wantSpan := range wantSpans {
			if !slices.ContainsFunc(spans, func(span tracetest.SpanStub) bool {
				return span.Name == wantSpan.name && span.SpanKind.String() == wantSpan.spanKind
			}) {
				missingAnySpan = true
			}
		}
		if !missingAnySpan {
			return spans, nil
		}
	}
	return nil, fmt.Errorf("error waiting for complete trace spans %v: %v", wantSpans, ctx.Err())
}

// validateTraces first first groups the received spans by their TraceID. For
// each trace group, it identifies the client, server, and attempt spans for
// both unary and streaming RPCs. It checks that the expected spans are
// present and that the server spans have the correct parent (attempt span).
// Finally, it compares the content of each span (name, kind, attributes,
// events) against the provided expected spans information.
func validateTraces(t *testing.T, spans tracetest.SpanStubs, wantSpanInfos []traceSpanInfo) {
	// Group spans by TraceID.
	traceSpans := make(map[oteltrace.TraceID][]tracetest.SpanStub)
	for _, span := range spans {
		traceID := span.SpanContext.TraceID()
		traceSpans[traceID] = append(traceSpans[traceID], span)
	}

	// For each trace group, verify relationships and content.
	for traceID, spans := range traceSpans {
		var unaryClient, unaryServer, unaryAttempt *tracetest.SpanStub
		var streamClient, streamServer, streamAttempt *tracetest.SpanStub
		var isUnary, isStream bool

		for _, span := range spans {
			switch {
			case span.Name == "grpc.testing.TestService.UnaryCall":
				isUnary = true
				if span.SpanKind == oteltrace.SpanKindClient {
					unaryClient = &span
				} else {
					unaryServer = &span
				}
			case span.Name == "Attempt.grpc.testing.TestService.UnaryCall":
				isUnary = true
				unaryAttempt = &span
			case span.Name == "grpc.testing.TestService.FullDuplexCall":
				isStream = true
				if span.SpanKind == oteltrace.SpanKindClient {
					streamClient = &span
				} else {
					streamServer = &span
				}
			case span.Name == "Attempt.grpc.testing.TestService.FullDuplexCall":
				isStream = true
				streamAttempt = &span
			}
		}

		if isUnary {
			// Verify Unary Call Spans.
			if unaryClient == nil {
				t.Error("Unary call client span not found")
			}
			if unaryServer == nil {
				t.Error("Unary call server span not found")
			}
			if unaryAttempt == nil {
				t.Error("Unary call attempt span not found")
			}
			// Check TraceID consistency.
			if unaryClient != nil && unaryClient.SpanContext.TraceID() != traceID || unaryServer.SpanContext.TraceID() != traceID {
				t.Error("Unary call spans have inconsistent TraceIDs")
			}
			// Check parent-child relationship via SpanID.
			if unaryServer != nil && unaryServer.Parent.SpanID() != unaryAttempt.SpanContext.SpanID() {
				t.Error("Unary server span parent does not match attempt span ID")
			}
		}

		if isStream {
			// Verify Streaming Call Spans.
			if streamClient == nil {
				t.Error("Streaming call client span not found")
			}
			if streamServer == nil {
				t.Error("Streaming call server span not found")
			}
			if streamAttempt == nil {
				t.Error("Streaming call attempt span not found")
			}
			// Check TraceID consistency.
			if streamClient != nil && streamClient.SpanContext.TraceID() != traceID || streamServer.SpanContext.TraceID() != traceID {
				t.Error("Streaming call spans have inconsistent TraceIDs")
			}
			if streamServer != nil && streamServer.Parent.SpanID() != streamAttempt.SpanContext.SpanID() {
				t.Error("Streaming server span parent does not match attempt span ID")
			}
		}
	}

	// Constructs a map from a slice of traceSpanInfo to retrieve the
	// corresponding expected span info based on span name and span kind
	// for comparison.
	wantSpanInfosMap := make(map[traceSpanInfoMapKey]traceSpanInfo)
	for _, info := range wantSpanInfos {
		key := traceSpanInfoMapKey{spanName: info.name, spanKind: info.spanKind}
		wantSpanInfosMap[key] = info
	}

	// Compare retrieved spans with expected spans.
	for _, span := range spans {
		// Check that the attempt span has the correct status.
		if got, want := span.Status.Code, otelcodes.Ok; got != want {
			t.Errorf("Got status code %v, want %v", got, want)
		}

		// Retrieve the corresponding expected span info based on span name and
		// span kind to compare.
		want, ok := wantSpanInfosMap[traceSpanInfoMapKey{spanName: span.Name, spanKind: span.SpanKind.String()}]
		if !ok {
			t.Errorf("Unexpected span: %v", span)
			continue
		}

		// comparers
		attributesSort := cmpopts.SortSlices(func(a, b attribute.KeyValue) bool {
			return a.Key < b.Key
		})
		attributesValueComparable := cmpopts.EquateComparable(attribute.KeyValue{}.Value)
		eventsTimeIgnore := cmpopts.IgnoreFields(trace.Event{}, "Time")

		// attributes
		if diff := cmp.Diff(want.attributes, span.Attributes, attributesSort, attributesValueComparable); diff != "" {
			t.Errorf("Attributes mismatch for span %s (-want +got):\n%s", span.Name, diff)
		}
		// events
		if diff := cmp.Diff(want.events, span.Events, attributesSort, attributesValueComparable, eventsTimeIgnore); diff != "" {
			t.Errorf("Events mismatch for span %s (-want +got):\n%s", span.Name, diff)
		}
	}
}

// TestMethodAttributeFilter tests the method attribute filter. The method
// filter set should bucket the grpc.method attribute into "other" if the method
// attribute filter specifies.
func (s) TestMethodAttributeFilter(t *testing.T) {
	maf := func(str string) bool {
		// Will allow duplex/any other type of RPC.
		return str != testgrpc.TestService_UnaryCall_FullMethodName
	}
	mo, reader := defaultMetricsOptions(t, maf)
	ss := setupStubServer(t, mo, nil)
	defer ss.Stop()

	// Make a Unary and Streaming RPC. The Unary RPC should be filtered by the
	// method attribute filter, and the Full Duplex (Streaming) RPC should not.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall"), attribute.String("grpc.target", ss.Target)),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall"), attribute.String("grpc.target", ss.Target)),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.server.call.duration",
			Description: "End-to-end time taken to complete a call from server transport's perspective.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{ // Method should go to "other" due to the method attribute filter.
						Attributes: attribute.NewSet(attribute.String("grpc.method", "other"), attribute.String("grpc.status", "OK")),
						Count:      1,
						Bounds:     testutils.DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall"), attribute.String("grpc.status", "OK")),
						Count:      1,
						Bounds:     testutils.DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}

	gotMetrics = testutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
	testutils.CompareMetrics(t, gotMetrics, wantMetrics)
}

// TestAllMetricsOneFunction tests emitted metrics from OpenTelemetry
// instrumentation component. It then configures a system with a gRPC Client and
// gRPC server with the OpenTelemetry Dial and Server Option configured
// specifying all the metrics provided by this package, and makes a Unary RPC
// and a Streaming RPC. These two RPCs should cause certain recording for each
// registered metric observed through a Manual Metrics Reader on the provided
// OpenTelemetry SDK's Meter Provider. It then makes an RPC that is unregistered
// on the Client (no StaticMethodCallOption set) and Server. The method
// attribute on subsequent metrics should be bucketed in "other".
func (s) TestAllMetricsOneFunction(t *testing.T) {
	mo, reader := defaultMetricsOptions(t, nil)
	ss := setupStubServer(t, mo, nil)
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics to be emitted, which should be observed through the
	// Metric Reader.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}, grpc.UseCompressor(gzip.Name)); err != nil { // Deterministic compression.
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}

	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)

	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	wantMetrics := testutils.MetricData(testutils.MetricDataOptions{
		Target:                     ss.Target,
		UnaryCompressedMessageSize: float64(57),
	})
	gotMetrics = testutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
	testutils.CompareMetrics(t, gotMetrics, wantMetrics)

	stream, err = ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}
	// This Invoke doesn't pass the StaticMethodCallOption. Thus, the method
	// attribute should become "other" on client side metrics. Since it is also
	// not registered on the server either, it should also become "other" on the
	// server metrics method attribute.
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)

	rm = &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	gotMetrics = map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}
	unaryMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall")
	duplexMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall")

	targetAttr := attribute.String("grpc.target", ss.Target)
	otherMethodAttr := attribute.String("grpc.method", "other")
	wantMetrics = []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, targetAttr),
						Value:      2,
					},
					{
						Attributes: attribute.NewSet(otherMethodAttr, targetAttr),
						Value:      3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.server.call.started",
			Description: "Number of server calls started.",
			Unit:        "call",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr),
						Value:      2,
					},
					{
						Attributes: attribute.NewSet(otherMethodAttr),
						Value:      3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
	}
	for _, metric := range wantMetrics {
		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Fatalf("Metric %v not present in recorded metrics", metric.Name)
		}
		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Fatalf("Metrics data type not equal for metric: %v", metric.Name)
		}
	}
}

// clusterWithLBConfiguration returns a cluster resource with the proto message
// passed Marshaled to an any and specified through the load_balancing_policy
// field.
func clusterWithLBConfiguration(t *testing.T, clusterName, edsServiceName string, secLevel e2e.SecurityLevel, m proto.Message) *v3clusterpb.Cluster {
	cluster := e2e.DefaultCluster(clusterName, edsServiceName, secLevel)
	cluster.LoadBalancingPolicy = &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
			{
				TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
					TypedConfig: itestutils.MarshalAny(t, m),
				},
			},
		},
	}
	return cluster
}

func metricsDataFromReader(ctx context.Context, reader *metric.ManualReader) map[string]metricdata.Metrics {
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}
	return gotMetrics
}

// TestWRRMetrics tests the metrics emitted from the WRR LB Policy. It
// configures WRR as an endpoint picking policy through xDS on a ClientConn
// alongside an OpenTelemetry stats handler. It makes a few RPC's, and then
// sleeps for a bit to allow weight to expire. It then asserts OpenTelemetry
// metrics atoms are eventually present for all four WRR Metrics, alongside the
// correct target and locality label for each metric.
func (s) TestWRRMetrics(t *testing.T) {
	cmr := orca.NewServerMetricsRecorder().(orca.CallMetricsRecorder)
	backend1 := stubserver.StartTestService(t, &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			if r := orca.CallMetricsRecorderFromContext(ctx); r != nil {
				// Copy metrics from what the test set in cmr into r.
				sm := cmr.(orca.ServerMetricsProvider).ServerMetrics()
				r.SetApplicationUtilization(sm.AppUtilization)
				r.SetQPS(sm.QPS)
				r.SetEPS(sm.EPS)
			}
			return &testpb.Empty{}, nil
		},
	}, orca.CallMetricsServerOption(nil))
	port1 := itestutils.ParsePort(t, backend1.Address)
	defer backend1.Stop()

	cmr.SetQPS(10.0)
	cmr.SetApplicationUtilization(1.0)

	backend2 := stubserver.StartTestService(t, &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			if r := orca.CallMetricsRecorderFromContext(ctx); r != nil {
				// Copy metrics from what the test set in cmr into r.
				sm := cmr.(orca.ServerMetricsProvider).ServerMetrics()
				r.SetApplicationUtilization(sm.AppUtilization)
				r.SetQPS(sm.QPS)
				r.SetEPS(sm.EPS)
			}
			return &testpb.Empty{}, nil
		},
	}, orca.CallMetricsServerOption(nil))
	port2 := itestutils.ParsePort(t, backend2.Address)
	defer backend2.Stop()

	const serviceName = "my-service-client-side-xds"

	// Start an xDS management server.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	wrrConfig := &v3wrrlocalitypb.WrrLocality{
		EndpointPickingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: itestutils.MarshalAny(t, &v3clientsideweightedroundrobinpb.ClientSideWeightedRoundRobin{
							EnableOobLoadReport: &wrapperspb.BoolValue{
								Value: false,
							},
							// BlackoutPeriod long enough to cause load report
							// weight to trigger in the scope of test case.
							// WeightExpirationPeriod will cause the load report
							// weight for backend 1 to expire.
							BlackoutPeriod:          durationpb.New(5 * time.Millisecond),
							WeightExpirationPeriod:  durationpb.New(500 * time.Millisecond),
							WeightUpdatePeriod:      durationpb.New(time.Second),
							ErrorUtilizationPenalty: &wrapperspb.FloatValue{Value: 1},
						}),
					},
				},
			},
		},
	}

	routeConfigName := "route-" + serviceName
	clusterName := "cluster-" + serviceName
	endpointsName := "endpoints-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{clusterWithLBConfiguration(t, clusterName, endpointsName, e2e.SecurityLevelNone, wrrConfig)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointsName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{{Ports: []uint32{port1}}, {Ports: []uint32{port2}}},
					Weight:   1,
				},
			},
		})},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))

	mo := opentelemetry.MetricsOptions{
		MeterProvider:  provider,
		Metrics:        opentelemetry.DefaultMetrics().Add("grpc.lb.wrr.rr_fallback", "grpc.lb.wrr.endpoint_weight_not_yet_usable", "grpc.lb.wrr.endpoint_weight_stale", "grpc.lb.wrr.endpoint_weights"),
		OptionalLabels: []string{"grpc.lb.locality"},
	}

	target := fmt.Sprintf("xds:///%s", serviceName)
	cc, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver), opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Make 100 RPC's. The two backends will send back load reports per call
	// giving the two SubChannels weights which will eventually expire. Two
	// backends needed as for only one backend, WRR does not recompute the
	// scheduler.
	receivedExpectedMetrics := grpcsync.NewEvent()
	go func() {
		for !receivedExpectedMetrics.HasFired() && ctx.Err() == nil {
			client.EmptyCall(ctx, &testpb.Empty{})
			time.Sleep(2 * time.Millisecond)
		}
	}()

	targetAttr := attribute.String("grpc.target", target)
	localityAttr := attribute.String("grpc.lb.locality", `{"region":"region-1","zone":"zone-1","subZone":"subzone-1"}`)

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.lb.wrr.rr_fallback",
			Description: "EXPERIMENTAL. Number of scheduler updates in which there were not enough endpoints with valid weight, which caused the WRR policy to fall back to RR behavior.",
			Unit:        "update",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(targetAttr, localityAttr),
						Value:      1, // value ignored
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},

		{
			Name:        "grpc.lb.wrr.endpoint_weight_not_yet_usable",
			Description: "EXPERIMENTAL. Number of endpoints from each scheduler update that don't yet have usable weight information (i.e., either the load report has not yet been received, or it is within the blackout period).",
			Unit:        "endpoint",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(targetAttr, localityAttr),
						Value:      1, // value ignored
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.lb.wrr.endpoint_weights",
			Description: "EXPERIMENTAL. Weight of each endpoint, recorded on every scheduler update. Endpoints without usable weights will be recorded as weight 0.",
			Unit:        "endpoint",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(targetAttr, localityAttr),
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}

	if err := pollForWantMetrics(ctx, t, reader, wantMetrics); err != nil {
		t.Fatal(err)
	}
	receivedExpectedMetrics.Fire()

	// Poll for 5 seconds for weight expiration metric. No more RPC's are being
	// made, so weight should expire on a subsequent scheduler update.
	eventuallyWantMetric := metricdata.Metrics{
		Name:        "grpc.lb.wrr.endpoint_weight_stale",
		Description: "EXPERIMENTAL. Number of endpoints from each scheduler update whose latest weight is older than the expiration period.",
		Unit:        "endpoint",
		Data: metricdata.Sum[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(targetAttr, localityAttr),
					Value:      1, // value ignored
				},
			},
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: true,
		},
	}

	if err := pollForWantMetrics(ctx, t, reader, []metricdata.Metrics{eventuallyWantMetric}); err != nil {
		t.Fatal(err)
	}
}

// pollForWantMetrics polls for the wantMetrics to show up on reader. Returns an
// error if metric is present but not equal to expected, or if the wantMetrics
// do not show up during the context timeout.
func pollForWantMetrics(ctx context.Context, t *testing.T, reader *metric.ManualReader, wantMetrics []metricdata.Metrics) error {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		gotMetrics := metricsDataFromReader(ctx, reader)
		containsAllMetrics := true
		for _, metric := range wantMetrics {
			val, ok := gotMetrics[metric.Name]
			if !ok {
				containsAllMetrics = false
				break
			}
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreValue(), metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
				return fmt.Errorf("metrics data type not equal for metric: %v", metric.Name)
			}
		}
		if containsAllMetrics {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}

	return fmt.Errorf("error waiting for metrics %v: %v", wantMetrics, ctx.Err())
}

// TestMetricsAndTracesOptionEnabled verifies the integration of metrics and traces
// emitted by the OpenTelemetry instrumentation in a gRPC environment. It sets up a
// stub server with both metrics and traces enabled, and tests the correct emission
// of metrics and traces during a Unary RPC and a Streaming RPC. The test ensures
// that the emitted metrics reflect the operations performed, including the size of
// the compressed message, and verifies that tracing information is correctly recorded.
func (s) TestMetricsAndTracesOptionEnabled(t *testing.T) {
	// Create default metrics options
	mo, reader := defaultMetricsOptions(t, nil)
	// Create default trace options
	to, exporter := defaultTraceOptions(t)

	ss := setupStubServer(t, mo, to)
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout*2)
	defer cancel()

	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics and traces to be emitted which should be observed
	// through metrics reader and span exporter respectively.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}, grpc.UseCompressor(gzip.Name)); err != nil { // Deterministic compression.
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}

	// Verify metrics
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)

	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	wantMetrics := testutils.MetricData(testutils.MetricDataOptions{
		Target:                     ss.Target,
		UnaryCompressedMessageSize: float64(57),
	})
	gotMetrics = testutils.WaitForServerMetrics(ctx, t, reader, gotMetrics, wantMetrics)
	testutils.CompareMetrics(t, gotMetrics, wantMetrics)

	wantSpanInfos := []traceSpanInfo{
		{
			name:     "grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
			},
		},
		{
			name:     "Attempt.grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
			},
		},
		{
			name:       "grpc.testing.TestService.UnaryCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
		{
			name:       "grpc.testing.TestService.FullDuplexCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "Attempt.grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
	}

	spans, err := waitForTraceSpans(ctx, exporter, wantSpanInfos)
	if err != nil {
		t.Fatal(err)
	}
	validateTraces(t, spans, wantSpanInfos)
}

// TestSpan verifies that the gRPC Trace Binary propagator correctly
// propagates span context between a client and server using the grpc-
// trace-bin header. It sets up a stub server with OpenTelemetry tracing
// enabled, makes a unary RPC, and streaming RPC as well.
//
// Verification:
//   - Verifies that the span context is correctly propagated from the client
//     to the server, including the trace ID and span ID.
//   - Verifies that the server can access the span context and create
//     child spans as expected during the RPC calls.
//   - Verifies that the tracing information is recorded accurately in
//     the OpenTelemetry backend.
func (s) TestSpan(t *testing.T) {
	mo, _ := defaultMetricsOptions(t, nil)
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter.
	to, exporter := defaultTraceOptions(t)
	// Start the server with trace options.
	ss := setupStubServer(t, mo, to)
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain traces to be emitted, which should be observed through the
	// span exporter.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}
	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}

	wantSpanInfos := []traceSpanInfo{
		{
			name:     "grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			name:     "Attempt.grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			name:       "grpc.testing.TestService.UnaryCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
		{
			name:       "grpc.testing.TestService.FullDuplexCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "Attempt.grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
	}

	spans, err := waitForTraceSpans(ctx, exporter, wantSpanInfos)
	if err != nil {
		t.Fatal(err)
	}
	validateTraces(t, spans, wantSpanInfos)
}

// TestSpan_WithW3CContextPropagator sets up a stub server with OpenTelemetry tracing
// enabled, makes a unary and a streaming RPC, and then asserts that the correct
// number of spans are created with the expected spans.
//
// Verification:
//   - Verifies that the correct number of spans are created for both unary and
//     streaming RPCs.
//   - Verifies that the spans have the expected names and attributes, ensuring
//     they accurately reflect the operations performed.
//   - Verifies that the trace ID and span ID are correctly assigned and accessible
//     in the OpenTelemetry backend.
func (s) TestSpan_WithW3CContextPropagator(t *testing.T) {
	mo, _ := defaultMetricsOptions(t, nil)
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter
	to, exporter := defaultTraceOptions(t)
	// Set the W3CContextPropagator as part of TracingOptions.
	to.TextMapPropagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
	// Start the server with OpenTelemetry options
	ss := setupStubServer(t, mo, to)
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain traces to be emitted, which should be observed through the
	// span exporter.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}

	wantSpanInfos := []traceSpanInfo{
		{
			name:     "grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			name:     "Attempt.grpc.testing.TestService.UnaryCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			name:       "grpc.testing.TestService.UnaryCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(false),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
		{
			name:       "grpc.testing.TestService.FullDuplexCall",
			spanKind:   oteltrace.SpanKindClient.String(),
			attributes: nil,
			events:     nil,
		},
		{
			name:     "Attempt.grpc.testing.TestService.FullDuplexCall",
			spanKind: oteltrace.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "FailFast",
					Value: attribute.BoolValue(true),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.BoolValue(false),
				},
			},
			events: nil,
		},
	}

	spans, err := waitForTraceSpans(ctx, exporter, wantSpanInfos)
	if err != nil {
		t.Fatal(err)
	}
	validateTraces(t, spans, wantSpanInfos)
}

// TestMetricsAndTracesDisabled verifies that RPCs call succeed as expected
// when metrics and traces are disabled in the OpenTelemetry instrumentation.
func (s) TestMetricsAndTracesDisabled(t *testing.T) {
	ss := &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: &testpb.Payload{
				Body: make([]byte, len(in.GetPayload().GetBody())),
			}}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}

	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make two RPCs, a unary RPC and a streaming RPC.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %v", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv received an unexpected error: %v, expected an EOF error", err)
	}
}

// TestRPCSpanErrorStatus verifies that errors during RPC calls are correctly
// reflected in the span status. It simulates a unary RPC that returns an error
// and checks that the span's status is set to error with the appropriate message.
func (s) TestRPCSpanErrorStatus(t *testing.T) {
	mo, _ := defaultMetricsOptions(t, nil)
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter
	to, exporter := defaultTraceOptions(t)
	const rpcErrorMsg = "unary call: internal server error"
	ss := &stubserver.StubServer{
		UnaryCallF: func(_ context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return nil, fmt.Errorf("%v", rpcErrorMsg)
		},
	}

	otelOptions := opentelemetry.Options{
		MetricsOptions: *mo,
		TraceOptions:   *to,
	}

	if err := ss.Start([]grpc.ServerOption{opentelemetry.ServerOption(otelOptions)},
		opentelemetry.DialOption(otelOptions)); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}})

	// Verify spans has error status with rpcErrorMsg as error message.
	for ; len(exporter.GetSpans()) == 0 && ctx.Err() == nil; <-time.After(time.Millisecond) {
		// wait until trace spans are collected
	}
	spans := exporter.GetSpans()
	if got, want := spans[0].Status.Description, rpcErrorMsg; got != want {
		t.Fatalf("got rpc error %s, want %s", spans[0].Status.Description, rpcErrorMsg)
	}
}
