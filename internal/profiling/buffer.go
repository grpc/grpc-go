package profiling

import (
	"sync"
	"sync/atomic"
	"unsafe"
	"runtime"
	"errors"
)

func floorCpuCount() uint32 {
	n := uint32(runtime.NumCPU())
	for i := uint32(1 << 31); i >= 2; i >>= 1 {
		if n & i > 0 {
			return i
		}
	}

	return 1
}

var numCircularBufferPairs = floorCpuCount()
var numCircularBufferPairsMask = numCircularBufferPairs - 1

type item struct {
	ptr unsafe.Pointer
	acquired uint32
}

type queue struct {
	arr      []item
	size     uint32
	mask     uint32
	acquired uint32
	drainingPostCheck uint32
}

// Allocates and returns a queue.
func NewQueue(size uint32) (q *queue) {
	q = &queue{
		arr: make([]item, size),
		size: size,
		mask: size - 1,
		acquired: 0,
	}

	for i := uint32(0); i < size; i++ {
		q.arr[i].acquired = ^uint32(0)
	}

	return
}

func (q *queue) drainWait() {
	acquired := atomic.LoadUint32(&q.acquired) - 1
	index := acquired & q.mask
	for acquired != atomic.LoadUint32(&q.arr[index].acquired) {
		runtime.Gosched()
		continue
	}
}

type queuePair struct {
	q0 unsafe.Pointer
	q1 unsafe.Pointer
	q unsafe.Pointer
}

func NewQueuePair(size uint32) (qp *queuePair) {
	qp = &queuePair{}
	qp.q0 = unsafe.Pointer(NewQueue(size))
	qp.q1 = unsafe.Pointer(NewQueue(size))
	qp.q = qp.q0
	return
}

// Switches the current queue for future pushes to proceed to the other queue
// so that there's no blocking. Assumes mutual exclusion across all drainers,
// however; this mutual exclusion is guaranteed by the mutex obtained by Drain
// at the start of execution.
//
// Returns a reference to the old queue.
func (qp *queuePair) switchQueues() (*queue) {
	if !atomic.CompareAndSwapPointer(&qp.q, qp.q0, qp.q1) {
		atomic.CompareAndSwapPointer(&qp.q, qp.q1, qp.q0)
		return (*queue) (qp.q1)
	} else {
		return (*queue) (qp.q0)
	}
}

// A circular buffer can store up to size elements (the most recent size
// elements, to be specific). A fixed size buffer is used so that there are no
// allocation costs at runtime.
type CircularBuffer struct {
	mu sync.Mutex
	qp []*queuePair
	qpn uint32
}

var errorInvalidCircularBufferSize = errors.New("Buffer size is not a power of two.")

// Allocates a circular buffer of size size and returns a reference to the
// struct. Only circular buffers of size 2^k are allowed (saves us from having
// to do expensive modulo operations).
func NewCircularBuffer(size uint32) (cb *CircularBuffer, err error) {
	if size & (size - 1) != 0 {
		err = errorInvalidCircularBufferSize
		return
	}

	cb = &CircularBuffer{}
	cb.qp = make([]*queuePair, numCircularBufferPairs)
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		cb.qp[i] = NewQueuePair(size / numCircularBufferPairs)
	}

	return
}

// Pushes an element in to the circular buffer.
func (cb *CircularBuffer) Push(x interface{}) {
	n := atomic.AddUint32(&cb.qpn, 1) & numCircularBufferPairsMask
	q := (*queue) (atomic.LoadPointer(&cb.qp[n].q))

	acquired := atomic.AddUint32(&q.acquired, 1) - 1

	if atomic.LoadUint32(&q.drainingPostCheck) > 0 {
		// Between our qc load and acquired increment, a drainer began execution
		// and switched the queues. This is NOT okay because we don't know if
		// acquired was incremented before or after the drainer's check for
		// acquired == writer. And we can't find this out without expensive
		// operations, which we'd like to avoid. If the acquired increment was
		// after, we cannot write to this buffer as the drainer's collection may
		// have already started; we must write to the other queue.
		//
		// Reverse our increment and retry. Since there's no SubUint32 in atomic,
		// ^uint32(0) is used to denote -1.
		// atomic.AddUint32(&q.acquired, ^uint32(0))
		return
	}

	// At this point, we're definitely writing to the right queue. That is, one
	// of the following is true: 
	//   1. No drainer is in execution.
	//   2. A drainer is in execution and it is waiting at the acquired barrier.
	index := acquired & q.mask
	addr := &q.arr[index].ptr
	old := atomic.LoadPointer(addr)

	// Even though we just verified that we haven't been wrapped around by
	// someone else, we cannot use a simple atomic store on the array
	// position because we may have been wrapped around between the acquired
	// check and the atomic store by someone else.
	//
	// As a result, we need a compare and swap operation to check that the
	// previously read value of the queue index is still the same. If the
	// compare and swap fails, we could either be the wrapper or the wrappee;
	// in either case, we'll simply retry the acquired check. Any push that has
	// been wrapped will fail that check and exit.
	//
	// This also makes the program safe in situations with more than two pushes
	// happening concurrently. For example, consider a situation where there
	// are three pushes A, B, C all in execution at the same time. Somehow, all
	// three get the same index with acquired being q.size apart. That is,
	// without any loss of generality, assume that:
	//
	//	 acquired_C = acquired_B + q.size = acquired_A + 2*q.size
	//
	// such that index is the same for all three. Let's say A and B complete
	// the first three steps; that is, item_A and item_B have been loaded
	// (let's call this value x0, denoting the value that the buffer slot held
	// before either push started execution). Now both pushes will check that
	// its acquired counter matches the queue's overall counter. Naturally, A
	// will fail since its acquired is at least q.size lower than the queue's
	// acquired counter. As a result, A will increment the written counter and
	// exit since the value it was about to write is stale anyway. At this
	// point, conventionally, B is considered to be the wrappee, and should be
	// allowed to proceed with a store at that position.
	//
	// But consider a situation where a push C begins execution just before
	// B's write to the slot happens. C completes every step up to and
	// including the step below successfully because it is the latest push.
	// If a regular atomic store was used instead of a compare-and-swap, B's
	// store would proceed successfully, producing incorrect output. The data
	// B was about to write to the buffer is now stale since it has been
	// superseded by a more up-to-date value (C). It should fail even though
	// it passed the acquired counter check. This is facilitated by a
	// compare-and-swap with B's previously read value in the buffer slot
	// (x0); the compare-and-swap will see that the slot no longer matches
	// the x0 value and will not swap the value. CompareAndSwap will return
	// false and B will need to retry its acquired counter check, which will
	// now fail, thanks to C's successful write. As a result, B will
	// correctly exit with a simple increment to the written counter without
	// touching the buffer itself.
	atomic.CompareAndSwapPointer(addr, old, unsafe.Pointer(&x))
	atomic.StoreUint32(&q.arr[index].acquired, acquired)
}

// Dereferences non-nil pointers from arr into result. Range of elements from
// arr that are copied is [from, to). Assumes that the result slice is already
// allocated and is large enough to hold all the elements that might be copied.
func dereferenceAppend(result []interface{}, arr []item, from, to uint32) ([]interface{}) {
	for i := from; i < to; i++ {
		x := (*interface{}) (atomic.LoadPointer(&arr[i].ptr))
		if x != nil {
			result = append(result, *x)
		}
		atomic.StoreUint32(&arr[i].acquired, ^uint32(0))
	}
	return result
}

// Allocates and returns an array of things pushed in to the circular buffer.
// Push order is not guaranteed; that is, if B was pushed after A, Drain may
// return B at a lower index than A in the returned array even if the circular
// buffer hasn't wrapped around.
func (cb *CircularBuffer) Drain() (result []interface{}) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	qs := make([]*queue, 0)
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		qs = append(qs, cb.qp[i].switchQueues())
	}

	// Wait for acquired == written after all queues have been switched so that
	// as few samples as thrown away by the drainingPostCheck check in Push.
	var wg sync.WaitGroup
	for i := uint32(0); i < numCircularBufferPairs; i++ {
		wg.Add(1)
		go func(qi uint32) {
			qs[qi].drainWait()
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
		qs[i].drainingPostCheck = 0
	}

	return
}
