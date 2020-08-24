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
	"container/heap"
	"sync"
)

// edfWrr is a struct for EDF weighted round robin implementation.
type edfWrr struct {
	lock               sync.Mutex
	queue              edfPriorityQueue
	currentOrderOffset uint64
	currentTime        float64
	items              map[interface{}]*edfEntry
}

// NewEDF creates Earliest Deadline First (EDF)
// (https://en.wikipedia.org/wiki/Earliest_deadline_first_scheduling) implementation for weighted round robin.
// Each pick from the schedule has the earliest deadline entry selected. Entries have deadlines set
// at current time + 1 / weight, providing weighted round robin behavior with O(log n) pick time.
func NewEDF() WRR {
	return &edfWrr{
		items: make(map[interface{}]*edfEntry),
	}
}

// edfEntry is an internal wrapper for item that also stores weight and relative position in the queue.
type edfEntry struct {
	index       int
	deadline    float64
	weight      int64
	orderOffset uint64
	item        interface{}
}

// edfPriorityQueue is a heap.Interface implementation for edfEntry elements.
type edfPriorityQueue []*edfEntry

func (pq edfPriorityQueue) Len() int { return len(pq) }
func (pq edfPriorityQueue) Less(i, j int) bool {
	return pq[i].deadline < pq[j].deadline || pq[i].deadline == pq[j].deadline && pq[i].orderOffset < pq[j].orderOffset
}
func (pq edfPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *edfPriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*edfEntry))
}

func (pq *edfPriorityQueue) Pop() interface{} {
	old := *pq
	*pq = old[0 : len(old)-1]
	return old[len(old)-1]
}

func (edf *edfWrr) Next() interface{} {
	edf.lock.Lock()
	defer edf.lock.Unlock()
	if edf.queue.Len() == 0 {
		return nil
	}
	item := edf.queue[0]
	edf.currentTime = item.deadline
	item.deadline = edf.currentTime + 1.0/float64(item.weight)
	heap.Fix(&edf.queue, 0)
	return item.item
}

func (edf *edfWrr) Remove(item interface{}) {
	edf.lock.Lock()
	defer edf.lock.Unlock()
	edf.removeLocked(item)
}

func (edf *edfWrr) removeLocked(item interface{}) {
	entry, ok := edf.items[item]
	if !ok {
		return
	}
	heap.Remove(&edf.queue, entry.index)
	delete(edf.items, item)
}

func (edf *edfWrr) UpdateOrAdd(item interface{}, weight int64) {
	edf.lock.Lock()
	defer edf.lock.Unlock()

	if weight == 0 {
		edf.removeLocked(item)
		return
	}

	if entry, ok := edf.items[item]; ok {
		entry.weight = weight
		return
	}

	entry := &edfEntry{
		deadline:    edf.currentTime + 1.0/float64(weight),
		weight:      weight,
		item:        item,
		orderOffset: edf.currentOrderOffset,
		index:       edf.queue.Len(),
	}
	edf.items[item] = entry
	edf.currentOrderOffset++
	heap.Push(&edf.queue, entry)
}
