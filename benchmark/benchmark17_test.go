// +build go1.7

package benchmark

import (
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchmarkClient(b *testing.B) {
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSizeBytes := []int{1, 1024}
	reqspSizeBytes := []int{1, 1024}
	kbps := []int{0, 10240} // if non-positive, infinite
	MTU := []int{0, 10}     // if non-positive, infinite
	// When set the latency to 0 (no delay), the result is slower than the real result with no delay
	// because latency simulation section has extra operations
	latency := []time.Duration{0, 40 * time.Millisecond} // if non-positive, no delay.

	for _, enableTracing := range []bool{true, false} {
		grpc.EnableTracing = enableTracing
		tracing := "Tracing"
		if !enableTracing {
			tracing = "noTrace"
		}
		for _, ltc := range latency {
			for _, k := range kbps {
				for _, mtu := range MTU {
					for _, maxC := range maxConcurrentCalls {
						for _, reqS := range reqSizeBytes {
							for _, respS := range reqspSizeBytes {
								b.Run(fmt.Sprintf("Unary-%s-kbps_%#v-MTU_%#v-maxConcurrentCalls_"+
									"%#v-reqSize_%#v-respSize_%#v-latency_%s",
									tracing, k, mtu, maxC, reqS, respS, ltc.String()), func(b *testing.B) {
									runUnary(b, maxC, reqS, respS, k, mtu, ltc)
								})
								b.Run(fmt.Sprintf("Stream-%s-kbps_%#v-MTU_%#v-maxConcurrentCalls_"+
									"%#v-reqSize_%#v-respSize_%#v-latency_%s",
									tracing, k, mtu, maxC, reqS, respS, ltc.String()), func(b *testing.B) {
									runStream(b, maxC, reqS, respS, k, mtu, ltc)
								})
							}
						}
					}
				}
			}
		}
	}

}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
