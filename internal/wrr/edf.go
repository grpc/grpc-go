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
	lock  sync.Mutex
	queue edfPriorityQueue
	items map[interface{}]*edfEntry
}

// NewEDF creates Earliest Deadline First (EDF)
// (https://en.wikipedia.org/wiki/Earliest_deadline_first_scheduling) implementation for weighted round robin.
// Each pick from the schedule has the earliest deadline entry selected. Entries have deadlines set
// at current time + 1 / weight, providing weighted round robin behavior with O(log n) pick time.
func NewEDF() WRR {
	return &edfWrr{items: make(map[interface{}]*edfEntry)}
}

// edfEntry is an internal wrapper for item that also stores weight and relative position in the queue.
type edfEntry struct {
	index    int
	deadline float64
	item     interface{}
	weight   int64
}

// edfPriorityQueue is a heap.Interface implementation for edfEntry elements.
type edfPriorityQueue []*edfEntry

func (pq edfPriorityQueue) Len() int           { return len(pq) }
func (pq edfPriorityQueue) Less(i, j int) bool { return pq[i].deadline < pq[j].deadline }
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

// Current time in EDF scheduler.
func (edf edfWrr) currentTime() float64 {
	if len(edf.queue) == 0 {
		return 0.0
	}
	return edf.queue[0].deadline
}

func (edf *edfWrr) Add(item interface{}, weight int64) {
	edf.lock.Lock()
	defer edf.lock.Unlock()

	entry, inQueue := edf.items[item]
	if inQueue {
		entry.weight += weight
		return
	}

	entry = &edfEntry{
		index:    edf.queue.Len(),
		deadline: edf.currentTime() + 1.0/float64(weight),
		item:     item,
		weight:   weight,
	}
	edf.items[item] = entry
	heap.Push(&edf.queue, entry)
}

func (edf *edfWrr) Next() interface{} {
	edf.lock.Lock()
	defer edf.lock.Unlock()
	if len(edf.queue) == 0 {
		return nil
	}
	entry := edf.queue[0]
	entry.deadline = edf.currentTime() + 1.0/float64(entry.weight)
	heap.Fix(&edf.queue, 0)
	return entry.item
}

func (edf *edfWrr) UpdateOrAdd(item interface{}, weight int64) {
	edf.lock.Lock()
	defer edf.lock.Unlock()
	entry, ok := edf.items[item]
	if ok {
		entry.weight = weight
	} else {
		entry = &edfEntry{
			index:    edf.queue.Len(),
			deadline: edf.currentTime() + 1.0/float64(weight),
			item:     item,
			weight:   weight,
		}
		edf.items[item] = entry
		heap.Push(&edf.queue, entry)
	}
}

func (edf *edfWrr) Remove(item interface{}) {
	edf.lock.Lock()
	defer edf.lock.Unlock()
	entry, ok := edf.items[item]
	if !ok {
		return
	}
	heap.Remove(&edf.queue, entry.index)
}
