package benchmark

import (
	"os"
	"sync"
	"testing"
	"time"

	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/stats"
)

func run(b *testing.B, maxConcurrentCalls int, caller func(testpb.TestServiceClient)) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer()
	defer stopper()
	conn := NewClientConn(target)
	tc := testpb.NewTestServiceClient(conn)

	// Warm up connection.
	for i := 0; i < 10; i++ {
		caller(tc)
	}

	ch := make(chan int, maxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(maxConcurrentCalls)

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for i := 0; i < maxConcurrentCalls; i++ {
		go func() {
			for _ = range ch {
				start := time.Now()
				caller(tc)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	b.StopTimer()
	close(ch)
	wg.Wait()
	conn.Close()
}

func runStream(b *testing.B, maxConcurrentCalls int, caller func(testpb.TestServiceClient)) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer()
	defer stopper()
	conn := NewClientConn(target)
	tc := testpb.NewTestServiceClient(conn)

	// Warm up connection.
	for i := 0; i < 10; i++ {
		caller(tc)
	}

	ch := make(chan int, maxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(maxConcurrentCalls)

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for i := 0; i < maxConcurrentCalls; i++ {
		go func() {
			for _ = range ch {
				start := time.Now()
				caller(tc)
				elapse := time.Since(start)
				mu.Lock()
				s.Add(elapse)
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	b.StopTimer()
	close(ch)
	wg.Wait()
	conn.Close()
}

func smallCaller(client testpb.TestServiceClient) {
	DoUnaryCall(client, 1, 1)
}

func streamCaller(client testpb.TestServiceClient) {
	//func streamCaller(client testpb.TestServiceClient){
	DoStreamingCall(client, 1, 1)

	//DoStreamingCall(client,1,1)
}

func BenchmarkClientStreamc1(b *testing.B) {
	runStream(b, 1, streamCaller)
}

func BenchmarkClientStreamc8(b *testing.B) {
	runStream(b, 8, streamCaller)
}

func BenchmarkClientStreamc64(b *testing.B) {
	runStream(b, 64, streamCaller)
}

func BenchmarkClientStreamc512(b *testing.B) {
	runStream(b, 512, streamCaller)
}
func BenchmarkClientSmallc1(b *testing.B) {
	run(b, 1, smallCaller)
}

func BenchmarkClientSmallc8(b *testing.B) {
	run(b, 8, smallCaller)
}

func BenchmarkClientSmallc64(b *testing.B) {
	run(b, 64, smallCaller)
}

func BenchmarkClientSmallc512(b *testing.B) {
	run(b, 512, smallCaller)
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
