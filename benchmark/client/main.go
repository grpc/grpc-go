package main

import (
	"flag"
	"math"
	"sync"
	"time"

	"fmt"
	
	"google.golang.org/grpc/benchmark"
	"google.golang.org/grpc/benchmark/stats"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	server = flag.String("server", "", "The server address")
	maxConcurrentRPCs = flag.Int("max_concurrent_rpcs", 1, "The max number of concurrent RPCs")
	duration = flag.Int("duration", math.MaxInt32, "The duration in seconds to run the benchmark client")
)

func caller(client testpb.TestServiceClient) {
	benchmark.DoUnaryCall(client, 1, 1)
}

func closeLoop() {
	s := stats.NewStats(256)
	conn := benchmark.NewClientConn(*server)
	tc := testpb.NewTestServiceClient(conn)
	// Warm up connection.
	for i := 0; i < 100; i++ {
		caller(tc)
	}
	ch := make(chan int, *maxConcurrentRPCs*4)
	var wg sync.WaitGroup
	wg.Add(*maxConcurrentRPCs)

	// Distribute RPCs over maxConcurrentCalls workers.
	for i := 0; i < *maxConcurrentRPCs; i++ {
		go func() {
			for _ = range ch {
				caller(tc)
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
		start := time.Now()
		select {
		case ch <-0:
			s.Add(time.Since(start))
		case <-done:
			ok = false
		}
	}
	close(ch)
	wg.Wait()
	conn.Close()
	fmt.Println(s.String())
}

func main() {
	flag.Parse()
	closeLoop()
}
