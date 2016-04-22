package stats

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// HistogramValue is the value of Histogram objects.
type HistogramValue struct {
	// Count is the total number of values added to the histogram.
	Count int64
	// Sum is the sum of all the values added to the histogram.
	Sum int64
	// SumOfSquares is the sum of squares of all values.
	SumOfSquares int64
	// Min is the minimum of all the values added to the histogram.
	Min int64
	// Max is the maximum of all the values added to the histogram.
	Max int64
	// Buckets contains all the buckets of the histogram.
	Buckets []HistogramBucket
}

// HistogramBucket is one histogram bucket.
type HistogramBucket struct {
	// LowBound is the lower bound of the bucket.
	LowBound float64
	// Count is the number of values in the bucket.
	Count int64
}

// Print writes textual output of the histogram values.
func (v HistogramValue) Print(w io.Writer) {
	avg := float64(v.Sum) / float64(v.Count)
	fmt.Fprintf(w, "Count: %d  Min: %d  Max: %d  Avg: %.2f\n", v.Count, v.Min, v.Max, avg)
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 60))
	if v.Count <= 0 {
		return
	}

	maxBucketDigitLen := len(strconv.FormatFloat(v.Buckets[len(v.Buckets)-1].LowBound, 'f', 6, 64))
	if maxBucketDigitLen < 3 {
		// For "inf".
		maxBucketDigitLen = 3
	}
	maxCountDigitLen := len(strconv.FormatInt(v.Count, 10))
	percentMulti := 100 / float64(v.Count)

	accCount := int64(0)
	for i, b := range v.Buckets {
		fmt.Fprintf(w, "[%*f, ", maxBucketDigitLen, b.LowBound)
		if i+1 < len(v.Buckets) {
			fmt.Fprintf(w, "%*f)", maxBucketDigitLen, v.Buckets[i+1].LowBound)
		} else {
			fmt.Fprintf(w, "%*s)", maxBucketDigitLen, "inf")
		}

		accCount += b.Count
		fmt.Fprintf(w, "  %*d  %5.1f%%  %5.1f%%", maxCountDigitLen, b.Count, float64(b.Count)*percentMulti, float64(accCount)*percentMulti)

		const barScale = 0.1
		barLength := int(float64(b.Count)*percentMulti*barScale + 0.5)
		fmt.Fprintf(w, "  %s\n", strings.Repeat("#", barLength))
	}
}

// String returns the textual output of the histogram values as string.
func (v HistogramValue) String() string {
	var b bytes.Buffer
	v.Print(&b)
	return b.String()
}

// Histogram accumulates values in the form of a histogram with
// exponentially increased bucket sizes.
// The first bucket (with index 0) is [0, n) where n = baseBucketSize.
// Bucket i (i>=1) contains [n * m^(i-1), n * m^i), where m = 1 + GrowthFactor.
// The type of the values is int64.
type Histogram struct {
	opts         HistogramOptions
	buckets      []bucketInternal
	count        *Counter
	sum          *Counter
	sumOfSquares *Counter
	tracker      *Tracker

	logBaseBucketSize             float64
	oneOverLogOnePlusGrowthFactor float64
}

// HistogramOptions contains the parameters that define the histogram's buckets.
type HistogramOptions struct {
	// NumBuckets is the number of buckets.
	NumBuckets int
	// GrowthFactor is the growth factor of the buckets. A value of 0.1
	// indicates that bucket N+1 will be 10% larger than bucket N.
	GrowthFactor float64
	// BaseBucketSize is the size of the first bucket.
	BaseBucketSize float64
	// MinValue is the lower bound of the first bucket.
	MinValue int64
}

// bucketInternal is the internal representation of a bucket, which includes a
// rate counter.
type bucketInternal struct {
	lowBound float64
	count    *Counter
}

// NewHistogram returns a pointer to a new Histogram object that was created
// with the provided options.
func NewHistogram(opts HistogramOptions) *Histogram {
	if opts.NumBuckets == 0 {
		opts.NumBuckets = 32
	}
	if opts.BaseBucketSize == 0.0 {
		opts.BaseBucketSize = 1.0
	}
	h := Histogram{
		opts:         opts,
		buckets:      make([]bucketInternal, opts.NumBuckets),
		count:        newCounter(),
		sum:          newCounter(),
		sumOfSquares: newCounter(),
		tracker:      newTracker(),

		logBaseBucketSize:             math.Log(opts.BaseBucketSize),
		oneOverLogOnePlusGrowthFactor: 1 / math.Log(1+opts.GrowthFactor),
	}
	m := 1.0 + opts.GrowthFactor
	delta := opts.BaseBucketSize
	h.buckets[0].lowBound = float64(opts.MinValue)
	h.buckets[0].count = newCounter()
	for i := 1; i < opts.NumBuckets; i++ {
		h.buckets[i].lowBound = float64(opts.MinValue) + delta
		h.buckets[i].count = newCounter()
		delta = delta * m
	}
	return &h
}

// Opts returns a copy of the options used to create the Histogram.
func (h *Histogram) Opts() HistogramOptions {
	return h.opts
}

// Add adds a value to the histogram.
func (h *Histogram) Add(value int64) error {
	bucket, err := h.findBucket(value)
	if err != nil {
		return err
	}
	h.buckets[bucket].count.Incr(1)
	h.count.Incr(1)
	h.sum.Incr(value)
	h.sumOfSquares.Incr(value * value)
	h.tracker.Push(value)
	return nil
}

// LastUpdate returns the time at which the object was last updated.
func (h *Histogram) LastUpdate() time.Time {
	return h.count.LastUpdate()
}

// Value returns the accumulated state of the histogram since it was created.
func (h *Histogram) Value() HistogramValue {
	b := make([]HistogramBucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = HistogramBucket{
			LowBound: v.lowBound,
			Count:    v.count.Value(),
		}
	}

	v := HistogramValue{
		Count:        h.count.Value(),
		Sum:          h.sum.Value(),
		SumOfSquares: h.sumOfSquares.Value(),
		Min:          h.tracker.Min(),
		Max:          h.tracker.Max(),
		Buckets:      b,
	}
	return v
}

// Delta1h returns the change in the last hour.
func (h *Histogram) Delta1h() HistogramValue {
	b := make([]HistogramBucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = HistogramBucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta1h(),
		}
	}

	v := HistogramValue{
		Count:        h.count.Delta1h(),
		Sum:          h.sum.Delta1h(),
		SumOfSquares: h.sumOfSquares.Delta1h(),
		Min:          h.tracker.Min1h(),
		Max:          h.tracker.Max1h(),
		Buckets:      b,
	}
	return v
}

// Delta10m returns the change in the last 10 minutes.
func (h *Histogram) Delta10m() HistogramValue {
	b := make([]HistogramBucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = HistogramBucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta10m(),
		}
	}

	v := HistogramValue{
		Count:        h.count.Delta10m(),
		Sum:          h.sum.Delta10m(),
		SumOfSquares: h.sumOfSquares.Delta10m(),
		Min:          h.tracker.Min10m(),
		Max:          h.tracker.Max10m(),
		Buckets:      b,
	}
	return v
}

// Delta1m returns the change in the last 10 minutes.
func (h *Histogram) Delta1m() HistogramValue {
	b := make([]HistogramBucket, len(h.buckets))
	for i, v := range h.buckets {
		b[i] = HistogramBucket{
			LowBound: v.lowBound,
			Count:    v.count.Delta1m(),
		}
	}

	v := HistogramValue{
		Count:        h.count.Delta1m(),
		Sum:          h.sum.Delta1m(),
		SumOfSquares: h.sumOfSquares.Delta1m(),
		Min:          h.tracker.Min1m(),
		Max:          h.tracker.Max1m(),
		Buckets:      b,
	}
	return v
}

func (h *Histogram) findBucket(value int64) (int, error) {
	delta := float64(value - h.opts.MinValue)
	var b int
	if delta >= h.opts.BaseBucketSize {
		// b = log_{1+growthFactor} (delta / baseBucketSize) + 1
		//   = log(delta / baseBucketSize) / log(1+growthFactor) + 1
		//   = (log(delta) - log(baseBucketSize)) * (1 / log(1+growthFactor)) + 1
		b = int((math.Log(delta)-h.logBaseBucketSize)*h.oneOverLogOnePlusGrowthFactor + 1)
	}
	if b >= len(h.buckets) {
		return 0, fmt.Errorf("no bucket for value: %d", value)
	}
	return b, nil
}
