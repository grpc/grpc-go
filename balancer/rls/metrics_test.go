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

package rls

import (
	"context"
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/internal/stubserver"
	rlstest "google.golang.org/grpc/internal/testutils/rls"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/stats/opentelemetry"
)

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

// TestRLSTargetPickMetric tests RLS Metrics in the case an RLS Balancer picks a
// target from an RLS Response for a RPC. This should emit a
// "grpc.lb.rls.target_picks" with certain labels and cache metrics with certain
// labels.
func (s) TestRLSTargetPickMetric(t *testing.T) {
	// Overwrite the uuid random number generator to be deterministic.
	uuid.SetRand(rand.New(rand.NewSource(1)))
	defer uuid.SetRand(nil)
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil)
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	defer backend.Stop()

	rlsServer.SetResponseCallback(func(context.Context, *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
		return &rlstest.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{backend.Address}}}
	})
	r := startManualResolverWithConfig(t, rlsConfig)
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider: provider,
		Metrics:       opentelemetry.DefaultMetrics().Add("grpc.lb.rls.cache_entries", "grpc.lb.rls.cache_size", "grpc.lb.rls.default_target_picks", "grpc.lb.rls.target_picks", "grpc.lb.rls.failed_picks"),
	}
	grpcTarget := r.Scheme() + ":///"
	cc, err := grpc.NewClient(grpcTarget, grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()), opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.lb.rls.target_picks",
			Description: "EXPERIMENTAL. Number of LB picks sent to each RLS target. Note that if the default target is also returned by the RLS server, RPCs sent to that target from the cache will be counted in this metric, not in grpc.rls.default_target_picks.",
			Unit:        "{pick}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.data_plane_target", backend.Address), attribute.String("grpc.lb.pick_result", "complete")),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},

		// Receives an empty RLS Response, so a single cache entry with no size.
		{
			Name:        "grpc.lb.rls.cache_entries",
			Description: "EXPERIMENTAL. Number of entries in the RLS cache.",
			Unit:        "{entry}",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      1,
					},
				},
			},
		},
		{
			Name:        "grpc.lb.rls.cache_size",
			Description: "EXPERIMENTAL. The current size of the RLS cache.",
			Unit:        "By",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      35,
					},
				},
			},
		},
	}
	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if err != nil {
		t.Fatalf("client.EmptyCall failed with error: %v", err)
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

	// Only one pick was made, which was a target pick, so no default target
	// pick or failed pick metric should emit.
	for _, metric := range []string{"grpc.lb.rls.default_target_picks", "grpc.lb.rls.failed_picks"} {
		if _, ok := gotMetrics[metric]; ok {
			t.Fatalf("Metric %v present in recorded metrics", metric)
		}
	}
}

// TestRLSDefaultTargetPickMetric tests RLS Metrics in the case an RLS Balancer
// falls back to the default target for an RPC. This should emit a
// "grpc.lb.rls.default_target_picks" with certain labels and cache metrics with
// certain labels.
func (s) TestRLSDefaultTargetPickMetric(t *testing.T) {
	// Overwrite the uuid random number generator to be deterministic.
	uuid.SetRand(rand.New(rand.NewSource(1)))
	defer uuid.SetRand(nil)

	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil)
	// Build RLS service config with a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
	backend := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	defer backend.Stop()
	rlsConfig.RouteLookupConfig.DefaultTarget = backend.Address

	r := startManualResolverWithConfig(t, rlsConfig)
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider: provider,
		Metrics:       opentelemetry.DefaultMetrics().Add("grpc.lb.rls.cache_entries", "grpc.lb.rls.cache_size", "grpc.lb.rls.default_target_picks", "grpc.lb.rls.target_picks", "grpc.lb.rls.failed_picks"),
	}
	grpcTarget := r.Scheme() + ":///"
	cc, err := grpc.NewClient(grpcTarget, grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()), opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.lb.rls.default_target_picks",
			Description: "EXPERIMENTAL. Number of LB picks sent to the default target.",
			Unit:        "{pick}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.data_plane_target", backend.Address), attribute.String("grpc.lb.pick_result", "complete")),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		// Receives a RLS Response with target information, so a single cache
		// entry with a certain size.
		{
			Name:        "grpc.lb.rls.cache_entries",
			Description: "EXPERIMENTAL. Number of entries in the RLS cache.",
			Unit:        "{entry}",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      1,
					},
				},
			},
		},
		{
			Name:        "grpc.lb.rls.cache_size",
			Description: "EXPERIMENTAL. The current size of the RLS cache.",
			Unit:        "By",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      0,
					},
				},
			},
		},
	}
	client := testgrpc.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall failed with error: %v", err)
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
	// No target picks and failed pick metrics should be emitted, as the test
	// made only one RPC which recorded as a default target pick.
	for _, metric := range []string{"grpc.lb.rls.target_picks", "grpc.lb.rls.failed_picks"} {
		if _, ok := gotMetrics[metric]; ok {
			t.Fatalf("Metric %v present in recorded metrics", metric)
		}
	}
}

// TestRLSFailedRPCMetric tests RLS Metrics in the case an RLS Balancer fails an
// RPC due to an RLS failure. This should emit a
// "grpc.lb.rls.default_target_picks" with certain labels and cache metrics with
// certain labels.
func (s) TestRLSFailedRPCMetric(t *testing.T) {
	// Overwrite the uuid random number generator to be deterministic.
	uuid.SetRand(rand.New(rand.NewSource(1)))
	defer uuid.SetRand(nil)

	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil)
	// Build an RLS config without a default target.
	rlsConfig := buildBasicRLSConfigWithChildPolicy(t, t.Name(), rlsServer.Address)
	// Register a manual resolver and push the RLS service config through it.
	r := startManualResolverWithConfig(t, rlsConfig)
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	mo := opentelemetry.MetricsOptions{
		MeterProvider: provider,
		Metrics:       opentelemetry.DefaultMetrics().Add("grpc.lb.rls.cache_entries", "grpc.lb.rls.cache_size", "grpc.lb.rls.default_target_picks", "grpc.lb.rls.target_picks", "grpc.lb.rls.failed_picks"),
	}
	grpcTarget := r.Scheme() + ":///"
	cc, err := grpc.NewClient(grpcTarget, grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()), opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: mo}))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.lb.rls.failed_picks",
			Description: "EXPERIMENTAL. Number of LB picks failed due to either a failed RLS request or the RLS channel being throttled.",
			Unit:        "{pick}",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address)),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		// Receives an empty RLS Response, so a single cache entry with no size.
		{
			Name:        "grpc.lb.rls.cache_entries",
			Description: "EXPERIMENTAL. Number of entries in the RLS cache.",
			Unit:        "{entry}",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      1,
					},
				},
			},
		},
		{
			Name:        "grpc.lb.rls.cache_size",
			Description: "EXPERIMENTAL. The current size of the RLS cache.",
			Unit:        "By",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.target", grpcTarget), attribute.String("grpc.lb.rls.server_target", rlsServer.Address), attribute.String("grpc.lb.rls.instance_uuid", "52fdfc07-2182-454f-963f-5f0f9a621d72")),
						Value:      0,
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatalf("client.EmptyCall error = %v, expected a non nil error", err)
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
	// Only one RPC was made, which was a failed pick due to an RLS failure, so
	// no metrics for target picks or default target picks should have emitted.
	for _, metric := range []string{"grpc.lb.rls.target_picks", "grpc.lb.rls.default_target_picks"} {
		if _, ok := gotMetrics[metric]; ok {
			t.Fatalf("Metric %v present in recorded metrics", metric)
		}
	}
}
