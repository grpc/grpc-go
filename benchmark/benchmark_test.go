package benchmark

import (
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/grpclog"
)

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func runUnary(b *testing.B, connCount, rpcCountPerConn int) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf"})
	defer stopper()

	conns := make([]*grpc.ClientConn, connCount, connCount)
	clients := make([]testpb.BenchmarkServiceClient, connCount, connCount)
	for ic := 0; ic < connCount; ic++ {
		conns[ic] = NewClientConn(target, grpc.WithInsecure())
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		// Warm up connection.
		for i := 0; i < 10; i++ {
			unaryCaller(tc)
		}
		clients[ic] = tc
	}
	ch := make(chan int, connCount*rpcCountPerConn*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(connCount * rpcCountPerConn)

	// Distribute the b.N calls over rpcCountPerConn workers.
	for _, tc := range clients {
		for i := 0; i < rpcCountPerConn; i++ {
			go func() {
				for range ch {
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
	}
	// Exclude firat and last min(1.5*totalCount, 1/10*b.N) rpcs when calculating QPS.
	qpsStartN := min(connCount*rpcCountPerConn*3/2, b.N/10)
	qpsEndN := b.N - qpsStartN
	b.ResetTimer()
	b.StartTimer()
	for i := 0; i < qpsStartN; i++ {
		ch <- i
	}
	s.StartQPS()
	for i := qpsStartN; i < qpsEndN; i++ {
		ch <- i
	}
	s.EndQPS()
	for i := qpsEndN; i < b.N; i++ {
		ch <- i
	}
	b.StopTimer()
	close(ch)
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
}

func runStream(b *testing.B, connCount, rpcCountPerConn int) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf"})
	defer stopper()

	conns := make([]*grpc.ClientConn, connCount, connCount)
	clients := make([]testpb.BenchmarkServiceClient, connCount, connCount)
	for ic := 0; ic < connCount; ic++ {
		conns[ic] = NewClientConn(target, grpc.WithInsecure())
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		// Warm up connection.
		stream, err := tc.StreamingCall(context.Background())
		if err != nil {
			b.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
		}
		for i := 0; i < 10; i++ {
			streamCaller(stream)
		}
		clients[ic] = tc
	}
	ch := make(chan int, connCount*rpcCountPerConn*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(connCount * rpcCountPerConn)

	// Distribute the b.N calls over rpcCountPerConn workers.
	for _, tc := range clients {
		for i := 0; i < rpcCountPerConn; i++ {
			go func() {
				stream, err := tc.StreamingCall(context.Background())
				if err != nil {
					b.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
				}
				for range ch {
					start := time.Now()
					streamCaller(stream)
					elapse := time.Since(start)
					mu.Lock()
					s.Add(elapse)
					mu.Unlock()
				}
				wg.Done()
			}()
		}
	}
	// Exclude firat and last min(1.5*totalCount, 1/10*b.N) rpcs when calculating QPS.
	qpsStartN := min(connCount*rpcCountPerConn*3/2, b.N/10)
	qpsEndN := b.N - qpsStartN
	b.ResetTimer()
	b.StartTimer()
	for i := 0; i < qpsStartN; i++ {
		ch <- i
	}
	s.StartQPS()
	for i := qpsStartN; i < qpsEndN; i++ {
		ch <- i
	}
	s.EndQPS()
	for i := qpsEndN; i < b.N; i++ {
		ch <- i
	}
	b.StopTimer()
	close(ch)
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
}

func unaryCaller(client testpb.BenchmarkServiceClient) {
	if err := DoUnaryCall(client, 1, 1); err != nil {
		grpclog.Fatalf("DoUnaryCall failed: %v", err)
	}
}

func streamCaller(stream testpb.BenchmarkService_StreamingCallClient) {
	if err := DoStreamingRoundTrip(stream, 1, 1); err != nil {
		grpclog.Fatalf("DoStreamingRoundTrip failed: %v", err)
	}
}

func BenchmarkClientStreamc1(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 1, 1)
}

func BenchmarkClientStreamc8(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 1, 8)
}

func BenchmarkClientStreamc64(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 1, 64)
}

func BenchmarkClientStreamc100x64(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 100, 64)
}

func BenchmarkClientStreamc512(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 1, 512)
}
func BenchmarkClientUnaryc1(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 1, 1)
}

func BenchmarkClientUnaryc8(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 1, 8)
}

func BenchmarkClientUnaryc64(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 1, 64)
}

func BenchmarkClientUnaryc100x64(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 100, 64)
}

func BenchmarkClientUnaryc512(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 1, 512)
}

func BenchmarkClientStreamNoTracec1(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 1, 1)
}

func BenchmarkClientStreamNoTracec8(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 1, 8)
}

func BenchmarkClientStreamNoTracec64(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 1, 64)
}

func BenchmarkClientStreamNoTracec100x64(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 100, 64)
}

func BenchmarkClientStreamNoTracec512(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 1, 512)
}
func BenchmarkClientUnaryNoTracec1(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 1, 1)
}

func BenchmarkClientUnaryNoTracec8(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 1, 8)
}

func BenchmarkClientUnaryNoTracec64(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 1, 64)
}

func BenchmarkClientUnaryNoTracec100x64(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 100, 64)
}

func BenchmarkClientUnaryNoTracec512(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 1, 512)
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
