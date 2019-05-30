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

// Package stats tracks the statistics associated with benchmark runs.
package stats

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

// FeatureIndex is an enum for features that usually differ across individual
// benchmark runs in a single execution. These are usually configured by the
// user through command line flags.
type FeatureIndex int

const (
	EnableTraceIndex FeatureIndex = iota
	ReadLatenciesIndex
	ReadKbpsIndex
	ReadMTUIndex
	MaxConcurrentCallsIndex
	ReqSizeBytesIndex
	RespSizeBytesIndex
	CompModesIndex
	EnableChannelzIndex
	EnablePreloaderIndex

	// This is a place holder to indicate the total number of feature indices we
	// have. Any new feature indices should be added above this.
	MaxFeatureIndex
)

// Features represent configured options for a specific benchmark run. This is
// usually constructed from command line arguments passed by the caller. See
// benchmark/benchmain/main.go for defined command line flags. This is also
// part of the BenchResults struct which is serialized and written to a file.
type Features struct {
	// Network mode used for this benchmark run. Could be one of Local, LAN, WAN
	// or Longhaul.
	NetworkMode string
	// UseBufCon indicates whether an in-memory connection was used for this
	// benchmark run instead of system network I/O.
	UseBufConn bool
	// EnableKeepalive indicates if keepalives were enabled on the connections
	// used in this benchmark run.
	EnableKeepalive bool
	// BenchTime indicates the duration of the benchmark run.
	BenchTime time.Duration

	// Features defined above are usually the same for all benchmark runs in a
	// particular invocation, while the features defined below could vary from
	// run to run based on the configured command line. These features have a
	// corresponding featureIndex value which is used for a variety of reasons.

	// EnableTrace indicates if tracing was enabled.
	EnableTrace bool
	// Latency is the simulated one-way network latency used.
	Latency time.Duration
	// Kbps is the simulated network throughput used.
	Kbps int
	// MTU is the simulated network MTU used.
	MTU int
	// MaxConcurrentCalls is the number of concurrent RPCs made during this
	// benchmark run.
	MaxConcurrentCalls int
	// ReqSizeBytes is the request size in bytes used in this benchmark run.
	ReqSizeBytes int
	// RespSizeBytes is the response size in bytes used in this benchmark run.
	RespSizeBytes int
	// ModeCompressor represents the compressor mode used.
	ModeCompressor string
	// EnableChannelz indicates if channelz was turned on.
	EnableChannelz bool
	// EnablePreloader indicates if preloading was turned on.
	EnablePreloader bool
}

// String returns all the feature values as a string.
func (f Features) String() string {
	return fmt.Sprintf("networkMode_%v-bufConn_%v-keepalive_%v-benchTime_%v-"+
		"trace_%v-latency_%v-kbps_%v-MTU_%v-maxConcurrentCalls_%v-"+
		"reqSize_%vB-respSize_%vB-compressor_%v-channelz_%v-preloader_%v",
		f.NetworkMode, f.UseBufConn, f.EnableKeepalive, f.BenchTime,
		f.EnableTrace, f.Latency, f.Kbps, f.MTU, f.MaxConcurrentCalls,
		f.ReqSizeBytes, f.RespSizeBytes, f.ModeCompressor, f.EnableChannelz, f.EnablePreloader)
}

// SharedFeatures returns the shared features as a pretty printable string.
// 'wantFeatures' is a bitmask of wanted features, indexed by FeaturesIndex.
func (f Features) SharedFeatures(wantFeatures []bool) string {
	var b bytes.Buffer
	if f.NetworkMode != "" {
		b.WriteString(fmt.Sprintf("Network: %v\n", f.NetworkMode))
	}
	if f.UseBufConn {
		b.WriteString(fmt.Sprintf("UseBufConn: %v\n", f.UseBufConn))
	}
	if f.EnableKeepalive {
		b.WriteString(fmt.Sprintf("EnableKeepalive: %v\n", f.EnableKeepalive))
	}
	b.WriteString(fmt.Sprintf("BenchTime: %v\n", f.BenchTime))
	f.partialString(&b, wantFeatures, ": ", "\n")
	return b.String()
}

// Name returns a one line name which includes the features specified by
// 'wantFeatures' which is a bitmask of wanted features, indexed by
// FeaturesIndex.
func (f Features) PrintableName(wantFeatures []bool) string {
	var b bytes.Buffer
	f.partialString(&b, wantFeatures, "_", "-")
	return b.String()
}

// partialString writes features specified by 'wantFeatures' to the provided
// bytes.Buffer.
func (f Features) partialString(b *bytes.Buffer, wantFeatures []bool, sep, delim string) {
	for i, sf := range wantFeatures {
		if sf {
			switch FeatureIndex(i) {
			case EnableTraceIndex:
				b.WriteString(fmt.Sprintf("Trace%v%v%v", sep, f.EnableTrace, delim))
			case ReadLatenciesIndex:
				b.WriteString(fmt.Sprintf("Latency%v%v%v", sep, f.Latency, delim))
			case ReadKbpsIndex:
				b.WriteString(fmt.Sprintf("Kbps%v%v%v", sep, f.Kbps, delim))
			case ReadMTUIndex:
				b.WriteString(fmt.Sprintf("MTU%v%v%v", sep, f.MTU, delim))
			case MaxConcurrentCallsIndex:
				b.WriteString(fmt.Sprintf("Callers%v%v%v", sep, f.MaxConcurrentCalls, delim))
			case ReqSizeBytesIndex:
				b.WriteString(fmt.Sprintf("Request size%v%vB%v", sep, f.ReqSizeBytes, delim))
			case RespSizeBytesIndex:
				b.WriteString(fmt.Sprintf("Response size%v%vB%v", sep, f.RespSizeBytes, delim))
			case CompModesIndex:
				b.WriteString(fmt.Sprintf("Compressor %v%v%v", sep, f.ModeCompressor, delim))
			case EnableChannelzIndex:
				b.WriteString(fmt.Sprintf("Channelz%v%v%v", sep, f.EnableChannelz, delim))
			case EnablePreloaderIndex:
				b.WriteString(fmt.Sprintf("Preloader%v%v%v", sep, f.EnablePreloader, delim))
			default:
				log.Fatalf("Unknown feature index %v. maxFeatureIndex is %v", i, MaxFeatureIndex)
			}
		}
	}
}

// BenchResults records features and results of a benchmark run. A collection
// of these structs is usually serialized and written to a file after a
// benchmark execution, and could later be read for pretty-printing or
// comparison with other benchmark results.
type BenchResults struct {
	// RunMode is the workload mode for this benchmark run. This could be unary,
	// stream or unconstrained.
	RunMode string
	// Features represents the configured feature options for this run.
	Features Features
	// SharedFeatures represents the features which were shared across all
	// benchmark runs during one execution. It is a slice indexed by
	// 'FeaturesIndex' and a value of true indicates that the associated
	// feature is shared across all runs.
	SharedFeatures []bool
	// Data contains the statistical data of interest from the benchmark run.
	Data RunData
}

// RunData contains statistical data of interest from a benchmark run.
type RunData struct {
	// TotalOps is the number of operations executed during this benchmark run.
	// Only makes sense for unary and streaming workloads.
	TotalOps uint64
	// SendOps is the number of send operations executed during this benchmark
	// run. Only makes sense for unconstrained workloads.
	SendOps uint64
	// RecvOps is the number of receive operations executed during this benchmark
	// run. Only makes sense for unconstrained workloads.
	RecvOps uint64
	// AllocedBytes is the average memory allocation in bytes per operation.
	AllocedBytes uint64
	// Allocs is the average number of memory allocations per operation.
	Allocs uint64
	// ReqT is the average request throughput associated with this run.
	ReqT float64
	// RespT is the average response throughput associated with this run.
	RespT float64
	// Latencies stores the different latencies associated with this run.  For
	// unary and streaming workloads, we will have one entry in this slice which
	// captures latencies for the whole call (sending and receiving), while for
	// unconstrained workloads, this slice will contain separate send (index 0)
	// and receive (index 1) latencies.
	Latencies []Latency
}

// Latency groups different latencies of interest from a benchmark run.
type Latency struct {
	// Fiftieth is the 50th percentile latency.
	Fiftieth time.Duration
	// Ninetieth is the 90th percentile latency.
	Ninetieth time.Duration
	// Ninetyninth is the 99th percentile latency.
	NinetyNinth time.Duration
	// Average is the average latency.
	Average time.Duration
}

type durationSlice []time.Duration

func (a durationSlice) Len() int           { return len(a) }
func (a durationSlice) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a durationSlice) Less(i, j int) bool { return a[i] < a[j] }

// Stats is a helper for gathering statistics about individual benchmark runs.
type Stats struct {
	mu         sync.Mutex
	numBuckets int
	req        *histWrapper
	resp       *histWrapper
	results    []BenchResults
	startMS    runtime.MemStats
	stopMS     runtime.MemStats
}

type histWrapper struct {
	unit      time.Duration
	histogram *Histogram
	durations durationSlice
}

// NewStats creates a new Stats instance. If numBuckets is not positive, the
// default value (16) will be used.
func NewStats(numBuckets int) *Stats {
	if numBuckets <= 0 {
		numBuckets = 16
	}
	// Use one more bucket for the last unbounded bucket.
	s := &Stats{numBuckets: numBuckets + 1}
	s.req = &histWrapper{}
	s.resp = &histWrapper{}
	return s
}

// StartRun is to be invoked to indicate the start of a new benchmark run.
func (s *Stats) StartRun(mode string, f Features, sf []bool) {
	s.mu.Lock()
	runtime.ReadMemStats(&s.startMS)
	s.results = append(s.results, BenchResults{RunMode: mode, Features: f, SharedFeatures: sf})
	s.mu.Unlock()
}

// EndRun is to be invoked to indicate the end of the ongoing benchmark run. It
// computes a bunch of stats and dumps them to stdout.
func (s *Stats) EndRun(count uint64) {
	s.mu.Lock()
	runtime.ReadMemStats(&s.stopMS)
	r := &s.results[len(s.results)-1]
	r.Data = RunData{
		TotalOps:     count,
		AllocedBytes: s.stopMS.TotalAlloc - s.startMS.TotalAlloc,
		Allocs:       s.stopMS.Mallocs - s.startMS.Mallocs,
		ReqT:         float64(count) * float64(r.Features.ReqSizeBytes) * 8 / r.Features.BenchTime.Seconds(),
		RespT:        float64(count) * float64(r.Features.RespSizeBytes) * 8 / r.Features.BenchTime.Seconds(),
	}
	s.computeLatencies(r)
	s.dump(r)
	s.req = &histWrapper{}
	s.resp = &histWrapper{}
	s.mu.Unlock()
}

// EndUnconstrainedRun is similar to EndRun, but is to be used for
// unconstrained workloads.
func (s *Stats) EndUnconstrainedRun(req uint64, resp uint64) {
	s.mu.Lock()
	runtime.ReadMemStats(&s.stopMS)
	r := &s.results[len(s.results)-1]
	r.Data = RunData{
		SendOps:      req,
		RecvOps:      resp,
		AllocedBytes: (s.stopMS.TotalAlloc - s.startMS.TotalAlloc) / ((req + resp) / 2),
		Allocs:       (s.stopMS.Mallocs - s.startMS.Mallocs) / ((req + resp) / 2),
		ReqT:         float64(req) * float64(r.Features.ReqSizeBytes) * 8 / r.Features.BenchTime.Seconds(),
		RespT:        float64(resp) * float64(r.Features.RespSizeBytes) * 8 / r.Features.BenchTime.Seconds(),
	}
	s.computeLatencies(r)
	s.dump(r)
	s.req = &histWrapper{}
	s.resp = &histWrapper{}
	s.mu.Unlock()
}

// AddDuration adds an elapsed duration per operation to the stats. This is
// used by unary and stream modes where request and response stats are equal.
func (s *Stats) AddDuration(d time.Duration) {
	s.mu.Lock()
	s.req.durations = append(s.req.durations, d)
	s.mu.Unlock()
}

// AddReqDuration adds an elapsed request duration per operation to the stats.
func (s *Stats) AddReqDuration(d time.Duration) {
	s.mu.Lock()
	s.req.durations = append(s.req.durations, d)
	s.mu.Unlock()
}

// AddRespDuration adds an elapsed response duration per operation to the
// stats.
func (s *Stats) AddRespDuration(d time.Duration) {
	s.mu.Lock()
	s.resp.durations = append(s.resp.durations, d)
	s.mu.Unlock()
}

// GetResults returns the results from all benchmark runs.
func (s *Stats) GetResults() []BenchResults {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.results
}

// computeLatencies computes percentile latencies based on durations stored in
// the stats object and updates the corresponding fields in the result object.
func (s *Stats) computeLatencies(result *BenchResults) {
	for _, hw := range []*histWrapper{s.req, s.resp} {
		if len(hw.durations) == 0 {
			continue
		}
		sort.Sort(hw.durations)
		minDuration := int64(hw.durations[0])
		maxDuration := int64(hw.durations[len(hw.durations)-1])

		// Use the largest unit that can represent the minimum time duration.
		hw.unit = time.Nanosecond
		for _, u := range []time.Duration{time.Microsecond, time.Millisecond, time.Second} {
			if minDuration <= int64(u) {
				break
			}
			hw.unit = u
		}

		numBuckets := s.numBuckets
		if n := int(maxDuration - minDuration + 1); n < numBuckets {
			numBuckets = n
		}
		hw.histogram = NewHistogram(HistogramOptions{
			NumBuckets: numBuckets,
			// max-min(lower bound of last bucket) = (1 + growthFactor)^(numBuckets-2) * baseBucketSize.
			GrowthFactor:   math.Pow(float64(maxDuration-minDuration), 1/float64(numBuckets-2)) - 1,
			BaseBucketSize: 1.0,
			MinValue:       minDuration,
		})
		for _, d := range hw.durations {
			hw.histogram.Add(int64(d))
		}
		result.Data.Latencies = append(result.Data.Latencies, Latency{
			Fiftieth:    hw.durations[max(hw.histogram.Count*int64(50)/100-1, 0)],
			Ninetieth:   hw.durations[max(hw.histogram.Count*int64(90)/100-1, 0)],
			NinetyNinth: hw.durations[max(hw.histogram.Count*int64(99)/100-1, 0)],
			Average:     time.Duration(float64(hw.histogram.Sum) / float64(hw.histogram.Count)),
		})
	}
}

// dump returns a printable version.
func (s *Stats) dump(result *BenchResults) {
	var b bytes.Buffer
	// This prints the run mode and all features of the bench on a line.
	b.WriteString(fmt.Sprintf("%s-%s:\n", result.RunMode, result.Features.String()))

	for i, l := range result.Data.Latencies {
		var unit time.Duration
		var dir, tUnit string
		var hist *Histogram
		if i == 0 {
			if len(result.Data.Latencies) > 1 {
				dir = "Requests: "
			}
			unit = s.req.unit
			tUnit = fmt.Sprintf("%v", unit)[1:] // stores one of s, ms, μs, ns
			hist = s.req.histogram
		} else {
			dir = "Responses: "
			unit = s.resp.unit
			tUnit = fmt.Sprintf("%v", unit)[1:] // stores one of s, ms, μs, ns
			hist = s.resp.histogram
		}
		// This prints the latencies and per-op stats.
		b.WriteString(dir)
		b.WriteString(fmt.Sprintf("50_Latency: %s%s\t", strconv.FormatFloat(float64(l.Fiftieth)/float64(unit), 'f', 4, 64), tUnit))
		b.WriteString(fmt.Sprintf("90_Latency: %s%s\t", strconv.FormatFloat(float64(l.Ninetieth)/float64(unit), 'f', 4, 64), tUnit))
		b.WriteString(fmt.Sprintf("99_Latency: %s%s\t", strconv.FormatFloat(float64(l.NinetyNinth)/float64(unit), 'f', 4, 64), tUnit))
		b.WriteString(fmt.Sprintf("Avg_Latency: %s%s\t", strconv.FormatFloat(float64(l.Average)/float64(unit), 'f', 4, 64), tUnit))
		b.WriteString(fmt.Sprintf("%v Bytes/op\t", result.Data.AllocedBytes))
		b.WriteString(fmt.Sprintf("%v Allocs/op\t\n", result.Data.Allocs))

		// This prints the histogram stats for the latency.
		if hist == nil {
			b.WriteString("Histogram (empty)\n")
		} else {
			b.WriteString(fmt.Sprintf("Histogram (unit: %s)\n", tUnit))
			hist.PrintWithUnit(&b, float64(unit))
		}
	}

	// Print throughput data.
	req := result.Data.SendOps
	if req == 0 {
		req = result.Data.TotalOps
	}
	resp := result.Data.RecvOps
	if resp == 0 {
		resp = result.Data.TotalOps
	}
	b.WriteString(fmt.Sprintf("Number of requests:  %v\tRequest throughput:  %v bit/s\n", req, result.Data.ReqT))
	b.WriteString(fmt.Sprintf("Number of responses: %v\tResponse throughput: %v bit/s\n", resp, result.Data.RespT))
	fmt.Println(b.String())
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
