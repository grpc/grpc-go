package main

import (
	//	"fmt"
	"flag"
	"math"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/grpclog"
)

var (
	server            = flag.String("server", "", "The server address")
	maxConcurrentRPCs = flag.Int("max_concurrent_rpcs", 1, "The max number of concurrent RPCs")
	duration          = flag.Int("duration", math.MaxInt32, "The duration in seconds to run the benchmark client")
	rpcType           = flag.Int("rpc_type", 0,
		`Configure different client rpc type. Valid options are:
		   0 : unary call close loop;
		   1 : streaming call close loop;
		   2 : unary call open loop`)
	targetQps = flag.Int("target_qps", 1000, "The target number of rpcs per second")
	workerNum = flag.Int("worker_number", 1, "The number of workers")
	maxProcs  = flag.Int("max_procs", 2, "The number of operating system threads that can execute user-level Go code simultaneously")
)

func unaryCaller(client testpb.TestServiceClient) {
	benchmark.DoUnaryCall(client, 1, 1)
}

func streamCaller(client testpb.TestServiceClient, stream testpb.TestService_StreamingCallClient) {
	benchmark.DoStreamingRoundTrip(client, stream, 1, 1)
}

func buildConnection() (s *stats.Stats, conn *grpc.ClientConn, tc testpb.TestServiceClient) {
	s = stats.NewStats(256)
	conn = benchmark.NewClientConn(*server)
	tc = testpb.NewTestServiceClient(conn)
	return s, conn, tc
}

// Close loop test for unary call.
func closeLoopUnary() {
	s, conn, tc := buildConnection()
	for i := 0; i < 5000; i++ {
		unaryCaller(tc)
	}
	ch := make(chan int, *maxConcurrentRPCs*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(*maxConcurrentRPCs)

	for i := 0; i < *maxConcurrentRPCs; i++ {
		go func() {
			for _ = range ch {
				start := time.Now()
				unaryCaller(tc)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	// Stop the client when time is up.
	done := make(chan struct{})
	go func() {
		<-time.After(time.Duration(*duration) * time.Second)
		close(done)
	}()
	ok := true
	for ok {
		select {
		case ch <- 0:
		case <-done:
			ok = false
		}
	}
	close(ch)
	wg.Wait()
	conn.Close()
	grpclog.Println(s.String())

}

// Close loop test for streaming call.
func closeLoopStream() {
	s, conn, tc := buildConnection()
	stream, err := tc.StreamingCall(context.Background())
	if err != nil {
		grpclog.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
	}
	for i := 0; i < 100; i++ {
		streamCaller(tc, stream)
	}
	ch := make(chan int, *maxConcurrentRPCs*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(*maxConcurrentRPCs)
	// Distribute RPCs over maxConcurrentCalls workers.
	for i := 0; i < *maxConcurrentRPCs; i++ {
		go func() {
			for _ = range ch {
				start := time.Now()
				streamCaller(tc, stream)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	// Stop the client when time is up.
	done := make(chan struct{})
	go func() {
		<-time.After(time.Duration(*duration) * time.Second)
		close(done)
	}()
	ok := true
	for ok {
		select {
		case ch <- 0:
		case <-done:
			ok = false
		}
	}
	close(ch)
	wg.Wait()
	conn.Close()
	grpclog.Println(s.String())
}

// Open loop test for unary call.
func openLoop() {
	s, conn, tc := buildConnection()
	// Warm up connection.
	for i := 0; i < 10000; i++ {
		unaryCaller(tc)
	}
	var (
		mu sync.Mutex
		wg sync.WaitGroup
		//	delay time.Duration
		count int
	)
	wg.Add(*workerNum)
	ch := make(chan int, 5**workerNum)
	for j := 0; j < *workerNum; j++ {
		go func() {
			for _ = range ch {
				start := time.Now()
				unaryCaller(tc)
				elapse := time.Since(start)
				mu.Lock()
				count++
				s.Add(elapse)
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		<-time.After(time.Duration(*duration) * time.Second)
		close(done)
	}()
	ok := true
	//delay = nextRpcDelay(*targetQps)
	tick := time.Tick(time.Duration(1e9 / *targetQps))
	for ok {
		select {
		case <-tick:
			ch <- 0
			//delay = nextRpcDelay(*targetQps)
			//tick = time.Tick(delay)
			//grpclog.Println("debug: the next delay interval is: ", delay)
		case <-done:
			ok = false
		}
	}

	d := time.Since(startTime)
	actualQps := float64(count) / float64(d/time.Second)
	grpclog.Println("actual qps = ", actualQps)
	close(ch)
	wg.Wait()
	conn.Close()
	grpclog.Println(s.String())
}

func openLoopV3() {
	//	rpcNum := *targetQps * *duration
	s, conn, tc := buildConnection()
	// Warm up connection.
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	//wg.Add(rpcNum)
	for i := 0; i < 5000; i++ {
		unaryCaller(tc)
	}
	startTime := time.Now()
	i := 0
	now := time.Now()
	nextRpc := now
	end := now.Add(time.Duration(*duration) * time.Second)
	for now.Before(time.Time(end)) {
		now = time.Now()
		if now.After(time.Time(nextRpc)) {
			//runtime.LockOSThread()
			wg.Add(1)
			nextRpc = nextRpc.Add(nextRpcDelay(*targetQps))
			go func() {
				start := time.Now()
				unaryCaller(tc)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
				wg.Done()
			}()
			i++
		}
		runtime.Gosched()
	}
	d := time.Since(startTime)
	actualQps := float64(i) / float64(d/time.Second)
	grpclog.Println("actual qps = ", actualQps)
	wg.Wait()
	conn.Close()
	grpclog.Println(s.String())
}

func openLoopV4() {
	rpcNum := *targetQps * *duration
	s, conn, tc := buildConnection()
	// Warm up connection.
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		delay time.Duration
	)
	wg.Add(rpcNum)
	for i := 0; i < 5000; i++ {
		unaryCaller(tc)
	}
	startTime := time.Now()
	i := 0
	//	now := time.Now()
	//nextRpc :=  now
	timer := time.NewTimer(delay)
	for i < rpcNum {
		//now = time.Now()
		select {
		//case i == rpcNum :
		//	break loop
		case <-timer.C:
			//now.After(time.Time(nextRpc)):
			//runtime.LockOSThread()
			//	nextRpc = nextRpc.Add(nextRpcDelay(*targetQps))
			delay = nextRpcDelay(*targetQps)
			timer.Reset(delay)
			go func() {
				start := time.Now()
				unaryCaller(tc)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
				wg.Done()
			}()
			i++

		}
		//	runtime.Gosched()
	}
	d := time.Since(startTime)
	actualQps := float64(rpcNum) / float64(d/time.Second)
	grpclog.Println("actual qps = ", actualQps)
	wg.Wait()
	conn.Close()
	grpclog.Println(s.String())
}

//workerNum := 3
// Generate the next rpc interval.
func nextRpcDelay(targetQps int) time.Duration {
	return time.Duration(rand.ExpFloat64() * float64(time.Second) / float64(targetQps))
}

func main() {
	flag.Parse()
	runtime.GOMAXPROCS(*maxProcs)
	go func() {
		lis, err := net.Listen("tcp", ":0")
		if err != nil {
			grpclog.Fatalf("Failed to listen: %v", err)
		}
		grpclog.Println("Client profiling address: ", lis.Addr().String())
		if err := http.Serve(lis, nil); err != nil {
			grpclog.Fatalf("Failed to serve: %v", err)
		}
	}()
	switch *rpcType {
	case 0:
		closeLoopUnary()
	case 1:
		closeLoopStream()
	case 2:
		openLoop()
	case 3:
		openLoopV3()
	case 4:
		openLoopV4()
	}
}
