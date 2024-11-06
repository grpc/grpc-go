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
	"testing"
	"time"

	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"

	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	trace2 "go.opentelemetry.io/otel/trace"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3clientsideweightedroundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/client_side_weighted_round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
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
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	itestutils "google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	setup "google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/orca"
	"google.golang.org/grpc/stats/opentelemetry"
	"google.golang.org/grpc/stats/opentelemetry/internal/testutils"
	"google.golang.org/grpc/stats/opentelemetry/tracing"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
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
func defaultTraceOptions(_ *testing.T) (*opentelemetry.TraceOptions, *tracetest.InMemoryExporter) {
	spanExporter := tracetest.NewInMemoryExporter()
	spanProcessor := trace.NewSimpleSpanProcessor(spanExporter)
	tracerProvider := trace.NewTracerProvider(trace.WithSpanProcessor(spanProcessor))
	textMapPropagator := propagation.NewCompositeTextMapPropagator(tracing.GRPCTraceBinPropagator{})
	traceOptions := &opentelemetry.TraceOptions{
		TracerProvider:    tracerProvider,
		TextMapPropagator: textMapPropagator,
	}
	return traceOptions, spanExporter
}

// setupStubServer creates a stub server with OpenTelemetry component configured on client
// and server side and returns the server.
func setupStubServer(t *testing.T, metricsOptions *opentelemetry.MetricsOptions, traceOptions *opentelemetry.TraceOptions) *stubserver.StubServer {
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
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

	testutils.CompareMetrics(ctx, t, reader, gotMetrics, wantMetrics)
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
	// certain metrics to be emitted, which should be able to be observed
	// through the Metric Reader.
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
	testutils.CompareMetrics(ctx, t, reader, gotMetrics, wantMetrics)

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
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
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
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
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
					Backends: []e2e.BackendOptions{{Port: port1}, {Port: port2}},
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
		for !receivedExpectedMetrics.HasFired() {
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

// spanInformation is the information received about the span. This is a subset
// of information that is important to verify that gRPC has knobs over, which
// goes through a stable OpenTelemetry API with well-defined behavior. This keeps
// the robustness of assertions over time.
type spanInformation struct {
	// SpanContext either gets pulled off the wire in certain cases server side
	// or created.
	sc         trace2.SpanContext
	spanKind   string
	name       string
	events     []trace.Event
	attributes []attribute.KeyValue
}

// TestClientCallSpanEvents verifies the events added to call spans
// for a unary RPC, including events for gRPC status and name resolution
// delays. It also verifies the same traceID is propagated across client
// to server. It sets up a stub server with OpenTelemetry tracing enabled,
// makes a unary RPC with gzip compression, and then asserts that the exported
// spans contain the expected events and attributes.
func (s) TestClientCallSpanEvents(t *testing.T) {
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter
	traceOptions, spanExporter := defaultTraceOptions(t)

	// Start the server with OpenTelemetry options
	ss := setupStubServer(t, nil, traceOptions)
	defer ss.Stop()

	// Create a parent span for the client call
	ctx, _ := otel.Tracer("grpc-open-telemetry").Start(context.Background(), "test-parent-span")
	md, _ := metadata.FromOutgoingContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, otelinternaltracing.NewCustomCarrier(ctx))
	ctx = metadata.NewOutgoingContext(ctx, md)

	grpc.NameResolutionDelayDuration = 0
	// Make a unary RPC
	if _, err := ss.Client.UnaryCall(
		ctx,
		&testpb.SimpleRequest{Payload: &testpb.Payload{Body: make([]byte, 10000)}},
		grpc.UseCompressor(gzip.Name), // Deterministic compression.
	); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	// Get the spans from the exporter
	spans := spanExporter.GetSpans()
	if got, want := len(spans), 3; got != want {
		t.Fatalf("Got %d spans, want %d", got, want)
	}

	clientSpan := spans[2]
	// Check that the client span has the correct status.
	if got, want := clientSpan.Status.Code, otelcodes.Ok; got != want {
		t.Errorf("Got client span status code %v, want %v", got, want)
	}
	// Check that the client has event for name resolution delay.
	if got, want := clientSpan.Events[0].Name, "Delayed name resolution complete"; got != want {
		t.Fatal("Client span didn't had event for delayed name resolution.")
	}
	// Check that same traceID is used in client and server.
	if got, want := spans[0].SpanContext.TraceID(), spans[2].SpanContext.TraceID(); got != want {
		t.Fatal("TraceID mismatch in client span and server span.")
	}
	// Check that the attempt span id of client matches the span id of server
	// SpanContext.
	if got, want := spans[0].Parent.SpanID(), spans[1].SpanContext.SpanID(); got != want {
		t.Fatal("SpanID mismatch in client span and server span.")
	}
}

// TestServerWithMetricsAndTraceOptions tests emitted metrics and traces from
// OpenTelemetry instrumentation component. It then configures a system with a gRPC
// Client and gRPC server with the OpenTelemetry Dial and Server Option configured
// specifying all the metrics and traces provided by this package, and makes a Unary
// RPC and a Streaming RPC. These two RPCs should cause certain recording for each
// registered metric observed through a Manual Metrics Reader on the provided
// OpenTelemetry SDK's Meter Provider. It also verifies the traces are recorded
// correctly.
func (s) TestServerWithMetricsAndTraceOptions(t *testing.T) {
	// Create default metrics options
	mo, reader := defaultMetricsOptions(t, nil)
	// Create default trace options
	to, exporter := defaultTraceOptions(t)

	ss := setupStubServer(t, mo, to)
	defer ss.Stop()
	// Create a parent span for the client call
	ctx, _ := otel.Tracer("grpc-open-telemetry").Start(context.Background(), "test-parent-span")
	md, _ := metadata.FromOutgoingContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, otelinternaltracing.NewCustomCarrier(ctx))
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics and traces to be emitted.
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
	testutils.CompareMetrics(ctx, t, reader, gotMetrics, wantMetrics)

	// Verify traces
	spans := exporter.GetSpans()
	if got, want := len(spans), 6; got != want {
		t.Fatalf("Got %d spans, want %d", got, want)
	}

	// Add assertions for specific span attributes and events as needed.
	// For example, to check if the client span has the correct status:
	clientSpan := spans[2]
	if got, want := clientSpan.Status.Code, otelcodes.Ok; got != want {
		t.Errorf("Got status code %v, want %v", got, want)
	}
}

// TestGrpcTraceBinPropagator verifies that the gRPC Trace Binary propagator
// correctly propagates span context between a client and server using the
// grpc-trace-bin header. It sets up a stub server with OpenTelemetry tracing
// enabled, makes a unary RPC.
func (s) TestGrpcTraceBinPropagator(t *testing.T) {
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter
	traceOptions, spanExporter := defaultTraceOptions(t)

	// Start the server with OpenTelemetry options
	ss := setupStubServer(t, nil, traceOptions)
	defer ss.Stop()
	// Override NameResolutionDelayDuration to 0 so that client span
	// contains event for name resolution delay.
	grpc.NameResolutionDelayDuration = 0

	// Create a parent span for the client call
	ctx, _ := otel.Tracer("grpc-open-telemetry").Start(context.Background(), "test-parent-span")
	md, _ := metadata.FromOutgoingContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, otelinternaltracing.NewCustomCarrier(ctx))
	ctx = metadata.NewOutgoingContext(ctx, md)
	// Make a unary RPC
	if _, err := ss.Client.UnaryCall(
		ctx,
		&testpb.SimpleRequest{Payload: &testpb.Payload{Body: make([]byte, 10000)}},
	); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	// Get the spans from the exporter
	spans := spanExporter.GetSpans()
	if got, want := len(spans), 3; got != want {
		t.Fatalf("Got %d spans, want %d", got, want)
	}

	wantSI := []spanInformation{
		{
			name:     "grpc.testing.TestService.UnaryCall",
			spanKind: trace2.SpanKindServer.String(),
			attributes: []attribute.KeyValue{
				{
					Key: "Client",
				},
				{
					Key: "FailFast",
				},
				{
					Key: "previous-rpc-attempts",
				},
				{
					Key: "transparent-retry",
				},
			},
			events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key: "sequence-number",
						},
						{
							Key: "message-size",
						},
						{
							Key: "message-size-compressed",
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key: "sequence-number",
						},
						{
							Key: "message-size",
						},
						{
							Key: "message-size-compressed",
						},
					},
				},
			},
		},
		{
			name:     "Attempt.grpc.testing.TestService.UnaryCall",
			spanKind: trace2.SpanKindInternal.String(),
			attributes: []attribute.KeyValue{
				{
					Key: "Client",
				},
				{
					Key: "FailFast",
				},
				{
					Key: "previous-rpc-attempts",
				},
				{
					Key: "transparent-retry",
				},
			},
			events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key: "sequence-number",
						},
						{
							Key: "message-size",
						},
						{
							Key: "message-size-compressed",
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key: "sequence-number",
						},
						{
							Key: "message-size",
						},
						{
							Key: "message-size-compressed",
						},
					},
				},
			},
		},
		{
			name:       "grpc.testing.TestService.UnaryCall",
			spanKind:   trace2.SpanKindClient.String(),
			attributes: []attribute.KeyValue{},
			events: []trace.Event{
				{
					Name: "Delayed name resolution complete",
				},
			},
		},
	}

	for index, span := range spans {
		// Check that the attempt span has the correct status
		if got, want := spans[index].Status.Code, otelcodes.Ok; got != want {
			t.Errorf("Got status code %v, want %v", got, want)
		}
		// name
		if got, want := span.Name, wantSI[index].name; got != want {
			t.Errorf("Span name is %q, want %q", got, want)
		}
		// spanKind
		if got, want := span.SpanKind.String(), wantSI[index].spanKind; got != want {
			t.Errorf("Got span kind %q, want %q", got, want)
		}
		// attributes
		if got, want := len(span.Attributes), len(wantSI[index].attributes); got != want {
			t.Errorf("Got attributes list of size %q, want %q", got, want)
		}
		for idx, att := range span.Attributes {
			if got, want := att.Key, wantSI[index].attributes[idx].Key; got != want {
				t.Errorf("Got attribute key for span name %q as %q, want %q", span.Name, got, want)
			}
		}
		// events
		if got, want := len(span.Events), len(wantSI[index].events); got != want {
			t.Errorf("Event length is %q, want %q", got, want)
		}
		for eventIdx, event := range span.Events {
			if got, want := event.Name, wantSI[index].events[eventIdx].Name; got != want {
				t.Errorf("Got event name for span name %q as %q, want %q", span.Name, got, want)
			}
			for idx, att := range span.Attributes {
				if got, want := att.Key, wantSI[eventIdx].attributes[idx].Key; got != want {
					t.Errorf("Got attribute key for span name %q with event name %q, as %q, want %q", span.Name, event.Name, got, want)
				}
			}
		}
	}
}

// TestW3CContextPropagator verifies that the W3C Trace Context propagator
// correctly propagates span context between a client and server using the
// headers. It sets up a stub server with  OpenTelemetry tracing enabled
// makes a unary and a streaming RPC, and then, asserts that the correct
// number of spans are created with the expected spans.
func (s) TestW3CContextPropagator(t *testing.T) {
	// Using defaultTraceOptions to set up OpenTelemetry with an in-memory exporter
	traceOptions, spanExporter := defaultTraceOptions(t)
	// Set the W3CContextPropagator as part of TracingOptions.
	traceOptions.TextMapPropagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})

	// Start the server with OpenTelemetry options
	ss := setupStubServer(t, nil, traceOptions)
	defer ss.Stop()
	ctx, _ := otel.Tracer("grpc-open-telemetry").Start(context.Background(), "test-parent-span")
	md, _ := metadata.FromOutgoingContext(ctx)
	ctx = metadata.NewOutgoingContext(ctx, md)
	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics to be emitted, which should be able to be observed
	// through the Metric Reader.
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

	// Get the spans from the exporter
	spans := spanExporter.GetSpans()
	if got, want := len(spans), 6; got != want {
		t.Fatalf("Got %d spans, want %d", got, want)
	}
}

// TestOtelSpanContextPropagation tests the propagation of OpenTelemetry span context across gRPC calls.
func (s) TestOtelSpanContextPropagation(t *testing.T) {
	traceOptions, spanExporter := defaultTraceOptions(t)

	ss := setupStubServer(t, nil, traceOptions)
	defer ss.Stop()

	// Create a parent span for the client call
	ctx, span := otel.Tracer("grpc-open-telemetry").Start(context.Background(), "test-parent-span")
	defer span.End()

	md, _ := metadata.FromOutgoingContext(ctx)
	otel.GetTextMapPropagator().Inject(ctx, otelinternaltracing.NewCustomCarrier(ctx))
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Make a unary RPC
	_, err := ss.Client.UnaryCall(
		ctx,
		&testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte("test")}},
	)
	if err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	// Get the spans from the exporter
	spans := spanExporter.GetSpans()
	if got, want := len(spans), 3; got != want {
		t.Fatalf("Got %d spans, want %d", got, want)
	}

	clientSpan := spans[1]

	if got, want := clientSpan.Status.Code, otelcodes.Ok; got != want {
		t.Errorf("Got client span status code %v, want %v", got, want)
	}

	if len(clientSpan.Events) == 0 {
		t.Error("Client span didn't have any events.")
	} else {

		for _, event := range clientSpan.Events {
			t.Logf("Recorded event: %v", event.Name)
		}

		got := clientSpan.Events[0].Name
		want := "Outbound compressed message"
		if got != want {
			t.Errorf("Client span had unexpected event: got %v, want %v", got, want)
		}
	}

}

// TestCensusToOtelGrpcTraceBinPropagator verifies that tracing information is propagated correctly
// from Census to OpenTelemetry through gRPC.

func (s) TestCensusToOtelGrpcTraceBinPropagator(t *testing.T) {
	traceOptions, spanExporter := defaultTraceOptions(t)

	ss := setupStubServer(t, nil, traceOptions)
	defer ss.Stop()

	ctx, span := otel.Tracer("test").Start(context.Background(), "client-call")
	defer span.End()

	_, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{Body: make([]byte, 10000)}})
	if err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	spans := spanExporter.GetSpans()
	for i, span := range spans {
		t.Logf("Span %d: %s", i, span.Name)
		for _, event := range span.Events {
			t.Logf("  Recorded event: %s", event.Name)
		}
	}

	expectedEventName := "grpc.testing.TestService.UnaryCall"
	foundEvent := false
	for _, span := range spans {
		if span.Name == expectedEventName {
			foundEvent = true
			break
		}
	}

	if !foundEvent {
		t.Errorf("Expected event %q not found in any span events", expectedEventName)
	}
}
