// +build !appengine

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
 *
 */

package profiling

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type queue struct {
	arr               []unsafe.Pointer
	size              uint32
	mask              uint32
	acquired          uint32
	written           uint32
	drainingPostCheck uint32
}

// Allocates and returns a new *queue. size needs to be a exponent of two.
func newQueue(size uint32) (q *queue) {
	q = &queue{
		arr:      make([]unsafe.Pointer, size),
		size:     size,
		mask:     size - 1,
		acquired: 0,
		written:  0,
	}

	return
}

// drainWait blocks the caller until all Pushes on this queue are complete.
func (q *queue) drainWait() {
	acquired := atomic.LoadUint32(&q.acquired)
	for acquired != atomic.LoadUint32(&q.written) {
		runtime.Gosched()
	}
}

// A queuePair has two queues. At any given time, Pushes go into the queue
// referenced by queuePair.q. The active queue get switched when there's a
// drain operation on the circular buffer.
type queuePair struct {
	q0 unsafe.Pointer
	q1 unsafe.Pointer
	q  unsafe.Pointer
}

// Allocates and returns a new *queuePair with its internal queues allocated.
func newQueuePair(size uint32) (qp *queuePair) {
	qp = &queuePair{}
	qp.q0 = unsafe.Pointer(newQueue(size))
	qp.q1 = unsafe.Pointer(newQueue(size))
	qp.q = qp.q0
	return
}

// Switches the current queue for future Pushes to proceed to the other queue
// so that there's no blocking in Push. Returns a pointer to the old queue that
// was in place before the switch.
func (qp *queuePair) switchQueues() *queue {
	// Even though we have mutual exclusion across drainers (thanks to mu.Lock in
	// drain), Push operations may access qp.q whilst we're writing to it.
	if atomic.CompareAndSwapPointer(&qp.q, qp.q0, qp.q1) {
		return (*queue)(qp.q0)
	} else {
		atomic.CompareAndSwapPointer(&qp.q, qp.q1, qp.q0)
		return (*queue)(qp.q1)
	}
}

// In order to not have expensive modulo operations, we require the maximum
// number of elements in the circular buffer (N) to be an exponent of two to
// use a bitwise AND mask. Since a circularBuffer is a collection of queuePairs
// (see below), we need to divide N; since exponents of two are only divisible
// by other exponents of two, we use floorCpuCount number of queuePairs within
// each circularBuffer.
//
// Floor of the number of CPUs (and not the ceiling) was found to the be the
// optimal number through experiments.
func floorCpuCount() uint32 {
	n := uint32(runtime.NumCPU())
	for i := uint32(1 << 31); i >= 2; i >>= 1 {
		if n&i > 0 {
			return i
		}
	}

	return 1
}

var numCircularBufferPairs = floorCpuCount()
var numCircularBufferPairsMask = numCircularBufferPairs - 1

// circularBuffer stores the Pushed elements in-memory. It uses a collection of
// queuePairs to reduce contention on the acquired and written counters within
// each queuePair; that is, at any given time, there may be floorCpuCount()
// Pushes happening simultaneously.
type circularBuffer struct {
	mu sync.Mutex
	qp []*queuePair
	// qpn is an monotonically incrementing counter that's used to determine
	// which queuePair a Push operation should to write to. This approach's
	// performance was found to be better than writing to a random queue.
	qpn uint32
}

var errorInvalidCircularBufferSize = errors.New("Buffer size is not an exponent of two.")

// Allocates a circular buffer of size size and returns a reference to the
// struct. Only circular buffers of size 2^k are allowed (saves us from having
// to do expensive modulo operations).
func newCircularBuffer(size uint32) (cb *circularBuffer, err error) {
	if size&(size-1) != 0 {
		err = errorInvalidCircularBufferSize
		return
	}

	cb = &circularBuffer{
		qp: make([]*queuePair, numCircularBufferPairs),
	}

	for i := uint32(0); i < numCircularBufferPairs; i++ {
		cb.qp[i] = newQueuePair(size / numCircularBufferPairs)
	}

	return
}

// Pushes an element in to the circular buffer.
func (cb *circularBuffer) Push(x interface{}) {
	n := atomic.AddUint32(&cb.qpn, 1) & numCircularBufferPairsMask
reloadq:
	q := (*queue)(atomic.LoadPointer(&cb.qp[n].q))

	acquired := atomic.AddUint32(&q.acquired, 1) - 1

	if atomic.LoadUint32(&q.drainingPostCheck) > 0 {
		// Between our q load and acquired increment, a drainer began execution
		// and switched the queues. This is NOT okay because we don't know if
		// acquired was incremented before or after the drainer's check for
		// acquired == writer. And we can't find which without expensive
		// operations, which we'd like to avoid. If the acquired increment was
		// after, we cannot write to this buffer as the drainer's collection may
		// have already started; we must write to the other queue.
		//
		// Reverse our increment and retry. Since there's no SubUint32 in atomic,
		// ^uint32(0) is used to denote -1.
		atomic.AddUint32(&q.acquired, ^uint32(0))
		goto reloadq
	}

	// At this point, we're definitely writing to the right queue. That is, one
	// of the following is true:
	//   1. No drainer is in execution.
	//   2. A drainer is in execution and it is waiting at the acquired ==
	//      written barrier.
	//
	// Let's say two Pushes A and B happen on the same queue. Say A and B are
	// q.size apart; i.e. they get the same index. That is,
	//
	//   index_A = index_B
	//   acquired_A + q.size = acquired_B
	//
	// We say "B has wrapped around A" when this happens. In this case, since A
	// occurred before B, B's Push should be the final value. However, we
	// accommodate A being the final value because wrap-arounds are extremely
	// rare and accounting for them requires an additional counter and a
	// significant performance penalty. Note that the below approach never leads
	// to any data corruption.
	index := acquired & q.mask
	atomic.StorePointer(&q.arr[index], unsafe.Pointer(&x))

	atomic.AddUint32(&q.written, 1)
}

// Dereferences non-nil pointers from arr into result. Range of elements from
// arr that are copied is [from, to). Assumes that the result slice is already
// allocated and is large enough to hold all the elements that might be copied.
// Also assumes mutual exclusion on the array of pointers.
func dereferenceAppend(result []interface{}, arr []unsafe.Pointer, from, to uint32) []interface{} {
	for i := from; i < to; i++ {
		// We have mutual exclusion on arr, there's no need for atomics.
		x := (*interface{})(arr[i])
		if x != nil {
			result = append(result, *x)
		}
	}
	return result
}

// Allocates and returns an array of things Pushed in to the circular buffer.
// Push order is not maintained; that is, if B was Pushed after A, drain may
// return B at a lower index than A in the returned array.
func (cb *circularBuffer) Drain() (result []interface{}) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	qs := make([]*queue, 0)
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		qs = append(qs, cb.qp[i].switchQueues())
	}

	var wg sync.WaitGroup
	wg.Add(int(numCircularBufferPairs))
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		go func(qi uint32) {
			qs[qi].drainWait()

			// Even though we have mutual exclusion across drainers, this alone
			// should be done with atomics because this is shared memory that is also
			// read by Push operations.
			atomic.StoreUint32(&qs[qi].drainingPostCheck, 1)

			wg.Done()
		}(i)
	}
	wg.Wait()

	result = make([]interface{}, 0)
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		if qs[i].acquired < qs[i].size {
			result = dereferenceAppend(result, qs[i].arr, 0, qs[i].acquired)
		} else {
			result = dereferenceAppend(result, qs[i].arr, 0, qs[i].size)
		}
	}

	for i := uint32(0); i < numCircularBufferPairs; i++ {
		qs[i].acquired = 0
		qs[i].written = 0
		qs[i].drainingPostCheck = 0
	}

	return
}
