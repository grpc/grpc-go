/*
 *
 * Copyright 2017 gRPC authors.
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

package stats

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

var printHistogram bool

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

//Sort method for durations
func (a durationSlice) Len() int           { return len(a) }
func (a durationSlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a durationSlice) Less(i, j int) bool { return a[i] < a[j] }

// maybeUpdate updates internal stat data if there was any newly added
// stats since this was updated.
func (stats *Stats) maybeUpdate() {
	if !stats.dirty {
		return
	}

	sort.Sort(stats.durations)
	stats.min = int64(stats.durations[0])
	stats.max = int64(stats.durations[len(stats.durations)-1])

	// Use the largest unit that can represent the minimum time duration.
	stats.unit = time.Nanosecond
	for _, u := range []time.Duration{time.Microsecond, time.Millisecond, time.Second} {
		if stats.min <= int64(u) {
			break
		}
		stats.unit = u
	}

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
		stats.histogram.Add(int64(d))
	}

	stats.dirty = false
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Print writes textual output of the Stats.
func (stats *Stats) Print(w io.Writer) {
	stats.maybeUpdate()
	if stats.durations.Len() != 0 {
		avg := float64(stats.histogram.Sum) / float64(stats.histogram.Count)
		var percentToObserve = []int64{50, 90, 99, 100}
		var timeUnit = fmt.Sprintf("%v", stats.unit)[1:]

		fmt.Fprintf(w, "Latency - unit: %s count: %d\n", timeUnit, stats.histogram.Count)
		fmt.Fprintf(w, "  %s\n", strings.Repeat("-", 28))
		//fmt.Fprintf(w, "stats.durations.Len(): %d, stats.hisgoram.count: %d \n", stats.durations.Len(), stats.histogram.Count)
		for _, position := range percentToObserve {
			fmt.Fprintf(w, "|   %*d%%   |   %9.4f %s   |\n", 3, position,
				float64(stats.durations[max(stats.histogram.Count*position/100-1, 0)])/float64(stats.unit), timeUnit)
		}
		fmt.Fprintf(w, "| Average  |   %9.4f %s   |\n", avg/float64(stats.unit), fmt.Sprintf("%v", stats.unit)[1:])
		fmt.Fprintf(w, "  %s\n", strings.Repeat("-", 28))
	} else {
		fmt.Fprint(w, "No stats data\n")
	}

	if printHistogram {
		if stats.histogram == nil {
			fmt.Fprint(w, "Histogram (empty)\n")
		} else {
			fmt.Fprintf(w, "Histogram (unit: %s)\n", fmt.Sprintf("%v", stats.unit)[1:])
			stats.histogram.Print(w)
		}
	}
}

// String returns the textual output of the Stats as string.
func (stats *Stats) String() string {
	var b bytes.Buffer
	stats.Print(&b)
	return b.String()
}
