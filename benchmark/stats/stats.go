/*
 *
 * Copyright 2017, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package stats

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"time"
)

// Stats is a simple helper for gathering additional statistics like histogram
// during benchmarks. This is not thread safe.
type Stats struct {
	numBuckets int
	unit       time.Duration
	min, max   int64
	histogram  *Histogram

	durations durationSlice
	dirty     bool
}

type durationSlice []time.Duration

// NewStats creates a new Stats instance. If numBuckets is not positive,
// the default value (16) will be used.
func NewStats(numBuckets int) *Stats {
	if numBuckets <= 0 {
		numBuckets = 16
	}
	return &Stats{
		// Use one more bucket for the last unbounded bucket.
		numBuckets: numBuckets + 1,
		durations:  make(durationSlice, 0, 100000),
	}
}

// Add adds an elapsed time per operation to the stats.
func (stats *Stats) Add(d time.Duration) {
	stats.durations = append(stats.durations, d)
	stats.dirty = true
}

// Clear resets the stats, removing all values.
func (stats *Stats) Clear() {
	stats.durations = stats.durations[:0]
	stats.histogram = nil
	stats.dirty = false
}

// maybeUpdate updates internal stat data if there was any newly added
// stats since this was updated.
func (stats *Stats) maybeUpdate() {
	if !stats.dirty {
		return
	}

	stats.min = math.MaxInt64
	stats.max = 0
	for _, d := range stats.durations {
		if stats.min > int64(d) {
			stats.min = int64(d)
		}
		if stats.max < int64(d) {
			stats.max = int64(d)
		}
	}

	// Use the largest unit that can represent the minimum time duration.
	stats.unit = time.Nanosecond
	for _, u := range []time.Duration{time.Microsecond, time.Millisecond, time.Second} {
		if stats.min <= int64(u) {
			break
		}
		stats.unit = u
	}

	// Adjust the min/max according to the new unit.
	stats.min /= int64(stats.unit)
	stats.max /= int64(stats.unit)
	numBuckets := stats.numBuckets
	if n := int(stats.max - stats.min + 1); n < numBuckets {
		numBuckets = n
	}
	stats.histogram = NewHistogram(HistogramOptions{
		NumBuckets: numBuckets,
		// max-min(lower bound of last bucket) = (1 + growthFactor)^(numBuckets-2) * baseBucketSize.
		GrowthFactor:   math.Pow(float64(stats.max-stats.min), 1/float64(numBuckets-2)) - 1,
		BaseBucketSize: 1.0,
		MinValue:       stats.min})

	for _, d := range stats.durations {
		stats.histogram.Add(int64(d / stats.unit))
	}

	stats.dirty = false
}

// Print writes textual output of the Stats.
func (stats *Stats) Print(w io.Writer) {
	stats.maybeUpdate()

	if stats.histogram == nil {
		fmt.Fprint(w, "Histogram (empty)\n")
	} else {
		fmt.Fprintf(w, "Histogram (unit: %s)\n", fmt.Sprintf("%v", stats.unit)[1:])
		stats.histogram.Print(w)
	}
}

// String returns the textual output of the Stats as string.
func (stats *Stats) String() string {
	var b bytes.Buffer
	stats.Print(&b)
	return b.String()
}
