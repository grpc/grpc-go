/*
 *
 * Copyright 2016 gRPC authors.
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

package main

import (
	"context"
	"flag"
	"google.golang.org/grpc/internal/grpcrand"
	"math"
	"runtime"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/syscall"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds" // To install the xds resolvers and balancers.
)

var caFile = flag.String("ca_file", "", "The file containing the CA root cert file")

type lockingHistogram struct {
	mu        sync.Mutex
	histogram *stats.Histogram
}

func (h *lockingHistogram) add(value int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.histogram.Add(value)
}

// swap sets h.histogram to o and returns its old value.
func (h *lockingHistogram) swap(o *stats.Histogram) *stats.Histogram {
	h.mu.Lock()
	defer h.mu.Unlock()
	old := h.histogram
	h.histogram = o
	return old
}

func (h *lockingHistogram) mergeInto(merged *stats.Histogram) {
	h.mu.Lock()
	defer h.mu.Unlock()
	merged.Merge(h.histogram)
}

type benchmarkClient struct {
	closeConns        func()
	stop              chan bool
	lastResetTime     time.Time
	histogramOptions  stats.HistogramOptions
	lockingHistograms []lockingHistogram // this is fixed based on number of conns * rpc count, but poisson is dynamic, so either a. don't record or b: append and change construction site
	rusageLastReset   *syscall.Rusage
}

func printClientConfig(config *testpb.ClientConfig) {
	// Some config options are ignored:
	// - client type:
	//     will always create sync client
	// - async client threads.
	// - core list
	logger.Infof(" * client type: %v (ignored, always creates sync client)", config.ClientType)
	logger.Infof(" * async client threads: %v (ignored)", config.AsyncClientThreads)
	// TODO: use cores specified by CoreList when setting list of cores is supported in go.
	logger.Infof(" * core list: %v (ignored)", config.CoreList)

	logger.Infof(" - security params: %v", config.SecurityParams)
	logger.Infof(" - core limit: %v", config.CoreLimit)
	logger.Infof(" - payload config: %v", config.PayloadConfig)
	logger.Infof(" - rpcs per chann: %v", config.OutstandingRpcsPerChannel)
	logger.Infof(" - channel number: %v", config.ClientChannels)
	logger.Infof(" - load params: %v", config.LoadParams)
	logger.Infof(" - rpc type: %v", config.RpcType)
	logger.Infof(" - histogram params: %v", config.HistogramParams)
	logger.Infof(" - server targets: %v", config.ServerTargets)
}

func setupClientEnv(config *testpb.ClientConfig) {
	// Use all cpu cores available on machine by default.
	// TODO: Revisit this for the optimal default setup.
	if config.CoreLimit > 0 {
		runtime.GOMAXPROCS(int(config.CoreLimit))
	} else {
		runtime.GOMAXPROCS(runtime.NumCPU())
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
		return nil, nil, status.Errorf(codes.InvalidArgument, "unknown client type: %v", config.ClientType)
	}

	// Check and set security options.
	if config.SecurityParams != nil {
		if *caFile == "" {
			*caFile = testdata.Path("ca.pem")
		}
		creds, err := credentials.NewClientTLSFromFile(*caFile, config.SecurityParams.ServerHostOverride)
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "failed to create TLS credentials: %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Use byteBufCodec if it is required.
	if config.PayloadConfig != nil {
		switch config.PayloadConfig.Payload.(type) {
		case *testpb.PayloadConfig_BytebufParams:
			opts = append(opts, grpc.WithDefaultCallOptions(grpc.CallCustomCodec(byteBufCodec{})))
		case *testpb.PayloadConfig_SimpleParams:
		default:
			return nil, nil, status.Errorf(codes.InvalidArgument, "unknown payload config: %v", config.PayloadConfig)
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
			return status.Errorf(codes.InvalidArgument, "unknown payload config: %v", config.PayloadConfig)
		}
	}

	// poission: (this distribution has one parameter and is now a fixed equation - learn and figure out)
	// on an poissonUnary - sending an RPC. Does that involve everything else listed here wrt preparing the payload etc?
	// how does it fit in with CloseLoopUnary and streaming?

	// TODO add open loop distribution.

	var poissonLambda *float64 // invariant - if nil then it's using poisson...
	switch t := config.LoadParams.Load.(type) {
	case *testpb.LoadParams_ClosedLoop:
		// does this actually do it's intention? i.e. run rpc, when finish do again?, I see this called once?
		// these areeee only two options though.
	case *testpb.LoadParams_Poisson:
		//this right here needs async rpc starting - or even caller
		if t.Poisson == nil {
			// return error
			return status.Errorf(codes.InvalidArgument, "LoadParams_Poisson.Poisson is nil, needs to be set")
		}
		if t.Poisson.OfferedLoad <= 0 {
			return status.Errorf(codes.InvalidArgument, "LoadParams_Poisson.Poisson is <= 0: %v, needs to be >0", t.Poisson.OfferedLoad)
		}
		// is 0 a valid, I think so will just never send
		// If it's zero, will cause divide by zero I honestly think just return an invalid argument
		// nack it and say won't send rpcs anyway so no point of benchmark:
		// if 0 nack?
		// can this be written to elsewhere
		poissonLambda = &t.Poisson.OfferedLoad // what if this is nil, invalidate?. I think if it's zero break...and just not run anything?

		//

		// look into proto type to get float64 off it, I think it's t := ...(type)
		// return status.Errorf(codes.Unimplemented, "unsupported load params: %v", config.LoadParams)
	default:
		return status.Errorf(codes.InvalidArgument, "unknown load params: %v", config.LoadParams)
	}

	rpcCountPerConn := int(config.OutstandingRpcsPerChannel)

	switch config.RpcType {
	case testpb.RpcType_UNARY:
		bc.doCloseLoopUnary(conns, rpcCountPerConn, payloadReqSize, payloadRespSize, poissonLambda)
		// TODO open loop.
	case testpb.RpcType_STREAMING:
		bc.doCloseLoopStreaming(conns, rpcCountPerConn, payloadReqSize, payloadRespSize, payloadType, poissonLambda)
		// TODO open loop.
	default:
		return status.Errorf(codes.InvalidArgument, "unknown rpc type: %v", config.RpcType)
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

	rpcCountPerConn := int(config.OutstandingRpcsPerChannel)
	bc := &benchmarkClient{
		histogramOptions: stats.HistogramOptions{
			NumBuckets:     int(math.Log(config.HistogramParams.MaxPossible)/math.Log(1+config.HistogramParams.Resolution)) + 1,
			GrowthFactor:   config.HistogramParams.Resolution,
			BaseBucketSize: (1 + config.HistogramParams.Resolution),
			MinValue:       0,
		},
		lockingHistograms: make([]lockingHistogram, rpcCountPerConn*len(conns)), // this is still the same fixed size

		stop:            make(chan bool),
		lastResetTime:   time.Now(),
		closeConns:      closeConns,
		rusageLastReset: syscall.GetRusage(),
	}

	if err = performRPCs(config, conns, bc); err != nil {
		// Close all connections if performRPCs failed.
		closeConns()
		return nil, err
	}

	return bc, nil
}

// func on the client representing? all the same conns except...?

func (bc *benchmarkClient) poisson() {

}

// what part to have the branching logic?
func (bc *benchmarkClient) poissonUnary(client testgrpc.BenchmarkServiceClient, idx int, reqSize int, respSize int, lambda float64) { // this needs to capture all it needs for rpc
	// async RPC based off configuration knobs...close on it for this function?
	go func() {
		// what happens if you error out of the rpc

		start := time.Now()
		if err := benchmark.DoUnaryCall(client, reqSize, respSize); err != nil { // share state? also need to account for results somewhere
			// what do I do with error just ignore or record somewhere?
		}
		elapse := time.Since(start)
		// locking histogram? Seems to be crucial to stats collection...dynamically scale up?
		// bc.lockingHistograms // persisted on the closure type

		// Problem: what to do about idx...
		// once you calculate what rpc to record for it's constant, I think just take it as an int
		bc.lockingHistograms[idx].add(int64(elapse))
	}()
	// start another timer with poissonUnary wrapped in based off
	// grpc rand math.rand exp / lambda passed in

	// protect from infinite recursion because break in between, once this starts continues
	timeBetweenRPCs := time.Duration((grpcrand.ExpFloat64() / lambda) * float64(time.Second))
	time.AfterFunc(timeBetweenRPCs, func(){
		bc.poissonUnary(client, idx, reqSize, respSize, lambda)
	}) // persist it? or keep passing down?
}

// close on this in calling function - I still think you should make branch calling function
func (bc *benchmarkClient) poissonStreaming(stream testgrpc.BenchmarkService_StreamingCallClient, idx int, reqSize int, respSize int, lambda float64, doRPC func(testgrpc.BenchmarkService_StreamingCallClient, int, int) error) {
	// same exact thing as above except doRPC, probably need to figure out when to read the branch
	go func() {
		start := time.Now()
		// Do errors just cause exits? Is this the right behavior?
		// pass this in as an arg to function after lambda or pass in bool and get it at top, I think latter if paramterizing
		if err := doRPC(stream, reqSize, respSize); err != nil {
			// just return and stop doing RPC's? That's behavior of closed loop.
		} // how to unit test this?
		elapse := time.Since(start)
		bc.lockingHistograms[idx].add(int64(elapse))
	}()
	// Can you have two concurrent writes to client? I think so...
	timeBetweenRPCs := time.Duration((grpcrand.ExpFloat64() / lambda) * float64(time.Second))
	time.AfterFunc(timeBetweenRPCs, func(){
		bc.poissonStreaming(stream, idx, reqSize, respSize, lambda, doRPC)
	}())
}

// goroutine (time.AfterFunc())
//     poissonUnary above
//         goroutine rpc
//     create another after func

// this recurses infinetly, how to clean all of these spawned goroutines up


// this does
// for conns
//      for rpcs on conn
//           do rpcs in loop (it's own goroutine)

// does closed || poisson apply to the whole conns/rpcs, It think so, so need this whole loop with same stuff
func (bc *benchmarkClient) doPoissonDistributionUnary(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int, lambda float64) {
	// I think you still need histogram...I think independent enough to warrant it's own function
	for ic, conn := range conns { // ic used for histogram - triage if you really need the histogram for stats collection, logically I think you do...
		client := testgrpc.NewBenchmarkServiceClient(conn) // once converted use for remainder for time including closure
		for j := 0; j < rpcCountPerConn; j++ {
			// poissonUnary() called - needs to be modularized...
			// initial time.AfterFunc() call to get the chain started...

			// Create histogram for each RPC.
			idx := ic * rpcCountPerConn + j
			bc.lockingHistograms[idx].histogram = stats.NewHistogram(bc.histogramOptions) // thus, this applies to whole thing

			// Honestly I think you can branch in the helper if you want to reuse code...

			// Two problems:
			// 1. grpcrand.ExpFloat64() / lambda - is a float64 representing seconds, need to convert to time.Duration of seconds
			// time.Second // will lose it's precision, but only at the nanosecond level since 1 time.Duration is a nanosecond
			// Done...

			// 2. how to give bc.poissonUnary enough data to work if the
			// AfterFunc() type is a func() with no parameters, another way to
			// do this timer...see DNS for example?

			// // will lose it's precision, but only at the nanosecond level since 1 time.Duration is a nanosecond
			// I think float64 has enough digits to represent a time.Second in it's non decimal places...
			timeBetweenRPCs := time.Duration((grpcrand.ExpFloat64() / lambda) * float64(time.Second)) // ping Richard about this, does it make sense to have the first one
			time.AfterFunc(timeBetweenRPCs, func() { // same func will be created up there...
				bc.poissonUnary(client, idx, reqSize, respSize, lambda)
			}) // this handles first rpc
		}
	}
}

// conn (can have multiple)
//      rpc      rpc      rpc
// each of these RPCs does the poisson loop of time between events - only based off the random number generator not the RPCs completing

// Give this to Richard

// how did richard say it unit tests?
// Streaming RPC I think is same thing just a different poissonUnary...


func (bc *benchmarkClient) doCloseLoopUnary(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int, poissonLambda *float64) { // keep the knobs these are derived from
	for ic, conn := range conns {
		client := testgrpc.NewBenchmarkServiceClient(conn)
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		for j := 0; j < rpcCountPerConn; j++ {
			// Create histogram for each goroutine.
			idx := ic*rpcCountPerConn + j
			bc.lockingHistograms[idx].histogram = stats.NewHistogram(bc.histogramOptions)
			// Start goroutine on the created mutex and histogram.
			go func(idx int) { // rpcs per channel
				// TODO: do warm up if necessary.
				// Now relying on worker client to reserve time to do warm up.
				// The worker client needs to wait for some time after client is created,
				// before starting benchmark.

				if poissonLambda == nil/*invariant that determines closed loop*/ { // lambda and lambda are determined here
					// Branch 1: Hit if closed loop parameter
					done := make(chan bool)
					for {
						// right here needs float64()/lambda (either pass in lambda explicitly or pass in config and read)
						// and that triggers a Unary Call which *doesn't block* like this

						go func() {
							start := time.Now()
							if err := benchmark.DoUnaryCall(client, reqSize, respSize); err != nil { // blocks until this returns
								select {
								case <-bc.stop:
								case done <- false:
								}
								return
							}
							elapse := time.Since(start)
							bc.lockingHistograms[idx].add(int64(elapse))
							select {
							case <-bc.stop:
							case done <- true: // sends done to continue on the loop
							}
						}()
						select {
						case <-bc.stop:
							return
						case <-done:
						}
					}
				} else {
					// Branch 2 based off Poisson distribution - pass lambda and etc. in...just pass config?
					// two pieces of data needed from config:
					// 1. branch of poisson vs. closed loop
					// 2. within poisson, the lambda parameter

					// If richard says to start first rpc immediately, switch this to a simple function call:
					// bc.poissonUnary
					timeBetweenRPCs := time.Duration((grpcrand.ExpFloat64() / *poissonLambda) * float64(time.Second)) // ping Richard about this, does it make sense to have the first one
					time.AfterFunc(timeBetweenRPCs, func() { // same func will be created up there...
						bc.poissonUnary(client, idx, reqSize, respSize, *poissonLambda)
					})
				}


			}(idx)
		}
	}
}

func (bc *benchmarkClient) doCloseLoopStreaming(conns []*grpc.ClientConn, rpcCountPerConn int, reqSize int, respSize int, payloadType string, poissonLambda *float64) { // scaleup these functions to take closed || poisson and do (what is here || poisson with afterFunc()) accordingly
	var doRPC func(testgrpc.BenchmarkService_StreamingCallClient, int, int) error
	if payloadType == "bytebuf" { // this branch needs to be reflected (lol) in my event...
		doRPC = benchmark.DoByteBufStreamingRoundTrip
	} else {
		doRPC = benchmark.DoStreamingRoundTrip
	}
	for ic, conn := range conns {
		// For each connection, create rpcCountPerConn goroutines to do rpc.
		for j := 0; j < rpcCountPerConn; j++ {
			c := testgrpc.NewBenchmarkServiceClient(conn)
			stream, err := c.StreamingCall(context.Background())
			if err != nil {
				logger.Fatalf("%v.StreamingCall(_) = _, %v", c, err)
			}
			// Create histogram for each goroutine.
			idx := ic*rpcCountPerConn + j
			bc.lockingHistograms[idx].histogram = stats.NewHistogram(bc.histogramOptions) // already persisted on the object, so will be there when I invoke streaming call
			if poissonLambda == nil {
				// Start goroutine on the created mutex and histogram.
				go func(idx int) {
					// TODO: do warm up if necessary.
					// Now relying on worker client to reserve time to do warm up.
					// The worker client needs to wait for some time after client is created,
					// before starting benchmark.
					for {
						start := time.Now()
						if err := doRPC(stream, reqSize, respSize); err != nil {
							return
						}
						elapse := time.Since(start) // you just need these stats after rpc is performed in goroutine in blocking manner here, I don't see any other way you do this...
						bc.lockingHistograms[idx].add(int64(elapse))
						select {
						case <-bc.stop:
							return
						default:
						}
					}
				}(idx)
			} else {
				timeBetweenRPCs := time.Duration((grpcrand.ExpFloat64() / *poissonLambda) * float64(time.Second)) // ping Richard about this, does it make sense to have the first one
				time.AfterFunc(timeBetweenRPCs, func() { // same func will be created up there...
					bc.poissonStreaming(stream, idx, reqSize, respSize, *poissonLambda, doRPC)
				})
			}
		}
	}
}

// getStats returns the stats for benchmark client.
// It resets lastResetTime and all histograms if argument reset is true.
func (bc *benchmarkClient) getStats(reset bool) *testpb.ClientStats { // this gets called eventually so you def need it
	var wallTimeElapsed, uTimeElapsed, sTimeElapsed float64
	mergedHistogram := stats.NewHistogram(bc.histogramOptions)

	if reset {
		// Merging histogram may take some time.
		// Put all histograms aside and merge later.
		toMerge := make([]*stats.Histogram, len(bc.lockingHistograms))
		for i := range bc.lockingHistograms { // looks at the bc.lockingHistograms type
			toMerge[i] = bc.lockingHistograms[i].swap(stats.NewHistogram(bc.histogramOptions))
		}

		for i := 0; i < len(toMerge); i++ {
			mergedHistogram.Merge(toMerge[i])
		}

		wallTimeElapsed = time.Since(bc.lastResetTime).Seconds()
		latestRusage := syscall.GetRusage()
		uTimeElapsed, sTimeElapsed = syscall.CPUTimeDiff(bc.rusageLastReset, latestRusage)

		bc.rusageLastReset = latestRusage
		bc.lastResetTime = time.Now()
	} else {
		// Merge only, not reset.
		for i := range bc.lockingHistograms {
			bc.lockingHistograms[i].mergeInto(mergedHistogram)
		}

		wallTimeElapsed = time.Since(bc.lastResetTime).Seconds()
		uTimeElapsed, sTimeElapsed = syscall.CPUTimeDiff(bc.rusageLastReset, syscall.GetRusage())
	}

	b := make([]uint32, len(mergedHistogram.Buckets))
	for i, v := range mergedHistogram.Buckets {
		b[i] = uint32(v.Count)
	}
	return &testpb.ClientStats{
		Latencies: &testpb.HistogramData{
			Bucket:       b,
			MinSeen:      float64(mergedHistogram.Min),
			MaxSeen:      float64(mergedHistogram.Max),
			Sum:          float64(mergedHistogram.Sum),
			SumOfSquares: float64(mergedHistogram.SumOfSquares),
			Count:        float64(mergedHistogram.Count),
		},
		TimeElapsed: wallTimeElapsed,
		TimeUser:    uTimeElapsed,
		TimeSystem:  sTimeElapsed,
	}
}

func (bc *benchmarkClient) shutdown() {
	close(bc.stop)
	bc.closeConns()
}
