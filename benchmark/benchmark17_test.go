// +build go1.7

package benchmark

import (
	"fmt"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchClient(b *testing.B, maxConcurrentCalls, reqSize, respSize int, enableTrace, unaryMode bool) {
	if enableTrace {
		grpc.EnableTracing = true
	}
	if unaryMode {
		runUnary(b, maxConcurrentCalls, reqSize, respSize)
	} else {
		runStream(b, maxConcurrentCalls, reqSize, respSize)
	}
}

/*
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
*/
func BenchmarkClient(b *testing.B) {
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSize := []int{1, 1024}
	respSize := []int{1, 1024}
	for _, maxC := range maxConcurrentCalls {
		for _, reqS := range reqSize {
			for _, respS := range respSize {
				b.Run(fmt.Sprintf("Unary-Trace-maxConcurrentCalls_"+
					"%#v-reqSize_%#v-respSize_%#v", maxC, reqS, respS), func(b *testing.B) {
					BenchClient(b, maxC, reqS, respS, true, true)
				})
				b.Run(fmt.Sprintf("Unary-noTrace-maxConcurrentCalls_"+
					"%#v-reqSize_%#v-respSize_%#v", maxC, reqS, respS), func(b *testing.B) {
					BenchClient(b, maxC, reqS, respS, false, true)
				})
				b.Run(fmt.Sprintf("Stream-Trace-maxConcurrentCalls_"+
					"%#v-reqSize_%#v-respSize_%#v", maxC, reqS, respS), func(b *testing.B) {
					BenchClient(b, maxC, reqS, respS, true, false)
				})
				b.Run(fmt.Sprintf("Stream-noTrace-maxConcurrentCalls_"+
					"%#v-reqSize_%#v-respSize_%#v", maxC, reqS, respS), func(b *testing.B) {
					BenchClient(b, maxC, reqS, respS, false, false)
				})
			}
		}
	}
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
