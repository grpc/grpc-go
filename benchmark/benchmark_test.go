package benchmark

import (
	"sync"
	"testing"

	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func run(b *testing.B, maxConcurrentCalls int, caller func(testpb.TestServiceClient)) {
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
	var wg sync.WaitGroup
	wg.Add(maxConcurrentCalls)

	b.StartTimer()

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for i := 0; i < maxConcurrentCalls; i++ {
		go func() {
			for _ = range ch {
				caller(tc)
			}
			wg.Done()
		}()
	}
	for i := 0; i < b.N; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
	b.StopTimer()
	conn.Close()
}

func smallCaller(client testpb.TestServiceClient) {
	DoUnaryCall(client, 1, 1)
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
