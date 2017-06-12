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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

func BenchmarkClient(b *testing.B) {
	maxConcurrentCalls := []int{1, 8, 64, 512}
	reqSizeBytes := []int{1, 1024}
	reqspSizeBytes := []int{1, 1024}
	for _, enableTracing := range []bool{true, false} {
		grpc.EnableTracing = enableTracing
		tracing := "Tracing"
		if !enableTracing {
			tracing = "noTrace"
		}
		for _, maxC := range maxConcurrentCalls {
			for _, reqS := range reqSizeBytes {
				for _, respS := range reqspSizeBytes {
					b.Run(fmt.Sprintf("Unary-%s-maxConcurrentCalls_"+
						"%#v-reqSize_%#vB-respSize_%#vB", tracing, maxC, reqS, respS), func(b *testing.B) {
						runUnary(b, maxC, reqS, respS)
					})
					b.Run(fmt.Sprintf("Stream-%s-maxConcurrentCalls_"+
						"%#v-reqSize_%#vB-respSize_%#vB", tracing, maxC, reqS, respS), func(b *testing.B) {
						runStream(b, maxC, reqS, respS)
					})
				}
			}
		}
	}

}

func TestMain(m *testing.M) {
	os.Exit(stats.RunTestMain(m))
}
