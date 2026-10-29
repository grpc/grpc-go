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

	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/internal/envconfig"
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

// countingWriter discards its input and records how many bytes it saw.
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// distinctValueCount exceeds what the HPACK dynamic table can hold, so a
// high-cardinality value is never matched again.
const distinctValueCount = 1024

// stableHeaderCount is the number of fixed-value headers sent on every RPC.
const stableHeaderCount = 5

// BenchmarkHPACKNeverIndex encodes headers with high-cardinality values through
// a single long-lived hpack.Encoder, with and without never-indexing them.
// reusable-bytes/rpc is the wire cost of the headers whose value repeats, which
// grows when the high-cardinality entries evict them from the dynamic table.
func BenchmarkHPACKNeverIndex(b *testing.B) {
	for _, n := range []int{1, 3, 10, 50} {
		names := make([]string, n)
		highCardinality := make(map[string]bool, n)
		for i := range names {
			names[i] = fmt.Sprintf("x-unique-%02d", i)
			highCardinality[names[i]] = true
		}
		highCardinality["grpc-timeout"] = true

		for _, neverIndex := range []bool{false, true} {
			name := fmt.Sprintf("highCardinalityHeaders=%d/neverIndex=%v", n, neverIndex)
			b.Run(name, func(b *testing.B) {
				orig := envconfig.HPACKNeverIndexHeaders
				b.Cleanup(func() { envconfig.HPACKNeverIndexHeaders = orig })
				envconfig.HPACKNeverIndexHeaders = nil
				if neverIndex {
					set := make(map[string]struct{}, len(names))
					for _, h := range names {
						set[h] = struct{}{}
					}
					envconfig.HPACKNeverIndexHeaders = set
				}

				values := make([][]string, len(names))
				for j := range names {
					vs := make([]string, distinctValueCount)
					for i := range vs {
						vs[i] = fmt.Sprintf("v%02d-%024d", j, i)
					}
					values[j] = vs
				}

				// Values are updated in place to keep allocations out of the loop.
				md := make(metadata.MD, len(names)+stableHeaderCount)
				for i := 0; i < stableHeaderCount; i++ {
					md[fmt.Sprintf("x-stable-%d", i)] = []string{fmt.Sprintf("stable-value-%d", i)}
				}
				for j, h := range names {
					md[h] = []string{values[j][0]}
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
				defer cancel()
				ctx = metadata.NewOutgoingContext(ctx, md)

				t := &http2Client{
					scheme:    "https",
					userAgent: "grpc-go/benchmark",
					md:        metadata.MD{},
				}
				callHdr := &CallHdr{
					Method: "/grpc.testing.BenchmarkService/UnaryCall",
					Host:   "server:443",
				}
				cw := &countingWriter{}
				enc := hpack.NewEncoder(cw)
				rpcs := int64(0)
				reusableBytes := int64(0)

				b.ReportAllocs()
				for b.Loop() {
					i := int(rpcs) % distinctValueCount
					for j := range names {
						md[names[j]][0] = values[j][i]
					}
					hf, err := t.createHeaderFields(ctx, callHdr)
					if err != nil {
						b.Fatal(err)
					}
					for _, f := range hf {
						before := cw.n
						if err := enc.WriteField(f); err != nil {
							b.Fatal(err)
						}
						if !highCardinality[f.Name] {
							reusableBytes += cw.n - before
						}
					}
					rpcs++
				}
				b.ReportMetric(float64(cw.n)/float64(rpcs), "bytes/rpc")
				b.ReportMetric(float64(reusableBytes)/float64(rpcs), "reusable-bytes/rpc")
			})
		}
	}
}
