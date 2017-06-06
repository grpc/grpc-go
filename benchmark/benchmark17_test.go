// +build go1.7

package benchmark

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchClient(b *testing.B, maxConcurrentCalls, reqSize, respSize int, runMode, enableTrace string) {
	if strings.EqualFold(enableTrace, "Trace") {
		grpc.EnableTracing = true
	} else {
		grpc.EnableTracing = false
	}
	if strings.EqualFold(runMode, "Unary") {
		runUnary(b, maxConcurrentCalls, reqSize, respSize)
	} else {
		runStream(b, maxConcurrentCalls, reqSize, respSize)
	}
}

func BenchmarkClient(b *testing.B) {
	mode_trace := []struct {
		runMode     string
		enableTrace string
	}{
		{"Unary", "Trace"},
		{"Unary", "noTrace"},
		{"Stream", "Trace"},
		{"Stream", "noTrace"},
	}
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSize := []int{1, 1024}
	respSize := []int{1, 1024}
	for _, mt := range mode_trace {
		for _, maxC := range maxConcurrentCalls {
			for _, reqS := range reqSize {
				for _, respS := range respSize {
					b.Run(fmt.Sprintf("%s-%s-maxConcurrentCalls_"+
						"%#v-reqSize_%#v-respSize_%#v", mt.runMode, mt.enableTrace, maxC, reqS, respS), func(b *testing.B) {
						BenchClient(b, maxC, reqS, respS, mt.runMode, mt.enableTrace)
					})
				}
			}
		}
	}
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
