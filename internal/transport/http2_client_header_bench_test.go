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

// distinctValueCount is the number of pre-generated values each
// high-cardinality header cycles through in BenchmarkHPACKNeverIndex. It only
// has to exceed the number of entries the dynamic table can hold, so that a
// value is always evicted long before it is reused and every RPC misses.
const distinctValueCount = 1024

// BenchmarkHPACKNeverIndex measures both sides of the trade-off made by
// GRPC_GO_EXPERIMENTAL_HPACK_NEVER_INDEX_HEADERS: sec/op covers the whole
// outgoing header path, and bytes/rpc covers what that costs on the wire.
//
// The header fields go through a single long-lived hpack.Encoder, as they do on
// a real connection, so the dynamic table fills up and starts evicting. The
// high-cardinality headers take a fresh value on every iteration, which is what
// makes indexing them pure churn: the entry they add can never be matched
// again. Values are generated up front so that the measured loop does no work
// beyond building and encoding the header set.
func BenchmarkHPACKNeverIndex(b *testing.B) {
	// Header names whose value differs on (almost) every RPC. These are the
	// kind of names the environment variable is meant to be pointed at.
	highCardinality := []string{"grpc-trace-bin", "traceparent", "x-request-id"}

	for _, n := range []int{1, 3} {
		for _, neverIndex := range []bool{false, true} {
			name := fmt.Sprintf("highCardinalityHeaders=%d/neverIndex=%v", n, neverIndex)
			b.Run(name, func(b *testing.B) {
				names := highCardinality[:n]

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

				values := make(map[string][]string, len(names))
				for _, h := range names {
					vs := make([]string, distinctValueCount)
					for i := range vs {
						vs[i] = fmt.Sprintf("%s-%016d", h, i)
					}
					values[h] = vs
				}

				// The metadata is built once and its values are refreshed in
				// place per iteration; allocating a fresh context and metadata
				// map per RPC would dominate the measurement.
				md := make(metadata.MD, len(names)+1)
				md["x-stable-header"] = []string{"stable-value"}
				for _, h := range names {
					md[h] = []string{values[h][0]}
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

				b.ReportAllocs()
				for b.Loop() {
					i := int(rpcs) % distinctValueCount
					for _, h := range names {
						md[h][0] = values[h][i]
					}
					hf, err := t.createHeaderFields(ctx, callHdr)
					if err != nil {
						b.Fatal(err)
					}
					for _, f := range hf {
						if err := enc.WriteField(f); err != nil {
							b.Fatal(err)
						}
					}
					rpcs++
				}
				b.ReportMetric(float64(cw.n)/float64(rpcs), "bytes/rpc")
			})
		}
	}
}
