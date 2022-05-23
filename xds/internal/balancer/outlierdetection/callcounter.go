/*
 *
 * Copyright 2022 gRPC authors.
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

package outlierdetection

import (
	"sync/atomic"
	"unsafe"
)

type bucket struct {
	numSuccesses  int64
	numFailures   int64
	requestVolume int64 // numSuccesses + numFailures, needed because this number will be used in interval timer algorithm
}

func newCallCounter() *callCounter {
	return &callCounter{
		activeBucket:   unsafe.Pointer(&bucket{}),
		inactiveBucket: &bucket{},
	}
}

type callCounter struct {
	// "The object contains two buckets, and each bucket has a number counting
	// successes, and another counting failures." - A50

	// activeBucket updates every time a call finishes (from picker passed to
	// Client Conn), so protect pointer read with atomic load of unsafe.Pointer
	// so picker does not have to grab a mutex per RPC, the critical path.
	activeBucket   unsafe.Pointer
	inactiveBucket *bucket
}

func (cc *callCounter) clear() {
	atomic.StorePointer(&cc.activeBucket, unsafe.Pointer(&bucket{}))
	cc.inactiveBucket = &bucket{}
}

// "When the timer triggers, the inactive bucket is zeroed and swapped with the
// active bucket. Then the inactive bucket contains the number of successes and
// failures since the last time the timer triggered. Those numbers are used to
// evaluate the ejection criteria." - A50
func (cc *callCounter) swap() {
	ab := (*bucket)(atomic.LoadPointer(&cc.activeBucket))
	// Don't do it exactly like defined but the same logically, as picker reads
	// ref to active bucket so instead of swapping the pointers (inducing race
	// conditions where picker writes to inactive bucket which is being used for
	// outlier detection algorithm, copy active bucket to new memory on heap,
	// picker updates which race simply write to deprecated heap memory
	// activeBucket used to point to). The other options is to do this as
	// defined, swap the pointers and have atomic reads of the Inactive Bucket
	// in interval timer algorithm, but I think this is cleaner in regards to
	// dealing with picker race condition. See the wrappedPicker explanation for
	// the write to activeBucket for a more in depth explanation.
	cc.inactiveBucket = &bucket{
		numSuccesses:  atomic.LoadInt64(&ab.numSuccesses),
		numFailures:   atomic.LoadInt64(&ab.numFailures),
		requestVolume: atomic.LoadInt64(&ab.requestVolume),
	}
	atomic.StorePointer(&cc.activeBucket, unsafe.Pointer(&bucket{}))
	// end result, same as in gRFC: the inactive bucket contains the number of
	// successes and failures since the last time the timer triggered.
}
