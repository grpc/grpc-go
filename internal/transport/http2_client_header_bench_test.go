/*
 *
 * Copyright 2026 gRPC authors.
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

package transport

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// BenchmarkCreateHeaderFields measures header-slice construction as the amount
// of outgoing metadata varies.
func BenchmarkCreateHeaderFields(b *testing.B) {
	for _, mdCount := range []int{0, 4, 12} {
		b.Run(fmt.Sprintf("mdCount=%d", mdCount), func(b *testing.B) {
			t := &http2Client{
				scheme:    "https",
				userAgent: "grpc-go/benchmark",
				md:        metadata.MD{},
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			if mdCount > 0 {
				md := make(metadata.MD, mdCount)
				for i := 0; i < mdCount; i++ {
					md[fmt.Sprintf("header-%d", i)] = []string{fmt.Sprintf("value-%d", i)}
				}
				ctx = metadata.NewOutgoingContext(ctx, md)
			}
			callHdr := &CallHdr{
				Method: "/grpc.testing.BenchmarkService/UnaryCall",
				Host:   "server:443",
			}

			b.ReportAllocs()
			for b.Loop() {
				hf, err := t.createHeaderFields(ctx, callHdr)
				if err != nil {
					b.Fatal(err)
				}
				if len(hf) == 0 {
					b.Fatal("no header fields produced")
				}
			}
		})

		b.Run(fmt.Sprintf("mdCount=%d-appended", mdCount), func(b *testing.B) {
			t := &http2Client{
				scheme:    "https",
				userAgent: "grpc-go/benchmark",
				md:        metadata.MD{},
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			kvs := make([]string, 0, mdCount*2)
			for i := 0; i < mdCount; i++ {
				kvs = append(kvs, fmt.Sprintf("header-%d", i), fmt.Sprintf("value-%d", i))
			}
			ctx = metadata.AppendToOutgoingContext(ctx, kvs...)
			callHdr := &CallHdr{
				Method: "/grpc.testing.BenchmarkService/UnaryCall",
				Host:   "server:443",
			}

			b.ReportAllocs()
			for b.Loop() {
				hf, err := t.createHeaderFields(ctx, callHdr)
				if err != nil {
					b.Fatal(err)
				}
				if len(hf) == 0 {
					b.Fatal("no header fields produced")
				}
			}
		})
	}
}
