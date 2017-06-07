// +build go1.7

package benchmark

import (
	"fmt"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchClient(b *testing.B, maxConcurrentCalls, reqSize, respSize int, unaryMode, enableTrace bool) {
	if enableTrace {
		grpc.EnableTracing = true
	} else {
		grpc.EnableTracing = false
	}
	if unaryMode {
		runUnary(b, maxConcurrentCalls, reqSize, respSize)
	} else {
		runStream(b, maxConcurrentCalls, reqSize, respSize)
	}
}

func BenchmarkClient(b *testing.B) {
	const (
		runModeUnary     = true
		runModeStreaming = false
		megabyte         = 1048576
	)
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSizeBytes := []int{1, 1 * 1024}
	reqspSizeBytes := []int{1, 1 * 1024}

	for _, mode := range []bool{runModeUnary, runModeStreaming} {
		for _, enableTracing := range []bool{true, false} {
			for _, maxC := range maxConcurrentCalls {
				for _, reqS := range reqSizeBytes {
					for _, respS := range reqspSizeBytes {
						tracing := "Tracing"
						if !enableTracing {
							tracing = "noTrace"
						}
						runMode := "Unary"
						if !mode {
							runMode = "Stream"
						}
						b.Run(fmt.Sprintf("%s-%s-maxConcurrentCalls_"+
							"%#v-reqSize_%#v-respSize_%#v", runMode, tracing, maxC, reqS, respS), func(b *testing.B) {
							BenchClient(b, maxC, reqS, respS, mode, enableTracing)
						})
					}
				}
			}
		}
	}

}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
