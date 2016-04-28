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
	stop          chan bool
	mu            sync.RWMutex
	lastResetTime time.Time
	histogram     *stats.Histogram
}

func startBenchmarkClientWithSetup(setup *testpb.ClientConfig) (*benchmarkClient, error) {
	var opts []grpc.DialOption

	// Some setup options are ignored:
	// - client type:
	//     will always create sync client
	// - async client threads.
	// - core list
	grpclog.Printf(" * client type: %v (ignored, always creates sync client)", setup.ClientType)
	switch setup.ClientType {
	case testpb.ClientType_SYNC_CLIENT:
	case testpb.ClientType_ASYNC_CLIENT:
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknow client type: %v", setup.ClientType)
	}
	grpclog.Printf(" * async client threads: %v (ignored)", setup.AsyncClientThreads)
	grpclog.Printf(" * core list: %v (ignored)", setup.CoreList)

	grpclog.Printf(" - security params: %v", setup.SecurityParams)
	if setup.SecurityParams != nil {
		creds, err := credentials.NewClientTLSFromFile(Abs(caFile), setup.SecurityParams.ServerHostOverride)
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument, "failed to create TLS credentials %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	grpclog.Printf(" - core limit: %v", setup.CoreLimit)
	// Use one cpu core by default
	numOfCores := 1
	if setup.CoreLimit > 0 {
		numOfCores = int(setup.CoreLimit)
	}
	runtime.GOMAXPROCS(numOfCores)

	grpclog.Printf(" - payload config: %v", setup.PayloadConfig)
	var payloadReqSize, payloadRespSize int
	var payloadType string
	if setup.PayloadConfig != nil {
		switch c := setup.PayloadConfig.Payload.(type) {
		case *testpb.PayloadConfig_BytebufParams:
			opts = append(opts, grpc.WithCodec(byteBufCodec{}))
			payloadReqSize = int(c.BytebufParams.ReqSize)
			payloadRespSize = int(c.BytebufParams.RespSize)
			payloadType = "bytebuf"
		case *testpb.PayloadConfig_SimpleParams:
			payloadReqSize = int(c.SimpleParams.ReqSize)
			payloadRespSize = int(c.SimpleParams.RespSize)
			payloadType = "protobuf"
		case *testpb.PayloadConfig_ComplexParams:
			return nil, grpc.Errorf(codes.Unimplemented, "unsupported payload config: %v", setup.PayloadConfig)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", setup.PayloadConfig)
		}
	}

	grpclog.Printf(" - rpcs per chann: %v", setup.OutstandingRpcsPerChannel)
	grpclog.Printf(" - channel number: %v", setup.ClientChannels)

	rpcCountPerConn, connCount := int(setup.OutstandingRpcsPerChannel), int(setup.ClientChannels)

	grpclog.Printf(" - load params: %v", setup.LoadParams)
	var dist *int
	switch lp := setup.LoadParams.Load.(type) {
	case *testpb.LoadParams_ClosedLoop:
	case *testpb.LoadParams_Poisson:
		grpclog.Printf("   - %v", lp.Poisson)
		return nil, grpc.Errorf(codes.Unimplemented, "unsupported load params: %v", setup.LoadParams)
		// TODO poisson
	case *testpb.LoadParams_Uniform:
		return nil, grpc.Errorf(codes.Unimplemented, "unsupported load params: %v", setup.LoadParams)
	case *testpb.LoadParams_Determ:
		return nil, grpc.Errorf(codes.Unimplemented, "unsupported load params: %v", setup.LoadParams)
	case *testpb.LoadParams_Pareto:
		return nil, grpc.Errorf(codes.Unimplemented, "unsupported load params: %v", setup.LoadParams)
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknown load params: %v", setup.LoadParams)
	}

	grpclog.Printf(" - rpc type: %v", setup.RpcType)
	var rpcType string
	switch setup.RpcType {
	case testpb.RpcType_UNARY:
		rpcType = "unary"
	case testpb.RpcType_STREAMING:
		rpcType = "streaming"
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknown rpc type: %v", setup.RpcType)
	}

	grpclog.Printf(" - histogram params: %v", setup.HistogramParams)
	grpclog.Printf(" - server targets: %v", setup.ServerTargets)

	conns := make([]*grpc.ClientConn, connCount)

	for connIndex := 0; connIndex < connCount; connIndex++ {
		conns[connIndex] = benchmark.NewClientConn(setup.ServerTargets[connIndex%len(setup.ServerTargets)], opts...)
	}

	bc := benchmarkClient{
		histogram: stats.NewHistogram(stats.HistogramOptions{
			NumBuckets:     int(math.Log(setup.HistogramParams.MaxPossible)/math.Log(1+setup.HistogramParams.Resolution)) + 1,
			GrowthFactor:   setup.HistogramParams.Resolution,
			BaseBucketSize: (1 + setup.HistogramParams.Resolution),
			MinValue:       0,
		}),
		stop:          make(chan bool),
		lastResetTime: time.Now(),
	}

	switch rpcType {
	case "unary":
		if dist == nil {
			bc.doCloseLoopUnaryBenchmark(conns, rpcCountPerConn, payloadReqSize, payloadRespSize)
		}
		// TODO else do open loop
	case "streaming":
		if dist == nil {
			bc.doCloseLoopStreamingBenchmark(conns, rpcCountPerConn, payloadReqSize, payloadRespSize, payloadType)
		}
		// TODO else do open loop
	}

	return &bc, nil
}

func (bc *benchmarkClient) doCloseLoopUnaryBenchmark(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int) {
	clients := make([]testpb.BenchmarkServiceClient, len(conns))
	for ic, conn := range conns {
		clients[ic] = testpb.NewBenchmarkServiceClient(conn)
		// Do some warm up.
		for j := 0; j < 10; j++ {
			benchmark.DoUnaryCall(clients[ic], 1, 1)
		}
	}
	for ic, conn := range conns {
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		// Close this connection after all go routines finish.
		var wg sync.WaitGroup
		wg.Add(rpcCountPerConn)
		for j := 0; j < rpcCountPerConn; j++ {
			go func(client testpb.BenchmarkServiceClient) {
				defer wg.Done()
				for {
					done := make(chan bool)
					go func() {
						start := time.Now()
						if err := benchmark.DoUnaryCall(client, reqSize, respSize); err != nil {
							done <- false
							return
						}
						elapse := time.Since(start)
						bc.mu.Lock()
						bc.histogram.Add(int64(elapse / time.Nanosecond))
						bc.mu.Unlock()
						done <- true
					}()
					select {
					case <-bc.stop:
						return
					case <-done:
					}
				}
			}(clients[ic])
		}
		go func(conn *grpc.ClientConn) {
			wg.Wait()
			conn.Close()
		}(conn)
	}
}

func (bc *benchmarkClient) doCloseLoopStreamingBenchmark(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int, payloadType string) {
	var doRPC func(testpb.BenchmarkService_StreamingCallClient, int, int) error
	if payloadType == "bytebuf" {
		doRPC = benchmark.DoByteBufStreamingRoundTrip
	} else {
		doRPC = benchmark.DoStreamingRoundTrip
	}
	streams := make([]testpb.BenchmarkService_StreamingCallClient, len(conns)*rpcCountPerConn)
	for ic, conn := range conns {
		for j := 0; j < rpcCountPerConn; j++ {
			c := testpb.NewBenchmarkServiceClient(conn)
			s, err := c.StreamingCall(context.Background())
			if err != nil {
				grpclog.Fatalf("%v.StreamingCall(_) = _, %v", c, err)
			}
			streams[ic*rpcCountPerConn+j] = s
			// Do some warm up.
			for j := 0; j < 10; j++ {
				doRPC(streams[ic], 1, 1)
			}
		}
	}
	for ic, conn := range conns {
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		// Close this connection after all go routines finish.
		var wg sync.WaitGroup
		wg.Add(rpcCountPerConn)
		for j := 0; j < rpcCountPerConn; j++ {
			go func(stream testpb.BenchmarkService_StreamingCallClient) {
				defer wg.Done()
				for {
					done := make(chan bool)
					go func() {
						start := time.Now()
						if err := doRPC(stream, reqSize, respSize); err != nil {
							done <- false
							return
						}
						elapse := time.Since(start)
						bc.mu.Lock()
						bc.histogram.Add(int64(elapse / time.Nanosecond))
						bc.mu.Unlock()
						done <- true
					}()
					select {
					case <-bc.stop:
						return
					case <-done:
					}
				}
			}(streams[ic*rpcCountPerConn+j])
		}
		go func(conn *grpc.ClientConn) {
			wg.Wait()
			conn.Close()
		}(conn)
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

func (bc *benchmarkClient) reset() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.lastResetTime = time.Now()
	bc.histogram.Clear()
}

func (bc *benchmarkClient) shutdown() {
	close(bc.stop)
}
