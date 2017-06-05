// +build go1.7

package benchmark

import (
	"fmt"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchClientUnary(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	grpc.EnableTracing = true
	runUnary(b, maxConcurrentCalls, reqSize, respSize)
}

func BenchClientStream(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	grpc.EnableTracing = true
	runStream(b, maxConcurrentCalls, reqSize, respSize)
}

func BenchClientUnaryNoTrace(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	grpc.EnableTracing = false
	runUnary(b, maxConcurrentCalls, reqSize, respSize)
}

func BenchClientStreamNoTrace(b *testing.B, maxConcurrentCalls, reqSize, respSize int) {
	grpc.EnableTracing = false
	runStream(b, maxConcurrentCalls, reqSize, respSize)
}

func BenchmarkClient(b *testing.B) {
	benchmarks := []struct {
		maxConcurrentCalls int
		reqSize            int
		respSize           int
	}{
		{1, 1, 1},
		{8, 1, 1},
		{64, 1, 1},
		{512, 1, 1},
		{1, 1, 1024},
		{1, 1024, 1},
		{1, 1024, 1024},
		{8, 1, 1024},
		{8, 1024, 1},
		{8, 1024, 1024},
		{64, 1, 1024},
		{64, 1024, 1},
		{64, 1024, 1024},
		{512, 1, 1024},
		{512, 1024, 1},
		{512, 1024, 1024},
	}

	for _, bm := range benchmarks {
		maxC, reqS, respS := bm.maxConcurrentCalls, bm.reqSize, bm.respSize

		b.Run(fmt.Sprintf("Unary-Trace maxConcurrentCalls: "+
			"%#v, reqSize: %#v, respSize: %#v", maxC, reqS, respS), func(b *testing.B) {
			BenchClientUnary(b, bm.maxConcurrentCalls, bm.reqSize, bm.respSize)
		})

		b.Run(fmt.Sprintf("Stream-Trace maxConcurrentCalls: "+
			"%#v, reqSize: %#v, respSize: %#v", maxC, reqS, respS), func(b *testing.B) {
			BenchClientStream(b, bm.maxConcurrentCalls, bm.reqSize, bm.respSize)
		})

		b.Run(fmt.Sprintf("Unary-NoTrace maxConcurrentCalls: "+
			"%#v, reqSize: %#v, respSize: %#v", maxC, reqS, respS), func(b *testing.B) {
			BenchClientUnaryNoTrace(b, bm.maxConcurrentCalls, bm.reqSize, bm.respSize)
		})

		b.Run(fmt.Sprintf("Stream-NoTrace maxConcurrentCalls: "+
			"%#v, reqSize: %#v, respSize: %#v", maxC, reqS, respS), func(b *testing.B) {
			BenchClientStreamNoTrace(b, bm.maxConcurrentCalls, bm.reqSize, bm.respSize)
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
