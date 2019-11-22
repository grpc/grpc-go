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

// The profiling package contains two logical components: buffer.go and
// profiling.go. The former implements a circular buffer (a.k.a. ring buffer)
// in a lock-free manner using atomics. This ring buffer is used by
// profiling.go to store various statistics. For example, StreamStats is a
// circular buffer of Stat objects, each of which is comprised of Timers.
//
// This abstraction is designed to accommodate more stats in the future; for
// example, if one wants to profile the load balancing layer, which is
// independent of RPC queries, a separate CircularBuffer can be used.
//
// Note that the circular buffer simply takes any interface{}. In the future,
// more types of measurements (such as the number of memory allocations) could
// be measured, which might require a different type of object being pushed
// into the circular buffer.

package profiling

import (
	"sync"
	"sync/atomic"
	"time"
)

// 0 or 1 representing profiling off and on, respectively. Use IsEnabled and
// SetEnabled to get and set this in a safe manner.
var profilingEnabled uint32

// IsEnabled returns whether or not profiling is enabled.
func IsEnabled() bool {
	return atomic.LoadUint32(&profilingEnabled) > 0
}

// Enable turns profiling on and off.
//
// Note that it is impossible to enable profiling for one server and leave it
// turned off for another. This is intentional and by design -- if the status
// of profiling was server-specific, clients wouldn't be able to profile
// themselves. As a result, SetEnabled turns profiling on and off for all
// servers and clients in the binary. Each stat will be, however, tagged with
// whether it's a client stat or a server stat; so you should be able to filter
// for the right type of stats in post-processing.
// SetEnabled is the internal
func Enable(enabled bool) {
	if enabled {
		atomic.StoreUint32(&profilingEnabled, 1)
	} else {
		atomic.StoreUint32(&profilingEnabled, 0)
	}
}

// A Timer represents the wall-clock beginning and ending of a logical
// operation.
type Timer struct {
	// Each Timer has a tag that identifies it uniquely within a Stat. This is
	// usually a forward-slash-separated hierarchical string.
	TimerTag string
	// Begin marks the beginning of this timer. The timezone is unspecified, but
	// must use the same timezone as End; this is so shave off the small, but
	// non-zero time required to convert to a standard timezone such as UTC.
	Begin time.Time
	// End marks the end of a timer.
	End time.Time
	// Each Timer must be started and ended within the same goroutine; GoID
	// captures this goroutine ID. The Go runtime doesn not typically expose this
	// information, so this is set to zero in the typical case. However, a
	// trivial patch to the runtime package can make this field useful. See
	// goid_modified.go in this package for more details.
	GoID int64
}

// NewTimer creates and returns a new Timer object. This is useful when you
// don't already have a Stat object to associate this Timer with; for example,
// before the context of a new RPC query is created, a Timer may be needed to
// measure transport-related operations.
//
// Use AppendTimer to append the returned Timer to a Stat.
func NewTimer(timerTag string) *Timer {
	return &Timer{
		TimerTag: timerTag,
		Begin:    time.Now(),
		GoID:     goid(),
	}
}

// A Stat is a collection of Timers that represent timing information for
// different components within this Stat. For example, a Stat may be used to
// reference the entire lifetime of an RPC request, with Timers within it
// representing different components such as encoding, compression, and
// transport.
//
// The user is expected to use the included helper functions to do operations
// on the Stat such as creating or appending a new timer. Direct operations on
// the Stat's exported fields (which are exported for encoding reasons) may
// lead to data races.
type Stat struct {
	// Each Stat has a tag associated with it to identify it in post-processing.
	// This is can be any arbitrary string.
	StatTag string
	// Stats may also need to store other unstructured information specific to
	// this stat. For example, a StreamStat will use these bytes to encode the
	// connection number and the stream ID for each RPC, thereby uniquely
	// identifying it. The underlying encoding for this is unspecified.
	Metadata []byte
	// A collection of Timers and a mutex for append operations on the slice.
	mu     sync.Mutex
	Timers []Timer
}

// A power of two that's large enough to hold all timers within an average RPC
// request (defined to be a unary request) without any reallocation. A typical
// unary RPC creates 80-100 timers for various things. While this number is
// purely anecdotal and may change in the future as the resolution of profiling
// increases or decreases, it serves as a good estimate for what the initial
// allocation size should be.
const defaultStatAllocatedTimers int32 = 128

// NewStat creates and returns a new Stat object.
func NewStat(statTag string) *Stat {
	return &Stat{
		StatTag: statTag,
		Timers:  make([]Timer, 0, defaultStatAllocatedTimers),
	}
}

// NewTimer creates a Timer object within the given stat if stat is non-nil.
// The value passed in timerTag will be attached to the newly created Timer.
// NewTimer also automatically sets the Begin value of the Timer to the current
// time.
//
// The user is expected to call stat.Egress with the returned index as argument
// to mark the end. The return value is not a pointer to a Timer object because
// the internal slice may be re-allocated freely, which would make the
// reference to pointers of the past obsolete.
//
// Why a slice of timer? To make NewTimer allocation-free, we don't allocate a
// new timer every time. Instead a slice of timers large enough to hold most
// data is created and elements from this slice are used. This should put less
// pressure on the GC too.
func (stat *Stat) NewTimer(timerTag string) int {
	if stat == nil {
		return 0
	}

	// TODO(adtac): Make this lock-free? We don't expect much contention on
	// this, so I don't predict a significant improvement in performance. Using
	// a lock-free, concurrent queue will require us to write our own
	// reallocation and copy, which is probably unnecessary code complexity if
	// the improvement isn't significant.
	stat.mu.Lock()
	stat.Timers = append(stat.Timers, Timer{
		TimerTag: timerTag,
		Begin:    time.Now(),
		GoID:     goid(),
	})
	index := len(stat.Timers) - 1
	stat.mu.Unlock()
	return index
}

// Egress marks the completion of a given timer within a stat.
func (stat *Stat) Egress(index int) {
	if stat == nil {
		return
	}

	stat.mu.Lock()
	if index < len(stat.Timers) {
		stat.Timers[index].End = time.Now()
	}
	stat.mu.Unlock()
}

// AppendTimer appends a given Timer object to the internal slice of timers. A
// deep copy of the timer is made (i.e. no reference is retained to this
// pointer) and the user is expected to lose their reference to the timer to
// allow the Timer object to be garbage collected.
func (stat *Stat) AppendTimer(timer *Timer) int {
	if stat == nil || timer == nil {
		return 0
	}

	stat.mu.Lock()
	stat.Timers = append(stat.Timers, *timer)
	index := len(stat.Timers) - 1
	stat.mu.Unlock()
	return index
}

// ServerConnectionCounter counts the number of connections a server has seen.
// This counter is embedded within a StreamStat's Metadata along with each
// stream's stream ID to uniquely identify a query. Accessed atomically.
var ServerConnectionCounter uint64

// ClientConnectionCounter counts the number of connections a client has
// initiated. This counter is embedded within a StreamStat's Metadata along
// with each stream's stream ID to uniquely identify a query. Accessed
// atomically.
var ClientConnectionCounter uint64

// statsInitialized is 0 before InitStats has been called. Changed to 1 by
// exactly one call to InitStats.
var statsInitialized int32

// Stats for the last defaultStreamStatsBufsize RPCs will be stored in memory.
// This is can be configured by the registering server at profiling service
// initialization with google.golang.org/grpc/profiling/service.ProfilingConfig
const defaultStreamStatsSize uint32 = 16 << 10

// StreamStats is a CircularBuffer containing data from the last N RPC calls
// served, where N is set by the user. This will contain both server stats and
// client stats (but each stat will be tagged with whether it's a server or a
// client in its StatTag).
var StreamStats *circularBuffer

// InitStats initializes all the relevant Stat objects. Must be called exactly
// once per lifetime of a process; calls after the first one are ignored.
func InitStats(streamStatsSize uint32) error {
	var err error
	if !atomic.CompareAndSwapInt32(&statsInitialized, 0, 1) {
		// If initialized, do nothing.
		return nil
	}

	if streamStatsSize == 0 {
		streamStatsSize = defaultStreamStatsSize
	}

	StreamStats, err = newCircularBuffer(streamStatsSize)
	if err != nil {
		return err
	}

	return nil
}
