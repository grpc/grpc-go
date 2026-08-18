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
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/stats/opentelemetry"
)

type stressServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *stressServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

func (s *stressServer) FullDuplexCall(stream testgrpc.TestService_FullDuplexCallServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&testpb.StreamingOutputCallResponse{
			Payload: req.GetPayload(),
		}); err != nil {
			return err
		}
	}
}

func setupStressTest(t *testing.T) (*metric.ManualReader, *metric.MeterProvider, *grpc.Server, string) {
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

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer(serverOpt)
	testgrpc.RegisterTestServiceServer(s, &stressServer{})
	go s.Serve(lis)

	return reader, provider, s, lis.Addr().String()
}

// TestTCPMetrics_HighConcurrency_RapidConnectDisconnect stress tests TCP metrics
// under high concurrency and rapid connect/disconnect cycles.
func TestTCPMetrics_HighConcurrency_RapidConnectDisconnect(t *testing.T) {
	reader, provider, s, addr := setupStressTest(t)
	defer s.Stop()

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

	clientOpt := opentelemetry.DialOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       tcpMetricSet,
		},
	})

	const numWorkers = 25
	const iterationsPerWorker = 20
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterationsPerWorker; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				cc, err := grpc.NewClient(addr, clientOpt, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					t.Errorf("Worker %d iter %d: dial failed: %v", workerID, j, err)
					cancel()
					continue
				}

				client := testgrpc.NewTestServiceClient(cc)
				_, err = client.EmptyCall(ctx, &testpb.Empty{})
				if err != nil {
					t.Errorf("Worker %d iter %d: EmptyCall failed: %v", workerID, j, err)
				}
				cc.Close()
				cancel()
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Completed %d connect/RPC/disconnect cycles in %v", numWorkers*iterationsPerWorker, time.Since(start))

	// Allow goroutines and socket close handlers to finish executing.
	time.Sleep(300 * time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Process and validate collected metrics
	sumMetrics := make(map[string]int64)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				var sum int64
				for _, dp := range data.DataPoints {
					sum += dp.Value
					if dp.Value < 0 && m.Name != opentelemetry.TCPConnectionCountMetricName {
						t.Errorf("Metric %s recorded negative value: %d", m.Name, dp.Value)
					}
				}
				sumMetrics[m.Name] += sum
				t.Logf("Metric %s: sum = %d", m.Name, sumMetrics[m.Name])
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					if minVal, ok := dp.Min.Value(); ok {
						t.Logf("Metric %s (Histogram): count=%d, sum=%f, min=%f", m.Name, dp.Count, dp.Sum, minVal)
						if minVal <= 0 {
							t.Errorf("Metric %s min RTT is non-positive: %f", m.Name, minVal)
						}
					}
				}
			}
		}
	}

	// Active connection count must be 0 after all connections close
	if connCount := sumMetrics[opentelemetry.TCPConnectionCountMetricName]; connCount != 0 {
		t.Errorf("Expected active connection count to be 0 after close, got %d", connCount)
	}

	// Total connections created should be > 0
	if created := sumMetrics[opentelemetry.TCPConnectionsCreatedMetricName]; created < int64(numWorkers*iterationsPerWorker) {
		t.Errorf("Expected connections created >= %d, got %d", numWorkers*iterationsPerWorker, created)
	}

	if runtime.GOOS == "linux" {
		// Verify Linux socket metrics were populated and non-negative
		if bytesSent := sumMetrics[opentelemetry.TCPBytesSentMetricName]; bytesSent <= 0 {
			t.Errorf("Expected bytes_sent > 0 on Linux, got %d", bytesSent)
		}
		if syscallWrites := sumMetrics[opentelemetry.TCPSyscallWritesMetricName]; syscallWrites <= 0 {
			t.Errorf("Expected syscall_writes > 0 on Linux, got %d", syscallWrites)
		}
		if syscallReads := sumMetrics[opentelemetry.TCPSyscallReadsMetricName]; syscallReads <= 0 {
			t.Errorf("Expected syscall_reads > 0 on Linux, got %d", syscallReads)
		}
	}
}

// TestTCPMetrics_AbruptCloseWithActiveRPCs tests race conditions when connections
// are closed while RPCs are active.
func TestTCPMetrics_AbruptCloseWithActiveRPCs(t *testing.T) {
	reader, provider, s, addr := setupStressTest(t)
	defer s.Stop()

	tcpMetricSet := stats.NewMetricSet(
		opentelemetry.TCPConnectionsCreatedMetricName,
		opentelemetry.TCPConnectionCountMetricName,
		opentelemetry.TCPMinRTTMetricName,
		opentelemetry.TCPBytesSentMetricName,
		opentelemetry.TCPSyscallWritesMetricName,
		opentelemetry.TCPSyscallReadsMetricName,
	)

	clientOpt := opentelemetry.DialOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       tcpMetricSet,
		},
	})

	const numConns = 40
	var wg sync.WaitGroup

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc, err := grpc.NewClient(addr, clientOpt, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return
			}
			client := testgrpc.NewTestServiceClient(cc)

			streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer streamCancel()

			stream, err := client.FullDuplexCall(streamCtx)
			if err != nil {
				cc.Close()
				return
			}

			// Send payload then abruptly close connection while streaming
			_ = stream.Send(&testpb.StreamingOutputCallRequest{
				Payload: &testpb.Payload{Body: []byte("test-payload-bytes")},
			})

			time.Sleep(5 * time.Millisecond)
			cc.Close()
		}()
	}

	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if data, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range data.DataPoints {
					if dp.Value < 0 && m.Name != opentelemetry.TCPConnectionCountMetricName {
						t.Errorf("Abrupt close metric %s recorded negative value: %d", m.Name, dp.Value)
					}
				}
			}
		}
	}
}

// TestTCPMetrics_MemoryAndGoroutineLeak verifies no memory or goroutine leaks
// occur over repetitive connection lifetimes.
func TestTCPMetrics_MemoryAndGoroutineLeak(t *testing.T) {
	reader, provider, s, addr := setupStressTest(t)
	defer s.Stop()

	tcpMetricSet := stats.NewMetricSet(
		opentelemetry.TCPConnectionsCreatedMetricName,
		opentelemetry.TCPConnectionCountMetricName,
	)

	clientOpt := opentelemetry.DialOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       tcpMetricSet,
		},
	})

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	const cycles = 100
	for i := 0; i < cycles; i++ {
		cc, err := grpc.NewClient(addr, clientOpt, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("Dial failed on cycle %d: %v", i, err)
		}
		client := testgrpc.NewTestServiceClient(cc)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = client.EmptyCall(ctx, &testpb.Empty{})
		cancel()
		cc.Close()
	}

	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Goroutine count: initial=%d, final=%d", initialGoroutines, finalGoroutines)

	// Margin of 5 goroutines allowed for background GC / timer threads
	if finalGoroutines > initialGoroutines+5 {
		t.Errorf("Potential goroutine leak: started with %d, ended with %d", initialGoroutines, finalGoroutines)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
}
