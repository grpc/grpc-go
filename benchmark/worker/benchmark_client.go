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
	caFile = "/usr/local/google/home/menghanl/go/src/google.golang.org/grpc/benchmark/server/testdata/ca.pem"
)

type benchmarkClient struct {
	conns                []*grpc.ClientConn
	histogramGrowFactor  float64
	histogramMaxPossible float64
	stop                 chan bool
	mu                   sync.RWMutex
	lastResetTime        time.Time
	histogram            *stats.Histogram
}

func startBenchmarkClientWithSetup(setup *testpb.ClientConfig) (*benchmarkClient, error) {
	var opts []grpc.DialOption

	grpclog.Printf(" - client type: %v", setup.ClientType)
	switch setup.ClientType {
	// Ignore client type
	case testpb.ClientType_SYNC_CLIENT:
	case testpb.ClientType_ASYNC_CLIENT:
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknow client type: %v", setup.ClientType)
	}

	grpclog.Printf(" - security params: %v", setup.SecurityParams)
	if setup.SecurityParams != nil {
		creds, err := credentials.NewClientTLSFromFile(caFile, setup.SecurityParams.ServerHostOverride)
		if err != nil {
			grpclog.Fatalf("failed to create TLS credentials %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	// Ignore async client threads.

	grpclog.Printf(" - core limit: %v", setup.CoreLimit)
	if setup.CoreLimit > 0 {
		runtime.GOMAXPROCS(int(setup.CoreLimit))
	} else {
		// runtime.GOMAXPROCS(runtime.NumCPU())
		runtime.GOMAXPROCS(1)
	}

	// TODO payload config
	grpclog.Printf(" - payload config: %v", setup.PayloadConfig)
	var payloadReqSize, payloadRespSize int
	var payloadType string
	if setup.PayloadConfig != nil {
		// TODO payload config
		grpclog.Printf("payload config: %v", setup.PayloadConfig)
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
			return nil, grpc.Errorf(codes.InvalidArgument, "unsupported payload config: %v", setup.PayloadConfig)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", setup.PayloadConfig)
		}
	}

	// TODO core list
	grpclog.Printf(" - core list: %v", setup.CoreList)

	grpclog.Printf(" - histogram params: %v", setup.HistogramParams)
	grpclog.Printf(" - server targets: %v", setup.ServerTargets)
	grpclog.Printf(" - rpcs per chann: %v", setup.OutstandingRpcsPerChannel)
	grpclog.Printf(" - channel number: %v", setup.ClientChannels)

	rpcCount, connCount := int(setup.OutstandingRpcsPerChannel), int(setup.ClientChannels)

	bc := &benchmarkClient{
		conns:                make([]*grpc.ClientConn, connCount),
		histogramGrowFactor:  setup.HistogramParams.Resolution,
		histogramMaxPossible: setup.HistogramParams.MaxPossible,
	}

	for connIndex := 0; connIndex < connCount; connIndex++ {
		bc.conns[connIndex] = benchmark.NewClientConn(setup.ServerTargets[connIndex%len(setup.ServerTargets)], opts...)
	}

	bc.histogram = stats.NewHistogram(stats.HistogramOptions{
		NumBuckets:   int(math.Log(bc.histogramMaxPossible)/math.Log(1+bc.histogramGrowFactor)) + 1,
		GrowthFactor: bc.histogramGrowFactor,
		MinValue:     0,
	})

	grpclog.Printf(" - load params: %v", setup.LoadParams)
	switch lp := setup.LoadParams.Load.(type) {
	case *testpb.LoadParams_ClosedLoop:
		grpclog.Printf("   - %v", lp.ClosedLoop)
	case *testpb.LoadParams_Poisson:
		grpclog.Printf("   - %v", lp.Poisson)
	case *testpb.LoadParams_Uniform:
		grpclog.Printf("   - %v", lp.Uniform)
	case *testpb.LoadParams_Determ:
		grpclog.Printf("   - %v", lp.Determ)
	case *testpb.LoadParams_Pareto:
		grpclog.Printf("   - %v", lp.Pareto)
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknown load params: %v", setup.LoadParams)
	}

	grpclog.Printf(" - rpc type: %v", setup.RpcType)
	bc.stop = make(chan bool)
	switch setup.RpcType {
	case testpb.RpcType_UNARY:
		doCloseLoopUnaryBenchmark(bc.histogram, bc.conns, rpcCount, payloadReqSize, payloadRespSize, bc.stop)
	case testpb.RpcType_STREAMING:
		doCloseLoopStreamingBenchmark(bc.histogram, bc.conns, rpcCount, payloadReqSize, payloadRespSize, payloadType, bc.stop)
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknown rpc type: %v", setup.RpcType)
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.lastResetTime = time.Now()
	return bc, nil
}

func doCloseLoopUnaryBenchmark(h *stats.Histogram, conns []*grpc.ClientConn, rpcCount int, reqSize int, respSize int, stop <-chan bool) {

	clients := make([]testpb.BenchmarkServiceClient, len(conns))
	for ic, conn := range conns {
		clients[ic] = testpb.NewBenchmarkServiceClient(conn)
		for j := 0; j < 100/len(conns); j++ {
			benchmark.DoUnaryCall(clients[ic], reqSize, respSize)
		}
	}
	var mu sync.Mutex
	for ic, _ := range conns {
		for j := 0; j < rpcCount; j++ {
			go func() {
				for {
					done := make(chan bool)
					go func() {
						start := time.Now()
						benchmark.DoUnaryCall(clients[ic], reqSize, respSize)
						elapse := time.Since(start)
						mu.Lock()
						h.Add(int64(elapse / time.Nanosecond))
						mu.Unlock()
						done <- true
					}()
					select {
					case <-stop:
						grpclog.Printf("stopped")
						return
					case <-done:
					}
				}
			}()
		}
	}
	grpclog.Printf("close loop done, count: %v", rpcCount)
}

func doCloseLoopStreamingBenchmark(h *stats.Histogram, conns []*grpc.ClientConn, rpcCount int, reqSize int, respSize int, payloadType string, stop <-chan bool) {
	var doRPC func(testpb.BenchmarkService_StreamingCallClient, int, int)
	if payloadType == "bytebuf" {
		doRPC = benchmark.DoGenericStreamingRoundTrip
	} else {
		doRPC = benchmark.DoStreamingRoundTrip
	}
	streams := make([]testpb.BenchmarkService_StreamingCallClient, len(conns))
	for ic, conn := range conns {
		c := testpb.NewBenchmarkServiceClient(conn)
		s, err := c.StreamingCall(context.Background())
		if err != nil {
			grpclog.Fatalf("%v.StreamingCall(_) = _, %v", c, err)
		}
		streams[ic] = s
		for j := 0; j < 100/len(conns); j++ {
			doRPC(streams[ic], reqSize, respSize)
		}
	}
	var mu sync.Mutex
	for ic, _ := range conns {
		for j := 0; j < rpcCount; j++ {
			go func() {
				for {
					done := make(chan bool)
					go func() {
						start := time.Now()
						doRPC(streams[ic], reqSize, respSize)
						elapse := time.Since(start)
						mu.Lock()
						h.Add(int64(elapse / time.Nanosecond))
						mu.Unlock()
						done <- true
					}()
					select {
					case <-stop:
						grpclog.Printf("stopped")
						return
					case <-done:
					}
				}
			}()
		}
	}
	grpclog.Printf("close loop done, count: %v", rpcCount)
}

func (bc *benchmarkClient) getStats() *testpb.ClientStats {
	bc.mu.RLock()
	// time.Sleep(1 * time.Second)
	defer bc.mu.RUnlock()
	histogramValue := bc.histogram.Value()
	b := make([]uint32, len(histogramValue.Buckets))
	tempCount := make(map[int64]int)
	for i, v := range histogramValue.Buckets {
		b[i] = uint32(v.Count)
		tempCount[v.Count] += 1
	}
	grpclog.Printf("+++++\n%v count: %v\n+++++", tempCount, histogramValue.Count)
	return &testpb.ClientStats{
		Latencies: &testpb.HistogramData{
			Bucket:  b,
			MinSeen: float64(histogramValue.Min),
			MaxSeen: float64(histogramValue.Max),
			Sum:     float64(histogramValue.Sum),
			// TODO change to squares
			SumOfSquares: float64(histogramValue.Sum),
			Count:        float64(histogramValue.Count),
		},
		TimeElapsed: time.Since(bc.lastResetTime).Seconds(),
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
