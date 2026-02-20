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
)

type aliasWRR struct {
	items        []*weightedItem
	alias        []int
	prob         []float64
	equalWeights bool
}

// NewAlias creates a new WRR with aliasing.
func NewAlias() WRR {
	return &aliasWRR{equalWeights: true}
}

func (aw *aliasWRR) Add(item any, weight int64) {
	if len(aw.items) > 0 && aw.equalWeights {
		if weight != aw.items[0].weight {
			aw.equalWeights = false
		}
	}
	aw.items = append(aw.items, &weightedItem{item: item, weight: weight})
	if !aw.equalWeights {
		aw.buildAliasTable()
	}
}

func (aw *aliasWRR) Next() any {
	if len(aw.items) == 0 {
		return nil
	}

	if aw.equalWeights {
		return aw.items[rand.IntN(len(aw.items))].item
	}

	n := len(aw.items)
	i := rand.IntN(n)
	r := rand.Float64()

	if r < aw.prob[i] {
		return aw.items[i].item
	}
	return aw.items[aw.alias[i]].item
}

func (aw *aliasWRR) buildAliasTable() {
	n := len(aw.items)
	if n == 0 {
		return
	}

	totalWeight := int64(0)
	for _, item := range aw.items {
		totalWeight += item.weight
	}

	aw.prob = make([]float64, n)
	aw.alias = make([]int, n)

	// small and large are stacks of indices
	small := make([]int, 0, n)
	large := make([]int, 0, n)

	// Scale probabilities so that average is 1.0
	avgWeight := float64(totalWeight) / float64(n)

	for i, item := range aw.items {
		if avgWeight == 0 {
			aw.prob[i] = 0
		} else {
			aw.prob[i] = float64(item.weight) / avgWeight
		}

		if aw.prob[i] < 1.0 {
			small = append(small, i)
		} else {
			large = append(large, i)
		}
	}

	for len(small) > 0 && len(large) > 0 {
		l := small[len(small)-1]
		small = small[:len(small)-1]

		g := large[len(large)-1]
		large = large[:len(large)-1]

		aw.alias[l] = g
		aw.prob[g] = (aw.prob[g] + aw.prob[l]) - 1.0

		if aw.prob[g] < 1.0 {
			small = append(small, g)
		} else {
			large = append(large, g)
		}
	}

	for len(large) > 0 {
		g := large[len(large)-1]
		large = large[:len(large)-1]
		aw.prob[g] = 1.0
	}

	for len(small) > 0 {
		l := small[len(small)-1]
		small = small[:len(small)-1]
		aw.prob[l] = 1.0
	}
}

func (aw *aliasWRR) String() string {
	return fmt.Sprint(aw.items)
}
