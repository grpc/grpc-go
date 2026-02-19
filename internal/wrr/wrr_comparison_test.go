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
 */

package wrr

import (
	"fmt"
	rand "math/rand/v2"
	"testing"
)

func BenchmarkWRR_Comparison(b *testing.B) {
	counts := []int{10, 100, 1000, 10000}

	maxWeight := int64(100)

	for _, count := range counts {
		weights := make([]int64, count)
		for i := 0; i < count; i++ {
			weights[i] = rand.Int64N(maxWeight) + 1
		}

		b.Run(fmt.Sprintf("Random_O_logN_N=%d", count), func(b *testing.B) {
			w := NewRandom()
			for i, weight := range weights {
				w.Add(i, weight)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.Next()
			}
		})

		b.Run(fmt.Sprintf("Alias_O_1_N=%d", count), func(b *testing.B) {
			w := NewAlias()
			for i, weight := range weights {
				w.Add(i, weight)
			}
			w.Next()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w.Next()
			}
		})
	}
}
