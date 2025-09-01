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
	"testing"

	"github.com/google/go-cmp/cmp"
)

func (b1 *bucket) Equal(b2 *bucket) bool {
	if b1 == nil && b2 == nil {
		return true
	}
	if (b1 != nil) != (b2 != nil) {
		return false
	}
	if b1.numSuccesses != b2.numSuccesses {
		return false
	}
	return b1.numFailures == b2.numFailures
}

func (cc *callCounter) Equal(cc2 *callCounter) bool {
	if cc == nil && cc2 == nil {
		return true
	}
	if (cc != nil) != (cc2 != nil) {
		return false
	}
	ab1 := cc.activeBucket.Load()
	ab2 := cc2.activeBucket.Load()
	if !ab1.Equal(ab2) {
		return false
	}
	return cc.inactiveBucket.Equal(cc2.inactiveBucket)
}

// TestClear tests that clear on the call counter clears (everything set to 0)
// the active and inactive buckets.
func (s) TestClear(t *testing.T) {
	cc := newCallCounter()
	ab := cc.activeBucket.Load()
	ab.numSuccesses = 1
	ab.numFailures = 2
	cc.inactiveBucket.numSuccesses = 4
	cc.inactiveBucket.numFailures = 5
	cc.clear()
	// Both the active and inactive buckets should be cleared.
	ccWant := newCallCounter()
	if diff := cmp.Diff(cc, ccWant); diff != "" {
		t.Fatalf("callCounter is different than expected, diff (-got +want): %v", diff)
	}
}

// TestSwap tests that swap() on the callCounter successfully has the desired
// end result of inactive bucket containing the previous active buckets data,
// and the active bucket being cleared.
func (s) TestSwap(t *testing.T) {
	cc := newCallCounter()
	ab := cc.activeBucket.Load()
	ab.numSuccesses = 1
	ab.numFailures = 2
	cc.inactiveBucket.numSuccesses = 4
	cc.inactiveBucket.numFailures = 5
	ib := cc.inactiveBucket
	cc.swap()
	// Inactive should pick up active's data, active should be swapped to zeroed
	// inactive.
	ccWant := newCallCounter()
	ccWant.inactiveBucket.numSuccesses = 1
	ccWant.inactiveBucket.numFailures = 2
	ccWant.activeBucket.Store(ib)
	if diff := cmp.Diff(cc, ccWant); diff != "" {
		t.Fatalf("callCounter is different than expected, diff (-got +want): %v", diff)
	}
}
