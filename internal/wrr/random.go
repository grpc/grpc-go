/*
 *
 * Copyright 2019 gRPC authors.
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
	"sync"

	"google.golang.org/grpc/internal/grpcrand"
)

// weightedItem is a wrapped weighted item that is used to implement weighted random algorithm.
type weightedItem struct {
	Item   interface{}
	Weight int64
}

// randomWRR is a struct that contains weighted items implement weighted random algorithm.
type randomWRR struct {
	mu           sync.RWMutex
	items        []*weightedItem
	itemIndex    map[interface{}]int
	sumOfWeights int64
}

// NewRandom creates a new WRR with random.
func NewRandom() WRR {
	return &randomWRR{
		itemIndex: make(map[interface{}]int),
	}
}

var grpcrandInt63n = grpcrand.Int63n

func (rw *randomWRR) Next() (item interface{}) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	if rw.sumOfWeights == 0 {
		return nil
	}
	// Random number in [0, sum).
	randomWeight := grpcrandInt63n(rw.sumOfWeights)
	for _, item := range rw.items {
		randomWeight = randomWeight - item.Weight
		if randomWeight < 0 {
			return item.Item
		}
	}

	return rw.items[len(rw.items)-1].Item
}

func (rw *randomWRR) UpdateOrAdd(item interface{}, weight int64) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if idx, ok := rw.itemIndex[item]; ok {
		rw.sumOfWeights += weight - rw.items[idx].Weight
		rw.items[idx].Weight = weight
		return
	}
	rItem := &weightedItem{Item: item, Weight: weight}
	rw.items = append(rw.items, rItem)
	rw.itemIndex[item] = len(rw.items) - 1
	rw.sumOfWeights += weight
}

func (rw *randomWRR) Remove(item interface{}) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if idx, ok := rw.itemIndex[item]; ok {
		rw.sumOfWeights -= rw.items[idx].Weight
		rw.items = append(rw.items[:idx], rw.items[idx+1:]...)
		delete(rw.itemIndex, item)
	}
}
