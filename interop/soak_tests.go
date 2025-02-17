/*
*
* Copyright 2014 gRPC authors.
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

package interop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/peer"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// SoakWorkerResults stores the aggregated results for a specific worker during the soak test.
type SoakWorkerResults struct {
	iterationsSucceeded int
	Failures            int
	Latencies           *stats.Histogram
}

// SoakIterationConfig holds the parameters required for a single soak iteration.
type SoakIterationConfig struct {
	RequestSize  int                        // The size of the request payload in bytes.
	ResponseSize int                        // The expected size of the response payload in bytes.
	Client       testgrpc.TestServiceClient // The gRPC client to make the call.
	CallOptions  []grpc.CallOption          // Call options for the RPC.
}

// SoakTestConfig holds the configuration for the entire soak test.
type SoakTestConfig struct {
	RequestSize                      int
	ResponseSize                     int
	PerIterationMaxAcceptableLatency time.Duration
	MinTimeBetweenRPCs               time.Duration
	OverallTimeout                   time.Duration
	ServerAddr                       string
	NumWorkers                       int
	Iterations                       int
	MaxFailures                      int
	ChannelForTest                   func() (*grpc.ClientConn, func())
}

func doOneSoakIteration(ctx context.Context, config SoakIterationConfig) (latency time.Duration, err error) {
	start := time.Now()
	// Do a large-unary RPC.
	// Create the request payload.
	pl := ClientNewPayload(testpb.PayloadType_COMPRESSABLE, config.RequestSize)
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(config.ResponseSize),
		Payload:      pl,
	}
	// Perform the GRPC call.
	var reply *testpb.SimpleResponse
	reply, err = config.Client.UnaryCall(ctx, req, config.CallOptions...)
	if err != nil {
		err = fmt.Errorf("/TestService/UnaryCall RPC failed: %s", err)
		return 0, err
	}
	// Validate response.
	t := reply.GetPayload().GetType()
	s := len(reply.GetPayload().GetBody())
	if t != testpb.PayloadType_COMPRESSABLE || s != config.ResponseSize {
		err = fmt.Errorf("got the reply with type %d len %d; want %d, %d", t, s, testpb.PayloadType_COMPRESSABLE, config.ResponseSize)
		return 0, err
	}
	// Calculate latency and return result.
	latency = time.Since(start)
	return latency, nil
}

func executeSoakTestInWorker(ctx context.Context, config SoakTestConfig, startTime time.Time, workerID int, soakWorkerResults *SoakWorkerResults) {
	timeoutDuration := config.OverallTimeout
	soakIterationsPerWorker := config.Iterations / config.NumWorkers
	if soakWorkerResults.Latencies == nil {
		soakWorkerResults.Latencies = stats.NewHistogram(stats.HistogramOptions{
			NumBuckets:     20,
			GrowthFactor:   1,
			BaseBucketSize: 1,
			MinValue:       0,
		})
	}

	for i := 0; i < soakIterationsPerWorker; i++ {
		if ctx.Err() != nil {
			return
		}
		if time.Since(startTime) >= timeoutDuration {
			fmt.Printf("Test exceeded overall timeout of %v, stopping...\n", config.OverallTimeout)
			return
		}
		earliestNextStart := time.After(config.MinTimeBetweenRPCs)
		currentChannel, cleanup := config.ChannelForTest()
		defer cleanup()
		client := testgrpc.NewTestServiceClient(currentChannel)
		var p peer.Peer
		iterationConfig := SoakIterationConfig{
			RequestSize:  config.RequestSize,
			ResponseSize: config.ResponseSize,
			Client:       client,
			CallOptions:  []grpc.CallOption{grpc.Peer(&p)},
		}
		latency, err := doOneSoakIteration(ctx, iterationConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Worker %d: soak iteration: %d elapsed_ms: %d peer: %v server_uri: %s failed: %s\n", workerID, i, 0, p.Addr, config.ServerAddr, err)
			soakWorkerResults.Failures++
			<-earliestNextStart
			continue
		}
		if latency > config.PerIterationMaxAcceptableLatency {
			fmt.Fprintf(os.Stderr, "Worker %d: soak iteration: %d elapsed_ms: %d peer: %v server_uri: %s exceeds max acceptable latency: %d\n", workerID, i, latency.Milliseconds(), p.Addr, config.ServerAddr, config.PerIterationMaxAcceptableLatency.Milliseconds())
			soakWorkerResults.Failures++
			<-earliestNextStart
			continue
		}
		// Success: log the details of the iteration.
		soakWorkerResults.Latencies.Add(latency.Milliseconds())
		soakWorkerResults.iterationsSucceeded++
		fmt.Fprintf(os.Stderr, "Worker %d: soak iteration: %d elapsed_ms: %d peer: %v server_uri: %s succeeded\n", workerID, i, latency.Milliseconds(), p.Addr, config.ServerAddr)
		<-earliestNextStart
	}
}

// DoSoakTest runs large unary RPCs in a loop for a configurable number of times, with configurable failure thresholds.
// If resetChannel is false, then each RPC will be performed on tc. Otherwise, each RPC will be performed on a new
// stub that is created with the provided server address and dial options.
// TODO(mohanli-ml): Create SoakTestOptions as a parameter for this method.
func DoSoakTest(ctx context.Context, soakConfig SoakTestConfig) {
	if soakConfig.Iterations%soakConfig.NumWorkers != 0 {
		fmt.Fprintf(os.Stderr, "soakIterations must be evenly divisible by soakNumWThreads\n")
	}
	startTime := time.Now()
	var wg sync.WaitGroup
	soakWorkerResults := make([]SoakWorkerResults, soakConfig.NumWorkers)
	for i := 0; i < soakConfig.NumWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			executeSoakTestInWorker(ctx, soakConfig, startTime, workerID, &soakWorkerResults[workerID])
		}(i)
	}
	// Wait for all goroutines to complete.
	wg.Wait()

	// Handle results.
	totalSuccesses := 0
	totalFailures := 0
	latencies := stats.NewHistogram(stats.HistogramOptions{
		NumBuckets:     20,
		GrowthFactor:   1,
		BaseBucketSize: 1,
		MinValue:       0,
	})
	for _, worker := range soakWorkerResults {
		totalSuccesses += worker.iterationsSucceeded
		totalFailures += worker.Failures
		if worker.Latencies != nil {
			// Add latencies from the worker's Histogram to the main latencies.
			latencies.Merge(worker.Latencies)
		}
	}
	var b bytes.Buffer
	totalIterations := totalSuccesses + totalFailures
	latencies.Print(&b)
	fmt.Fprintf(os.Stderr,
		"(server_uri: %s) soak test successes: %d / %d iterations. Total failures: %d. Latencies in milliseconds: %s\n",
		soakConfig.ServerAddr, totalSuccesses, soakConfig.Iterations, totalFailures, b.String())

	if totalIterations != soakConfig.Iterations {
		logger.Fatalf("Soak test consumed all %v of time and quit early, ran %d out of %d iterations.\n", soakConfig.OverallTimeout, totalIterations, soakConfig.Iterations)
	}

	if totalFailures > soakConfig.MaxFailures {
		logger.Fatalf("Soak test total failures: %d exceeded max failures threshold: %d\n", totalFailures, soakConfig.MaxFailures)
	}
	if soakConfig.ChannelForTest != nil {
		_, cleanup := soakConfig.ChannelForTest()
		defer cleanup()
	}
}
