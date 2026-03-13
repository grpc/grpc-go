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

package pickfirst_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/stats"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/stats/opentelemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

var pfConfig string

func init() {
	pfConfig = fmt.Sprintf(`{
  		"loadBalancingConfig": [
    		{
      			%q: {
      		}
    	}
  	]
	}`, pickfirst.Name)
}

// TestPickFirstMetrics tests pick first metrics. It configures a pick first
// balancer, causes it to connect and then disconnect, and expects the
// subsequent metrics to emit from that.
func (s) TestPickFirstMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	ss.StartServer()
	defer ss.Stop()

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(pfConfig)

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		ServiceConfig: sc,
		Addresses:     []resolver.Address{{Addr: ss.Address}}},
	)

	tmr := stats.NewTestMetricsRecorder()
	cc, err := grpc.NewClient(r.Scheme()+":///", grpc.WithStatsHandler(tmr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("NewClient() failed with error: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_failed", got, 0)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}

	// Checking for subchannel metrics as well
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_failed"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_failed", got, 0)
	}
	if got, _ := tmr.Metric("grpc.subchannel.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.disconnections", got, 0)
	}
	if got, _ := tmr.Metric("grpc.subchannel.open_connections"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.open_connections", got, 1)
	}

	ss.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 1)
	}
	if got, _ := tmr.Metric("grpc.subchannel.disconnections"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.disconnections", got, 1)
	}
	if got, _ := tmr.Metric("grpc.subchannel.open_connections"); got != -1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.open_connections", got, -1)
	}
}

// TestPickFirstMetricsFailure tests the connection attempts failed metric. It
// configures a channel and scenario that causes a pick first connection attempt
// to fail, and then expects that metric to emit.
func (s) TestPickFirstMetricsFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(pfConfig)

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		ServiceConfig: sc,
		Addresses:     []resolver.Address{{Addr: "bad address"}}},
	)
	grpcTarget := r.Scheme() + ":///"
	tmr := stats.NewTestMetricsRecorder()
	cc, err := grpc.NewClient(grpcTarget, grpc.WithStatsHandler(tmr), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("NewClient() failed with error: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatalf("EmptyCall() passed when expected to fail")
	}

	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 0)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_failed", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}
}

// TestPickFirstMetricsE2E tests the pick first metrics end to end. It
// configures a channel with an OpenTelemetry plugin, induces all 3 pick first
// metrics to emit, and makes sure the correct OpenTelemetry metrics atoms emit.
func (s) TestPickFirstMetricsE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	ss.StartServer()
	defer ss.Stop()

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(pfConfig)
	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		ServiceConfig: sc,
		Addresses:     []resolver.Address{{Addr: "bad address"}}},
	) // Will trigger connection failed.

	grpcTarget := r.Scheme() + ":///"
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider: provider,
		Metrics:       opentelemetry.DefaultMetrics().Add("grpc.lb.pick_first.disconnections", "grpc.lb.pick_first.connection_attempts_succeeded", "grpc.lb.pick_first.connection_attempts_failed"),
	}

	cc, err := grpc.NewClient(grpcTarget, opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("NewClient() failed with error: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatalf("EmptyCall() passed when expected to fail")
	}

	r.UpdateState(resolver.State{
		ServiceConfig: sc,
		Addresses:     []resolver.Address{{Addr: ss.Address}},
	}) // Will trigger successful connection metric.
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Stop the server, that should send signal to disconnect, which will
	// eventually emit disconnection metric before ClientConn goes IDLE.
	ss.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)
	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.lb.pick_first.connection_attempts_succeeded",
			Description: "EXPERIMENTAL. Number of successful connection attempts.",
			Unit:        "{attempt}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget)),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.lb.pick_first.connection_attempts_failed",
			Description: "EXPERIMENTAL. Number of failed connection attempts.",
			Unit:        "{attempt}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget)),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.lb.pick_first.disconnections",
			Description: "EXPERIMENTAL. Number of times the selected subchannel becomes disconnected.",
			Unit:        "{disconnection}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget)),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
	}

	gotMetrics := metricsDataFromReader(ctx, reader)
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

func (s) TestDisconnectLabel(t *testing.T) {
	// 1. Valid GOAWAY
	// Server GracefulStop sends GOAWAY with active streams = 0.
	// This usually sends NoError(0) code.
	t.Run("GoAway", func(t *testing.T) {
		runDisconnectLabelTest(t, "GOAWAY NO_ERROR", func(ss *stubserver.StubServer) {
			ss.S.GracefulStop()
			// GracefulStop waits for connections to close, which happens after
			// GOAWAY is sent.
		})
	})

	// 2. IO Error
	// Server Stop closes the listener and active connections immediately.
	// This often results in "connection reset" or "EOF" (unknown) depending on timing/OS.
	// Let's check for "unknown" or "connection reset" or "subchannel shutdown" strictly.
	// In this test env, it often results in io.EOF which we mapped to "unknown".
	t.Run("IO_Error", func(t *testing.T) {
		runDisconnectLabelTest(t, "unknown", func(ss *stubserver.StubServer) {
			ss.Stop()
		})
	})

	// Scenario 3: Unknown (Client closes - voluntary? actually client close might be UNKNOWN or not recorded as split)
	// If client closes, we might not record "disconnections" metric from ClientConn perspective?
	// disconnections metric is "Number of times the selected subchannel becomes disconnected".
	// If we close 'cc', we tear down subchannels.
	// But let's try to trigger a case where we just disconnect without server side action?
	// Or maybe "unknown" is what we get for "Idle" timeout?
	// Let's stick to IO and GoAway first which are explicit in A94.
}

func runDisconnectLabelTest(t *testing.T, wantLabel string, triggerFunc func(*stubserver.StubServer)) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	ss.StartServer()
	defer ss.Stop() // Cleanup in case triggerFunc didn't fully stop or strict cleanup needed

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(pfConfig)
	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{
		ServiceConfig: sc,
		Addresses:     []resolver.Address{{Addr: ss.Address}},
	})

	grpcTarget := r.Scheme() + ":///"
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider:  provider,
		Metrics:        opentelemetry.DefaultMetrics().Add("grpc.subchannel.disconnections"),
		OptionalLabels: []string{"grpc.disconnect_error"},
	}

	cc, err := grpc.NewClient(grpcTarget, opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer cc.Close()

	tsc := testgrpc.NewTestServiceClient(cc)
	// Ensure connected
	if _, err := tsc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Trigger disconnection
	triggerFunc(ss)

	// Wait for Idle state (disconnection happened)
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Verify metrics

	gotMetrics := metricsDataFromReader(ctx, reader)
	val, ok := gotMetrics["grpc.subchannel.disconnections"]
	if !ok {
		t.Fatalf("Metric grpc.subchannel.disconnections not found")
	}

	// We used AssertEqual in the other test, let's use it here too if available.
	// But checking attributes manually might be safer if AssertEqual is strict on other optional fields.
	// Let's iterate datapoints.
	start := time.Now()
	for {
		points := val.Data.(metricdata.Sum[int64]).DataPoints
		if len(points) == 0 {
			t.Fatalf("No data points for disconnections")
		}
		dp := points[0]
		// Check attributes
		seenLabel := false
		var foundAttrs []string
		for _, kv := range dp.Attributes.ToSlice() {
			foundAttrs = append(foundAttrs, fmt.Sprintf("%s=%s", kv.Key, kv.Value.AsString()))
			if kv.Key == "grpc.disconnect_error" {
				seenLabel = true
				if kv.Value.AsString() != wantLabel {
					t.Errorf("Want label %q, got %q", wantLabel, kv.Value.AsString())
				}
			}
		}
		if !seenLabel && wantLabel != "" {
			if time.Since(start) > time.Second {
				t.Errorf("Label grpc.disconnect_error missing. Found attributes: %v", foundAttrs)
			}
		}
		if seenLabel || time.Since(start) > time.Second {
			break
		}
		time.Sleep(10 * time.Millisecond)
		gotMetrics = metricsDataFromReader(ctx, reader)
		val = gotMetrics["grpc.subchannel.disconnections"]
	}
}
