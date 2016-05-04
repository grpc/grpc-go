/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package main

import (
	"math"
	"runtime"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

var (
	caFile = "benchmark/server/testdata/ca.pem"
)

type benchmarkClient struct {
	closeConns    func()
	stop          chan bool
	mu            sync.RWMutex
	lastResetTime time.Time
	histogram     *stats.Histogram
}

func printClientConfig(config *testpb.ClientConfig) {
	// Some config options are ignored:
	// - client type:
	//     will always create sync client
	// - async client threads.
	// - core list
	grpclog.Printf(" * client type: %v (ignored, always creates sync client)", config.ClientType)
	grpclog.Printf(" * async client threads: %v (ignored)", config.AsyncClientThreads)
	grpclog.Printf(" * core list: %v (ignored)", config.CoreList)

	grpclog.Printf(" - security params: %v", config.SecurityParams)
	grpclog.Printf(" - core limit: %v", config.CoreLimit)
	grpclog.Printf(" - payload config: %v", config.PayloadConfig)
	grpclog.Printf(" - rpcs per chann: %v", config.OutstandingRpcsPerChannel)
	grpclog.Printf(" - channel number: %v", config.ClientChannels)
	grpclog.Printf(" - load params: %v", config.LoadParams)
	grpclog.Printf(" - rpc type: %v", config.RpcType)
	grpclog.Printf(" - histogram params: %v", config.HistogramParams)
	grpclog.Printf(" - server targets: %v", config.ServerTargets)
}

func setupClientEnv(config *testpb.ClientConfig) {
	// Use one cpu core by default.
	// TODO: Revisit this for the optimal default setup.
	if config.CoreLimit > 1 {
		runtime.GOMAXPROCS(int(config.CoreLimit))
	} else {
		runtime.GOMAXPROCS(1)
	}
}

// createConns creates connections according to given config.
// It returns the connections and corresponding function to close them.
// It returns non-nil error if there is anything wrong.
func createConns(config *testpb.ClientConfig) ([]*grpc.ClientConn, func(), error) {
	var opts []grpc.DialOption

	// Sanity check for client type.
	switch config.ClientType {
	case testpb.ClientType_SYNC_CLIENT:
	case testpb.ClientType_ASYNC_CLIENT:
	default:
		return nil, nil, grpc.Errorf(codes.InvalidArgument, "unknow client type: %v", config.ClientType)
	}

	// Check and set security options.
	if config.SecurityParams != nil {
		creds, err := credentials.NewClientTLSFromFile(abs(caFile), config.SecurityParams.ServerHostOverride)
		if err != nil {
			return nil, nil, grpc.Errorf(codes.InvalidArgument, "failed to create TLS credentials %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	// Use byteBufCodec if it is required.
	if config.PayloadConfig != nil {
		switch config.PayloadConfig.Payload.(type) {
		case *testpb.PayloadConfig_BytebufParams:
			opts = append(opts, grpc.WithCodec(byteBufCodec{}))
		case *testpb.PayloadConfig_SimpleParams:
		default:
			return nil, nil, grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", config.PayloadConfig)
		}
	}

	// Create connections.
	connCount := int(config.ClientChannels)
	conns := make([]*grpc.ClientConn, connCount)
	for connIndex := 0; connIndex < connCount; connIndex++ {
		conns[connIndex] = benchmark.NewClientConn(config.ServerTargets[connIndex%len(config.ServerTargets)], opts...)
	}

	return conns, func() {
		for _, conn := range conns {
			conn.Close()
		}
	}, nil
}

func performRPCs(config *testpb.ClientConfig, conns []*grpc.ClientConn, bc *benchmarkClient) error {
	// Read payload size and type from config.
	var (
		payloadReqSize, payloadRespSize int
		payloadType                     string
	)
	if config.PayloadConfig != nil {
		switch c := config.PayloadConfig.Payload.(type) {
		case *testpb.PayloadConfig_BytebufParams:
			payloadReqSize = int(c.BytebufParams.ReqSize)
			payloadRespSize = int(c.BytebufParams.RespSize)
			payloadType = "bytebuf"
		case *testpb.PayloadConfig_SimpleParams:
			payloadReqSize = int(c.SimpleParams.ReqSize)
			payloadRespSize = int(c.SimpleParams.RespSize)
			payloadType = "protobuf"
		default:
			return grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", config.PayloadConfig)
		}
	}

	// TODO add open loop distribution.
	switch config.LoadParams.Load.(type) {
	case *testpb.LoadParams_ClosedLoop:
	case *testpb.LoadParams_Poisson:
		return grpc.Errorf(codes.Unimplemented, "unsupported load params: %v", config.LoadParams)
	default:
		return grpc.Errorf(codes.InvalidArgument, "unknown load params: %v", config.LoadParams)
	}

	rpcCountPerConn := int(config.OutstandingRpcsPerChannel)

	switch config.RpcType {
	case testpb.RpcType_UNARY:
		bc.doCloseLoopUnary(conns, rpcCountPerConn, payloadReqSize, payloadRespSize)
		// TODO open loop.
	case testpb.RpcType_STREAMING:
		bc.doCloseLoopStreaming(conns, rpcCountPerConn, payloadReqSize, payloadRespSize, payloadType)
		// TODO open loop.
	default:
		return grpc.Errorf(codes.InvalidArgument, "unknown rpc type: %v", config.RpcType)
	}

	return nil
}

func startBenchmarkClient(config *testpb.ClientConfig) (*benchmarkClient, error) {
	printClientConfig(config)

	// Set running environment like how many cores to use.
	setupClientEnv(config)

	conns, closeConns, err := createConns(config)
	if err != nil {
		return nil, err
	}

	bc := &benchmarkClient{
		histogram: stats.NewHistogram(stats.HistogramOptions{
			NumBuckets:     int(math.Log(config.HistogramParams.MaxPossible)/math.Log(1+config.HistogramParams.Resolution)) + 1,
			GrowthFactor:   config.HistogramParams.Resolution,
			BaseBucketSize: (1 + config.HistogramParams.Resolution),
			MinValue:       0,
		}),
		stop:          make(chan bool),
		lastResetTime: time.Now(),
		closeConns:    closeConns,
	}

	if err = performRPCs(config, conns, bc); err != nil {
		// Close all connections if performRPCs failed.
		closeConns()
		return nil, err
	}

	return bc, nil
}

func (bc *benchmarkClient) doCloseLoopUnary(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int) {
	for _, conn := range conns {
		client := testpb.NewBenchmarkServiceClient(conn)
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		for j := 0; j < rpcCountPerConn; j++ {
			go func() {
				// TODO: do warm up if necessary.
				// Now relying on worker client to reserve time to do warm up.
				// The worker client needs to wait for some time after client is created,
				// before starting benchmark.
				done := make(chan bool)
				for {
					go func() {
						start := time.Now()
						if err := benchmark.DoUnaryCall(client, reqSize, respSize); err != nil {
							select {
							case <-bc.stop:
							case done <- false:
							}
							return
						}
						elapse := time.Since(start)
						bc.mu.Lock()
						bc.histogram.Add(int64(elapse))
						bc.mu.Unlock()
						select {
						case <-bc.stop:
						case done <- true:
						}
					}()
					select {
					case <-bc.stop:
						return
					case <-done:
					}
				}
			}()
		}
	}
}

func (bc *benchmarkClient) doCloseLoopStreaming(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int, payloadType string) {
	var doRPC func(testpb.BenchmarkService_StreamingCallClient, int, int) error
	if payloadType == "bytebuf" {
		doRPC = benchmark.DoByteBufStreamingRoundTrip
	} else {
		doRPC = benchmark.DoStreamingRoundTrip
	}
	for _, conn := range conns {
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		for j := 0; j < rpcCountPerConn; j++ {
			c := testpb.NewBenchmarkServiceClient(conn)
			stream, err := c.StreamingCall(context.Background())
			if err != nil {
				grpclog.Fatalf("%v.StreamingCall(_) = _, %v", c, err)
			}
			// Create benchmark rpc goroutine.
			go func() {
				// TODO: do warm up if necessary.
				// Now relying on worker client to reserve time to do warm up.
				// The worker client needs to wait for some time after client is created,
				// before starting benchmark.
				done := make(chan bool)
				for {
					go func() {
						start := time.Now()
						if err := doRPC(stream, reqSize, respSize); err != nil {
							select {
							case <-bc.stop:
							case done <- false:
							}
							return
						}
						elapse := time.Since(start)
						bc.mu.Lock()
						bc.histogram.Add(int64(elapse))
						bc.mu.Unlock()
						select {
						case <-bc.stop:
						case done <- true:
						}
					}()
					select {
					case <-bc.stop:
						return
					case <-done:
					}
				}
			}()
		}
	}
}

func (bc *benchmarkClient) getStats() *testpb.ClientStats {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	timeElapsed := time.Since(bc.lastResetTime).Seconds()

	histogramValue := bc.histogram.Value()
	b := make([]uint32, len(histogramValue.Buckets))
	for i, v := range histogramValue.Buckets {
		b[i] = uint32(v.Count)
	}
	return &testpb.ClientStats{
		Latencies: &testpb.HistogramData{
			Bucket:       b,
			MinSeen:      float64(histogramValue.Min),
			MaxSeen:      float64(histogramValue.Max),
			Sum:          float64(histogramValue.Sum),
			SumOfSquares: float64(histogramValue.SumOfSquares),
			Count:        float64(histogramValue.Count),
		},
		TimeElapsed: timeElapsed,
		TimeUser:    0,
		TimeSystem:  0,
	}
}

// reset clears the contents for histogram and set lastResetTime to Now().
// It is called to get ready for benchmark runs.
func (bc *benchmarkClient) reset() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.lastResetTime = time.Now()
	bc.histogram.Clear()
}

func (bc *benchmarkClient) shutdown() {
	close(bc.stop)
	bc.closeConns()
}
