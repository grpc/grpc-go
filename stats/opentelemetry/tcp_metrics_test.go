/*
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
 */

package opentelemetry_test

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	estats "google.golang.org/grpc/experimental/stats"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/stats/opentelemetry"
)

// TestTCPMetricsDescriptorsRegistration tests that all 8 A80 TCP metric descriptors are registered in estats.
func TestTCPMetricsDescriptorsRegistration(t *testing.T) {
	tcpMetrics := []string{
		opentelemetry.TCPConnectionsCreatedMetricName,
		opentelemetry.TCPConnectionCountMetricName,
		opentelemetry.TCPMinRTTMetricName,
		opentelemetry.TCPPacketsRetransmittedMetricName,
		opentelemetry.TCPRecurringRetransmitsMetricName,
		opentelemetry.TCPBytesSentMetricName,
		opentelemetry.TCPSyscallWritesMetricName,
		opentelemetry.TCPSyscallReadsMetricName,
	}

	for _, metricName := range tcpMetrics {
		desc := estats.DescriptorForMetric(metricName)
		if desc == nil {
			t.Fatalf("DescriptorForMetric(%q) returned nil, expected registered descriptor", metricName)
		}
		if desc.Name != metricName {
			t.Errorf("Descriptor name = %q, want %q", desc.Name, metricName)
		}
	}
}

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *testServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

// TestTCPMetricsE2E verifies that TCP connection metrics and socket-level metrics
// are recorded by client and server stats handlers over an active gRPC TCP connection.
func TestTCPMetricsE2E(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))

	tcpMetricSet := stats.NewMetricSet(
		opentelemetry.TCPConnectionsCreatedMetricName,
		opentelemetry.TCPConnectionCountMetricName,
		opentelemetry.TCPMinRTTMetricName,
		opentelemetry.TCPPacketsRetransmittedMetricName,
		opentelemetry.TCPRecurringRetransmitsMetricName,
		opentelemetry.TCPBytesSentMetricName,
		opentelemetry.TCPSyscallWritesMetricName,
		opentelemetry.TCPSyscallReadsMetricName,
	)

	serverOpt := opentelemetry.ServerOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       tcpMetricSet,
		},
	})

	clientOpt := opentelemetry.DialOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       tcpMetricSet,
		},
	})

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer(serverOpt)
	testgrpc.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cc, err := grpc.NewClient(lis.Addr().String(), clientOpt, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall failed: %v", err)
	}

	// Close client connection to trigger ConnEnd and record closed connection metrics.
	cc.Close()
	time.Sleep(100 * time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	emittedMetrics := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			emittedMetrics[m.Name] = true
		}
	}

	if !emittedMetrics[opentelemetry.TCPConnectionsCreatedMetricName] {
		t.Errorf("Metric %q was not found in emitted metrics", opentelemetry.TCPConnectionsCreatedMetricName)
	}
	if !emittedMetrics[opentelemetry.TCPConnectionCountMetricName] {
		t.Errorf("Metric %q was not found in emitted metrics", opentelemetry.TCPConnectionCountMetricName)
	}

	if runtime.GOOS == "linux" {
		linuxSocketMetrics := []string{
			opentelemetry.TCPMinRTTMetricName,
			opentelemetry.TCPBytesSentMetricName,
			opentelemetry.TCPSyscallWritesMetricName,
			opentelemetry.TCPSyscallReadsMetricName,
		}
		for _, metricName := range linuxSocketMetrics {
			if !emittedMetrics[metricName] {
				t.Errorf("Socket metric %q was not found in emitted metrics on Linux", metricName)
			}
		}
	}
}
