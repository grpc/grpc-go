// +build go1.6,!go1.7

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
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark/stats"
)

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
