// +build go1.7

/*
 *
 * Copyright 2017 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package benchmark

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchmarkClient(b *testing.B) {
	enableTrace := []bool{true, false}
	// When set the latency to 0 (no delay), the result is slower than the real result with no delay
	// because latency simulation section has extra operations
	latency := []time.Duration{0, 40 * time.Millisecond} // if non-positive, no delay.
	kbps := []int{0, 10240}                              // if non-positive, infinite
	mtu := []int{0, 512}                                 // if non-positive, infinite
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSizeBytes := []int{1, 1024, 1024 * 1024}
	reqspSizeBytes := []int{1, 1024, 1024 * 1024}

	featuresPos := make([]int, 7)
	// 0:enableTracing 1:md 2:ltc 3:kbps 4:mtu 5:maxC 6:connCount 7:reqSize 8:respSize
	featuresNum := []int{len(enableTrace), len(latency), len(kbps), len(mtu), len(maxConcurrentCalls), len(reqSizeBytes), len(reqspSizeBytes)}

	// slice range preprocess
	initalPos := make([]int, len(featuresPos))
	// run benchmarks
	start := true
	for !reflect.DeepEqual(featuresPos, initalPos) || start {
		start = false
		tracing := "Trace"
		if !enableTrace[featuresPos[0]] {
			tracing = "noTrace"
		}

		benchFeature := Features{
			EnableTrace:        enableTrace[featuresPos[0]],
			Latency:            latency[featuresPos[1]],
			Kbps:               kbps[featuresPos[2]],
			Mtu:                mtu[featuresPos[3]],
			MaxConcurrentCalls: maxConcurrentCalls[featuresPos[4]],
			ReqSizeBytes:       reqSizeBytes[featuresPos[5]],
			RespSizeBytes:      reqspSizeBytes[featuresPos[6]],
		}

		grpc.EnableTracing = enableTrace[featuresPos[0]]
		b.Run(fmt.Sprintf("Unary-%s-%s",
			tracing, benchFeature.String()), func(b *testing.B) {
			runUnary(b, benchFeature)
		})

		b.Run(fmt.Sprintf("Stream-%s-%s",
			tracing, benchFeature.String()), func(b *testing.B) {
			runStream(b, benchFeature)
		})
		AddOne(featuresPos, featuresNum)
	}
}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
