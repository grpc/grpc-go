// +build go1.6,!go1.7

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

func runUnary(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf"}, grpc.MaxConcurrentStreams(uint32(maxConcurrentCalls+1)))
	defer stopper()
	conn := NewClientConn(target, grpc.WithInsecure())
	tc := testpb.NewBenchmarkServiceClient(conn)

	// Warm up connection.
	for i := 0; i < 10; i++ {
		unaryCaller(tc, reqSize, respSize)
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
			for range ch {
				start := time.Now()
				unaryCaller(tc, reqSize, respSize)
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

func runStream(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	s := stats.AddStats(b, 38)
	b.StopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf"}, grpc.MaxConcurrentStreams(uint32(maxConcurrentCalls+1)))
	defer stopper()
	conn := NewClientConn(target, grpc.WithInsecure())
	tc := testpb.NewBenchmarkServiceClient(conn)

	// Warm up connection.
	stream, err := tc.StreamingCall(context.Background())
	if err != nil {
		b.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
	}
	for i := 0; i < 10; i++ {
		streamCaller(stream, reqSize, respSize)
	}

	ch := make(chan struct{}, maxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(maxConcurrentCalls)

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for i := 0; i < maxConcurrentCalls; i++ {
		stream, err := tc.StreamingCall(context.Background())
		if err != nil {
			b.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
		}
		go func() {
			for range ch {
				start := time.Now()
				streamCaller(stream, reqSize, respSize)
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
		ch <- struct{}{}
	}
	b.StopTimer()
	close(ch)
	wg.Wait()
	conn.Close()
}
func unaryCaller(client testpb.BenchmarkServiceClient, reqSize, respSize int) {
	if err := DoUnaryCall(client, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoUnaryCall failed: %v", err)
	}
}

func streamCaller(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) {
	if err := DoStreamingRoundTrip(stream, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoStreamingRoundTrip failed: %v", err)
	}
}

func BenchmarkClientStreamc1(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 1, 1, 1)
}

func BenchmarkClientStreamc8(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 8, 1, 1)
}

func BenchmarkClientStreamc64(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 64, 1, 1)
}

func BenchmarkClientStreamc512(b *testing.B) {
	grpc.EnableTracing = true
	runStream(b, 512, 1, 1)
}
func BenchmarkClientUnaryc1(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 1, 1, 1)
}

func BenchmarkClientUnaryc8(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 8, 1, 1)
}

func BenchmarkClientUnaryc64(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 64, 1, 1)
}

func BenchmarkClientUnaryc512(b *testing.B) {
	grpc.EnableTracing = true
	runUnary(b, 512, 1, 1)
}

func BenchmarkClientStreamNoTracec1(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 1, 1, 1)
}

func BenchmarkClientStreamNoTracec8(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 8, 1, 1)
}

func BenchmarkClientStreamNoTracec64(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 64, 1, 1)
}

func BenchmarkClientStreamNoTracec512(b *testing.B) {
	grpc.EnableTracing = false
	runStream(b, 512, 1, 1)
}
func BenchmarkClientUnaryNoTracec1(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 1, 1, 1)
}

func BenchmarkClientUnaryNoTracec8(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 8, 1, 1)
}

func BenchmarkClientUnaryNoTracec64(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 64, 1, 1)
}

func BenchmarkClientUnaryNoTracec512(b *testing.B) {
	grpc.EnableTracing = false
	runUnary(b, 512, 1, 1)
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
