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
	"fmt"
	"testing"

	"golang.org/x/net/http2/hpack"

	"google.golang.org/grpc/metadata"
)

// BenchmarkAppendHeaderFieldsFromMD measures the slice re-allocation overhead
// of appendHeaderFieldsFromMD when the initial slice capacity is too small
// (old behavior) versus pre-sized to the exact metadata count (new behavior).
func BenchmarkAppendHeaderFieldsFromMD(b *testing.B) {
	for _, mdCount := range []int{0, 4, 12, 48} {
		b.Run(fmt.Sprintf("mdCount=%d", mdCount), func(b *testing.B) {
			md := make(metadata.MD, mdCount)
			for i := 0; i < mdCount; i++ {
				md[fmt.Sprintf("header-%d", i)] = []string{
					fmt.Sprintf("value-%d-a", i),
					fmt.Sprintf("value-%d-b", i),
				}
			}

			b.Run("unsized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					headerFields := make([]hpack.HeaderField, 0, 2)
					headerFields = appendHeaderFieldsFromMD(headerFields, md)
					_ = headerFields
				}
			})

			b.Run("sized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					headerFields := make([]hpack.HeaderField, 0, headerFieldsCountFromMD(md))
					headerFields = appendHeaderFieldsFromMD(headerFields, md)
					_ = headerFields
				}
			})
		})
	}
}
