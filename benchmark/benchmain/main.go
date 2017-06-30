// +build go1.7

/*
 *
 * Copyright 2017 gRPC authors.
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
	"errors"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"flag"
	"strconv"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	bm "google.golang.org/grpc/benchmark"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/latency"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

func runUnary(benchFeatures bm.Features, timeout time.Duration) {
	s := stats.NewStats(38)
	var memStats runtime.MemStats
	nw := &latency.Network{Kbps: benchFeatures.Kbps, Latency: benchFeatures.Latency, MTU: benchFeatures.Mtu}
	target, stopper := bm.StartServer(bm.ServerInfo{Addr: "localhost:0", Type: "protobuf", Network: nw}, grpc.MaxConcurrentStreams(uint32(benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls+1)))
	defer stopper()
	conns := make([]*grpc.ClientConn, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	clients := make([]testpb.BenchmarkServiceClient, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	for ic := 0; ic < benchFeatures.MaxConnCount; ic++ {
		conns[ic] = bm.NewClientConn(
			target, grpc.WithInsecure(),
			grpc.WithDialer(func(address string, timeout time.Duration) (net.Conn, error) {
				return nw.TimeoutDialer(net.DialTimeout)("tcp", address, timeout)
			}),
		)
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		// Warm up.
		for i := 0; i < 10; i++ {
			unaryCaller(tc, benchFeatures.Md, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
		}
		clients[ic] = tc
	}
	ch := make(chan int, benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(benchFeatures.MaxConnCount * benchFeatures.MaxConcurrentCalls)

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for _, tc := range clients {
		for i := 0; i < benchFeatures.MaxConcurrentCalls; i++ {
			go func() {
				for range ch {
					start := time.Now()
					unaryCaller(tc, benchFeatures.Md, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
					elapse := time.Since(start)
					mu.Lock()
					s.Add(elapse)
					mu.Unlock()
				}
				wg.Done()
			}()
		}
	}

	timeoutDur := time.After(timeout)
	timeoutFlag := true
	runtime.ReadMemStats(&memStats)
	startAllocs := memStats.Mallocs
	startBytes := memStats.TotalAlloc
	start := time.Now()
	count := 0
	for timeoutFlag {
		select {
		case <-timeoutDur:
			timeoutFlag = false
			break
		default:
			ch <- 1
			count++
		}
	}

	runtime.ReadMemStats(&memStats)
	close(ch)
	results := testing.BenchmarkResult{N: count, T: time.Now().Sub(start), Bytes: 0, MemAllocs: memStats.Mallocs - startAllocs, MemBytes: memStats.TotalAlloc - startBytes}
	//fmt.Println(mem1)
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
	fmt.Println(results.String(), results.MemString())
	fmt.Println(s.String())
}

func runStream(benchFeatures bm.Features, timeout time.Duration) {
	s := stats.NewStats(38)
	var memStats runtime.MemStats
	nw := &latency.Network{Kbps: benchFeatures.Kbps, Latency: benchFeatures.Latency, MTU: benchFeatures.Mtu}
	target, stopper := bm.StartServer(bm.ServerInfo{Addr: "localhost:0", Type: "protobuf", Network: nw}, grpc.MaxConcurrentStreams(uint32(benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls+1)))
	defer stopper()
	conns := make([]*grpc.ClientConn, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	clients := make([]testpb.BenchmarkServiceClient, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	ctx := metadata.NewContext(context.Background(), benchFeatures.Md)
	for ic := 0; ic < benchFeatures.MaxConnCount; ic++ {
		conns[ic] = bm.NewClientConn(
			target, grpc.WithInsecure(),
			grpc.WithDialer(func(address string, timeout time.Duration) (net.Conn, error) {
				return nw.TimeoutDialer(net.DialTimeout)("tcp", address, timeout)
			}),
		)
		// Warm up connection.
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		stream, err := tc.StreamingCall(ctx)
		if err != nil {
			fmt.Printf("%v.StreamingCall(_) = _, %v", tc, err)
			return
		}
		for i := 0; i < 10; i++ {
			streamCaller(stream, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
		}
		clients[ic] = tc
	}
	ch := make(chan struct{}, benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(benchFeatures.MaxConnCount * benchFeatures.MaxConcurrentCalls)
	// Distribute the b.N calls over maxConcurrentCalls workers.
	for _, tc := range clients {
		for i := 0; i < benchFeatures.MaxConcurrentCalls; i++ {
			stream, err := tc.StreamingCall(ctx)
			if err != nil {
				fmt.Printf("%v.StreamingCall(_) = _, %v", tc, err)
				return
			}
			go func() {
				for range ch {
					start := time.Now()
					streamCaller(stream, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
					elapse := time.Since(start)
					mu.Lock()
					s.Add(elapse)
					mu.Unlock()
				}
				wg.Done()
			}()
		}
	}
	timeoutDur := time.After(timeout)
	timeoutFlag := true
	runtime.ReadMemStats(&memStats)
	startAllocs := memStats.Mallocs
	startBytes := memStats.TotalAlloc
	start := time.Now()
	count := 0
	for timeoutFlag {
		select {
		case <-timeoutDur:
			timeoutFlag = false
			break
		default:
			ch <- struct{}{}
			count++
		}
	}

	runtime.ReadMemStats(&memStats)
	close(ch)
	results := testing.BenchmarkResult{N: count, T: time.Now().Sub(start), Bytes: 0, MemAllocs: memStats.Mallocs - startAllocs, MemBytes: memStats.TotalAlloc - startBytes}
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
	fmt.Println(results.String())
	fmt.Printf(s.String())
}

func unaryCaller(client testpb.BenchmarkServiceClient, md metadata.MD, reqSize, respSize int) {
	if err := bm.DoUnaryCall(client, md, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoUnaryCall failed: %v", err)
	}
}

func streamCaller(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) {
	if err := bm.DoStreamingRoundTrip(stream, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoStreamingRoundTrip failed: %v", err)
	}
}

type intSliceType []int

func (intSlice *intSliceType) String() string {
	return fmt.Sprintf("%v", *intSlice)
}

func (intSlice *intSliceType) Set(value string) error {
	if len(*intSlice) > 0 {
		return errors.New("interval flag already set")
	}
	for _, num := range strings.Split(value, ",") {
		next, err := strconv.Atoi(num)
		if err != nil {
			return err
		}
		*intSlice = append(*intSlice, next)
	}
	return nil
}

func readFromIntSlice(values *[]int, replace intSliceType) {
	if len(replace) == 0 {
		return
	}
	*values = replace
}

func readTimeFromIntSlice(values *[]time.Duration, replace intSliceType) {
	if len(replace) == 0 {
		return
	}
	*values = []time.Duration{}
	for _, t := range replace {
		*values = append(*values, time.Duration(t))
	}
}

func readDataFromFlag(runMode *[]bool, enableTrace *[]bool, md *[]metadata.MD, latency *[]time.Duration, kbps *[]int, mtu *[]int,
	maxConcurrentCalls *[]int, maxConnCount *[]int, reqSizeBytes *[]int, reqspSizeBytes *[]int) {
	var runUnary, runStream bool
	var traceMode, tracexnoTrace bool
	var mdMode, mdxnomd bool
	var readLatency string
	var readKbps, readMtu, readMaxConcurrentCalls, readMaxConnCount, readReqSizeBytes, readReqspSizeBytes intSliceType
	flag.BoolVar(&runUnary, "runUnary", false, "runUnary")
	flag.BoolVar(&runStream, "runStream", false, "runStream")
	flag.BoolVar(&traceMode, "traceMode", false, "traceMode")
	flag.BoolVar(&tracexnoTrace, "tracexnoTrace", false, "tracexnoTrace")
	flag.BoolVar(&mdMode, "mdMode", false, "mdMode")
	flag.BoolVar(&mdxnomd, "mdxnomd", false, "mdxnomd")
	flag.StringVar(&readLatency, "latency", "", "latency")
	flag.Var(&readKbps, "kbps", "kbps")
	flag.Var(&readMtu, "mtu", "mtu")
	flag.Var(&readMaxConcurrentCalls, "maxConcurrentCalls", "maxConcurrentCalls")
	flag.Var(&readMaxConnCount, "maxConnCount", "maxConnCount")
	flag.Var(&readReqSizeBytes, "reqSizeBytes", "reqSizeBytes")
	flag.Var(&readReqspSizeBytes, "reqspSizeBytes", "reqspSizeBytes")
	flag.Parse()
	// If no flags related to mode are set, it runs both by default.
	if runUnary || runStream {
		(*runMode)[0] = runUnary
		(*runMode)[1] = runStream
	}
	// If node flags related to trace are set, it runs trace by default.
	if !tracexnoTrace {
		if traceMode {
			*enableTrace = []bool{true}
		} else {
			*enableTrace = []bool{false}
		}
	}
	// If node flags related to metadate are set, it runs hasMeta by default.
	if !mdxnomd {
		if mdMode {
			*md = []metadata.MD{metadata.New(map[string]string{"key1": "val1"})}
		} else {
			*md = []metadata.MD{{}}
		}
	}
	// Latency has input (time + unit).
	if strings.Compare(readLatency, "") != 0 {
		*latency = []time.Duration{}
		for _, ltc := range strings.Split(readLatency, ",") {
			duration, err := time.ParseDuration(ltc)
			if err != nil {
				fmt.Println(err)
				return
			}
			*latency = append(*latency, duration)
		}
	}
	readFromIntSlice(kbps, readKbps)
	readFromIntSlice(mtu, readMtu)
	readFromIntSlice(maxConcurrentCalls, readMaxConcurrentCalls)
	readFromIntSlice(maxConnCount, readMaxConnCount)
	readFromIntSlice(reqSizeBytes, readReqSizeBytes)
	readFromIntSlice(reqspSizeBytes, readReqspSizeBytes)
}

func main() {
	// runMode{runUnary, runStream}
	runMode := []bool{true, true}
	enableTrace := []bool{true, false}
	md := []metadata.MD{{}, metadata.New(map[string]string{"key1": "val1"})}
	// When set the latency to 0 (no delay), the result is slower than the real result with no delay
	// because latency simulation section has extra operations
	latency := []time.Duration{0, 40 * time.Millisecond} // if non-positive, no delay.
	kbps := []int{0, 10240}                              // if non-positive, infinite
	mtu := []int{0, 512}                                 // if non-positive, infinite
	maxConcurrentCalls := []int{1, 8, 64, 512}
	maxConnCount := []int{1, 4}
	reqSizeBytes := []int{1, 1024, 1024 * 1024}
	respSizeBytes := []int{1, 1024, 1024 * 1024}
	timeout := time.Duration(5 * time.Second)

	readDataFromFlag(&runMode, &enableTrace, &md, &latency, &kbps, &mtu, &maxConcurrentCalls, &maxConnCount, &reqSizeBytes, &respSizeBytes)

	featuresPos := make([]int, 9)
	// 0:enableTracing 1:md 2:ltc 3:kbps 4:mtu 5:maxC 6:connCount 7:reqSize 8:respSize
	featuresNum := []int{len(enableTrace), len(md), len(latency), len(kbps), len(mtu),
		len(maxConcurrentCalls), len(maxConnCount), len(reqSizeBytes), len(respSizeBytes)}

	initalPos := make([]int, len(featuresPos))
	start := true
	for !reflect.DeepEqual(featuresPos, initalPos) || start {
		start = false
		tracing := "Trace"
		if featuresPos[0] == 0 {
			tracing = "noTrace"
		}
		hasMeta := "hasMetadata"
		if featuresPos[1] == 0 {
			hasMeta = "noMetadata"
		}
		benchFeature := bm.Features{
			EnableTrace:        enableTrace[featuresPos[0]],
			Md:                 md[featuresPos[1]],
			Latency:            latency[featuresPos[2]],
			Kbps:               kbps[featuresPos[3]],
			Mtu:                mtu[featuresPos[4]],
			MaxConcurrentCalls: maxConcurrentCalls[featuresPos[5]],
			MaxConnCount:       maxConnCount[featuresPos[6]],
			ReqSizeBytes:       reqSizeBytes[featuresPos[7]],
			RespSizeBytes:      respSizeBytes[featuresPos[8]],
		}

		grpc.EnableTracing = enableTrace[featuresPos[0]]
		if runMode[0] {
			fmt.Printf("Unary-%s-%s-%s: \n", tracing, hasMeta, benchFeature.String())
			runUnary(benchFeature, timeout)
		}
		if runMode[1] {
			fmt.Printf("Stream-%s-%s-%s\n", tracing, hasMeta, benchFeature.String())
			runStream(benchFeature, timeout)
		}

		bm.AddOne(featuresPos, featuresNum)
	}

}
