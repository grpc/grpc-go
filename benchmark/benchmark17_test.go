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
	"google.golang.org/grpc/metadata"
)

func (f features) String() string {
	return fmt.Sprintf("latency_%s-kbps_%#v-MTU_%#v-maxConcurrentCalls_"+
		"%#v-maxConn_%#v-reqSize_%#vB-respSize_%#vB",
		f.latency.String(), f.kbps, f.mtu, f.maxConcurrentCalls, f.maxConnCount, f.reqSizeBytes, f.respSizeBytes)
}

func Add(features []int, upperBound []int) {
	for i := len(features) - 1; i >= 0; i-- {
		features[i] = (features[i] + 1)
		if features[i]/upperBound[i] == 0 {
			break
		}
		features[i] = features[i] % upperBound[i]
	}
}

func BenchmarkClient(b *testing.B) {
	enableTrace := []bool{true, false}
	md := []metadata.MD{{}, metadata.New(map[string]string{"key1": "val1"})}
	// When set the latency to 0 (no delay), the result is slower than the real result with no delay
	// because latency simulation section has extra operations
	latency := []time.Duration{0, 40 * time.Millisecond} // if non-positive, no delay.
	kbps := []int{0, 10240}                              // if non-positive, infinite
	mtu := []int{0, 512}                                 // if non-positive, infinite
	maxConcurrentCalls := []int{1, 8, 64, 512}
	maxConnCount := []int{1, 4}
	reqSizeBytes := []int{1, 1024, 1024 * 1024}
	reqspSizeBytes := []int{1, 1024, 1024 * 1024}

	featuresPos := make([]int, 9)
	// 0:enableTracing 1:md 2:ltc 3:kbps 4:mtu 5:maxC 6:connCount 7:reqSize 8:respSize
	featuresNum := []int{2, 2, 2, 2, 2, 4, 2, 3, 3}

	// slice range preprocess
	initalPos := make([]int, len(featuresPos))
	// run benchmarks
	start := true
	for !reflect.DeepEqual(featuresPos, initalPos) || start {
		start = false
		tracing := "Trace"
		if featuresPos[0] == 0 {
			tracing = "noTrace"
		}
		hasMeta := "hasMetadata"
		if featuresPos[1] == 0 {
			hasMeta = "noMetadata"
		}

		benchFeature := features{
			enableTrace:        enableTrace[featuresPos[0]],
			md:                 md[featuresPos[1]],
			latency:            latency[featuresPos[2]],
			kbps:               kbps[featuresPos[3]],
			mtu:                mtu[featuresPos[4]],
			maxConcurrentCalls: maxConcurrentCalls[featuresPos[5]],
			maxConnCount:       maxConnCount[featuresPos[6]],
			reqSizeBytes:       reqSizeBytes[featuresPos[7]],
			respSizeBytes:      reqspSizeBytes[featuresPos[8]],
		}

		grpc.EnableTracing = enableTrace[featuresPos[0]]
		b.Run(fmt.Sprintf("Unary-%s-%s-%s",
			tracing, hasMeta, benchFeature.String()), func(b *testing.B) {
			runUnary(b, benchFeature)
		})
		b.Run(fmt.Sprintf("Stream-%s-%s-%s",
			tracing, hasMeta, benchFeature.String()), func(b *testing.B) {
			runStream(b, benchFeature)
		})

		Add(featuresPos, featuresNum)
	}

}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
