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
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/internal/grpcutil"
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

// stableClientHeaderBlock returns the reusable header fields a client sends on
// every RPC (pseudo-headers, content-type, user-agent, te) plus mdCount pieces
// of stable metadata. grpc-timeout is intentionally excluded; it is added by
// the benchmark below with a fresh value per iteration.
func stableClientHeaderBlock(mdCount int) []hpack.HeaderField {
	hf := []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/grpc.testing.BenchmarkService/UnaryCall"},
		{Name: ":authority", Value: "server:443"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "user-agent", Value: "grpc-go/benchmark"},
		{Name: "te", Value: "trailers"},
	}
	for i := 0; i < mdCount; i++ {
		hf = append(hf, hpack.HeaderField{
			Name:  fmt.Sprintf("header-%d", i),
			Value: fmt.Sprintf("value-%d", i),
		})
	}
	return hf
}

// BenchmarkEncodeGrpcTimeout isolates the HPACK encode path that dominates
// header CPU on the wire. A single encoder is reused across iterations, exactly
// as a transport reuses one encoder across every RPC, so the dynamic-table
// state carries over between requests.
//
// grpc-timeout carries the remaining time to the deadline, so its value differs
// on every RPC. The "indexed" case adds each unique value to the dynamic table
// (map insert per RPC, and eventual eviction of the reusable entries once the
// 4KB table fills), while the "sensitive" case skips indexing entirely. The
// delta between the two is the cost this change removes.
func BenchmarkEncodeGrpcTimeout(b *testing.B) {
	for _, mdCount := range []int{0, 4, 12} {
		for _, sensitive := range []bool{false, true} {
			mode := "indexed"
			if sensitive {
				mode = "sensitive"
			}
			b.Run(fmt.Sprintf("mdCount=%d/%s", mdCount, mode), func(b *testing.B) {
				stable := stableClientHeaderBlock(mdCount)
				var buf bytes.Buffer
				enc := hpack.NewEncoder(&buf)
				start := time.Now()

				b.ReportAllocs()
				var i int
				for b.Loop() {
					// Advance the deadline so EncodeDuration yields a distinct
					// value each iteration, mirroring production traffic.
					timeout := time.Until(start.Add(time.Duration(i) * time.Microsecond))
					if timeout <= 0 {
						timeout = time.Duration(i) * time.Microsecond
					}
					i++
					buf.Reset()
					for _, f := range stable {
						if err := enc.WriteField(f); err != nil {
							b.Fatal(err)
						}
					}
					if err := enc.WriteField(hpack.HeaderField{
						Name:      "grpc-timeout",
						Value:     grpcutil.EncodeDuration(timeout),
						Sensitive: sensitive,
					}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
