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

package test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"

	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// BenchmarkE2EHPACKNeverIndex is the end-to-end counterpart of
// transport.BenchmarkHPACKNeverIndex. Client and server run in-process, so the
// results include both HPACK encoding and decoding.
func BenchmarkE2EHPACKNeverIndex(b *testing.B) {
	const (
		stableHeaderCount  = 5
		distinctValueCount = 1024
	)
	stableKV := make([]string, 0, 2*stableHeaderCount)
	for s := 0; s < stableHeaderCount; s++ {
		stableKV = append(stableKV, fmt.Sprintf("x-stable-%d", s), fmt.Sprintf("stable-value-%d", s))
	}
	for _, n := range []int{1, 3, 10, 50} {
		names := make([]string, n)
		values := make([][]string, n)
		for j := range names {
			names[j] = fmt.Sprintf("x-unique-%02d", j)
			values[j] = make([]string, distinctValueCount)
			for i := range values[j] {
				values[j][i] = fmt.Sprintf("v%02d-%024d", j, i)
			}
		}

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

				ss := &stubserver.StubServer{
					EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
						return &testpb.Empty{}, nil
					},
				}
				if err := ss.Start(nil); err != nil {
					b.Fatalf("Error starting endpoint server: %v", err)
				}
				defer ss.Stop()

				var rpcs atomic.Uint64
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					kv := make([]string, 0, 2*(stableHeaderCount+n))
					for pb.Next() {
						i := int(rpcs.Add(1) % distinctValueCount)
						kv = append(kv[:0], stableKV...)
						for j, h := range names {
							kv = append(kv, h, values[j][i])
						}
						ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
						ctx = metadata.AppendToOutgoingContext(ctx, kv...)
						_, err := ss.Client.EmptyCall(ctx, &testpb.Empty{})
						cancel()
						if err != nil {
							b.Errorf("EmptyCall() failed: %v", err)
							return
						}
					}
				})
			})
		}
	}
}
