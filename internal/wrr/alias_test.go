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
	rand "math/rand/v2"
	"strconv"
	"testing"
)

func (s) TestAliasWRRNext(t *testing.T) {
	testWRRNext(t, NewAlias)
}

func BenchmarkAliasWRRNext(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		b.Run("equal-weights-"+strconv.Itoa(n)+"-items", func(b *testing.B) {
			w := NewAlias()
			sumOfWeights := n
			for i := 0; i < n; i++ {
				w.Add(i, 1)
			}
			w.Next()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for i := 0; i < sumOfWeights; i++ {
					w.Next()
				}
			}
		})
	}

	var maxWeight int64 = 1024
	for _, n := range []int{100, 500, 1000} {
		b.Run("random-weights-"+strconv.Itoa(n)+"-items", func(b *testing.B) {
			w := NewAlias()
			var sumOfWeights int64
			for i := 0; i < n; i++ {
				weight := rand.Int64N(maxWeight + 1)
				w.Add(i, weight)
				sumOfWeights += weight
			}
			w.Next()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for i := 0; i < int(sumOfWeights); i++ {
					w.Next()
				}
			}
		})
	}

	itemsNum := 200
	heavyWeight := int64(itemsNum)
	lightWeight := int64(1)
	heavyIndices := []int{0, itemsNum / 2, itemsNum - 1}
	for _, heavyIndex := range heavyIndices {
		b.Run("skew-weights-heavy-index-"+strconv.Itoa(heavyIndex), func(b *testing.B) {
			w := NewAlias()
			var sumOfWeights int64
			for i := 0; i < itemsNum; i++ {
				var weight int64
				if i == heavyIndex {
					weight = heavyWeight
				} else {
					weight = lightWeight
				}
				sumOfWeights += weight
				w.Add(i, weight)
			}
			w.Next()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for i := 0; i < int(sumOfWeights); i++ {
					w.Next()
				}
			}
		})
	}
}
