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
 *
 */

package transport

import (
	"math"
	"sync"
	"testing"
	"time"
)

func (s) TestBDPEstimator_ConcurrentAccess(*testing.T) {
	b := &bdpEstimator{
		bdp:               initialWindowSize,
		updateFlowControl: func(uint32) {},
	}

	const iterations = 1000
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < iterations; i++ {
			b.timesnap(bdpPing.data)
		}
	})
	wg.Go(func() {
		for i := 0; i < iterations; i++ {
			b.calculate(bdpPing.data)
		}
	})
	wg.Go(func() {
		for i := 0; i < iterations; i++ {
			b.add(100)
		}
	})
	wg.Wait()
}

// ackBDPPing acks the outstanding bdp ping as if it had been sent one
// millisecond ago. The rtt samples then stay close to one millisecond. So
// bandwidth samples differ mostly by size and a much larger sample sets a new
// bwMax.
func ackBDPPing(b *bdpEstimator) {
	b.mu.Lock()
	b.sentAt = time.Now().Add(-time.Millisecond)
	b.mu.Unlock()
	b.calculate(bdpPing.data)
}

// TestBDPEstimator_AckBeforeFirstPing checks that an ack before the first BDP
// ping leaves rtt finite. A later large sample then still grows the estimate.
func (s) TestBDPEstimator_AckBeforeFirstPing(t *testing.T) {
	var gotUpdate uint32
	b := &bdpEstimator{
		bdp:               initialWindowSize,
		updateFlowControl: func(n uint32) { gotUpdate = n },
	}
	b.calculate(bdpPing.data)
	if math.IsNaN(b.rtt) || math.IsInf(b.rtt, 0) {
		t.Fatalf("rtt = %v after an ack before the first ping, want a finite value", b.rtt)
	}

	const frameSize = 16 * 1024
	const frames = 4
	for i := range frames {
		if got, want := b.add(frameSize), i == 0; got != want {
			t.Fatalf("add() on frame %d = %v, want %v", i, got, want)
		}
	}
	ackBDPPing(b)
	if wantBDP := uint32(gamma * frames * frameSize); gotUpdate != wantBDP || b.bdp != wantBDP {
		t.Fatalf("Estimate = %d with update %d, want %d", b.bdp, gotUpdate, wantBDP)
	}
}
