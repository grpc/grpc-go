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
	"fmt"
	"sort"
	"sync"

	"google.golang.org/grpc/internal/grpcrand"
)

// weightedItem is a wrapped weighted item that is used to implement weighted random algorithm.
type weightedItem struct {
	Item interface{}
	// TODO Delete Weight? This field is not necessary for randomWRR to work.
	// But without this field, if we want to know an item's weight in randomWRR.Add , we have to
	// calculate it (i.e. weight = items.AccumulatedWeight - previousItem.AccumulatedWeight)
	// which is a bit less concise than items.Weight
	Weight            int64
	AccumulatedWeight int64
}

func (w *weightedItem) String() string {
	return fmt.Sprint(*w)
}

// randomWRR is a struct that contains weighted items implement weighted random algorithm.
type randomWRR struct {
	mu           sync.RWMutex
	items        []*weightedItem
	equalWeights bool
}

// NewRandom creates a new WRR with random.
func NewRandom() WRR {
	return &randomWRR{}
}

var grpcrandInt63n = grpcrand.Int63n
var grpcrandIntn = grpcrand.Intn

func (rw *randomWRR) Next() (item interface{}) {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	sumOfWeights := rw.items[len(rw.items)-1].AccumulatedWeight
	if sumOfWeights == 0 {
		return nil
	}
	if rw.equalWeights {
		return rw.items[grpcrandIntn(len(rw.items))].Item
	}
	// Random number in [0, sumOfWeights).
	randomWeight := grpcrandInt63n(sumOfWeights)
	// Item's accumulated weights are in ascending order, because item's weight >= 0.
	// Binary search rw.items to find first item whose AccumulatedWeight > randomWeight
	// The return i is guaranteed to be in range [0, len(rw.items)) because randomWeight < last item's AccumulatedWeight
	i := sort.Search(len(rw.items), func(i int) bool { return rw.items[i].AccumulatedWeight > randomWeight })
	return rw.items[i].Item
}

func (rw *randomWRR) Add(item interface{}, weight int64) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	accumulatedWeight := weight
	equalWeights := true
	if len(rw.items) > 0 {
		lastItem := rw.items[len(rw.items)-1]
		accumulatedWeight = lastItem.AccumulatedWeight + weight
		equalWeights = rw.equalWeights && weight == lastItem.Weight
	}
	rw.equalWeights = equalWeights
	rItem := &weightedItem{Item: item, Weight: weight, AccumulatedWeight: accumulatedWeight}
	rw.items = append(rw.items, rItem)
}

func (rw *randomWRR) String() string {
	return fmt.Sprint(rw.items)
}
