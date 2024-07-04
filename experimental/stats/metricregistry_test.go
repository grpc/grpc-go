/*
 *
 * Copyright 2024 gRPC authors.
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
	"google.golang.org/grpc/experimental/stats/metricregistry"
	"testing"
)

func (s) TestMetricRegistry(t *testing.T) {

	// Now there's an implied metrics recorder as part of record calls...

	// Component from client conn will satisfy interface...

	// Same thing create instruments, pass a metrics recorder, instrument is expected to call that metrics recorder

	// Register one of each instrument, verify the metrics recorder works with it...

}

type fakeMetricsRecorder struct {
	t *testing.T

	// 5 different or for one for ints/floats...?

	// Test the maps built out in OTel (mention to Doug this represents it...)
	intValues map[*MetricDescriptor]int64
	floatValues map[*MetricDescriptor]float64
}

// MetricsRecorderList layer just looks at labels/optional labels...

// newFakeMetricsRecorder returns a fake metrics recorder based off the current
// state of global instrument registry.
func newFakeMetricsRecorder(t *testing.T) *fakeMetricsRecorder {
	// Access globals, build a map like OTel would
	MetricsRegistry // map[stats.Metric]->Pointer to metrics descriptor, yeah let's test this out, make sure pointer can be used as key value...
}

// verifyLabels verifies that all of the labels keys expected are present in the
// labels received.
func verifyLabels(t *testing.T, labelsWant []string, optionalLabelsWant []string, labelsGot []stats.Label, optionalLabelsGot []stats.Label) {
	for i, label := range labelsWant {
		if labelsGot[i].Key != label {
			t.Fatalf("label key at position %v got %v, want %v", i, labelsGot[i].Key, label)
		}
	}
	if len(labelsWant) != len(labelsGot) {
		t.Fatalf("length of labels expected did not match got %v, want %v", len(labelsGot), len(optionalLabelsWant))
	}

	for i, label := range optionalLabelsWant {
		if optionalLabelsGot[i].Key != label {
			t.Fatalf("optional label key at position %v got %v, want %v", i, optionalLabelsGot[i].Key, label)
		}
	}
	if len(optionalLabelsWant) != len(optionalLabelsGot) {
		t.Fatalf("length of optional labels expected did not match got %v, want %v", len(optionalLabelsGot), len(optionalLabelsWant))
	}

	// This is essentially now a check of len(labels + optional labels) vs labels provided...

}

// Test 2 for each? 5 different maps...?

// All the operations will get a handle with pointer above, make sure it can use to record...

// Need a clear for testing...

// It still implements these methods but gets called from handle
func (r *fakeMetricsRecorder) RecordIntCount(handle Int64CountHandle, labels []Label, optionalLabels []Label, incr int64) { // Techncialy this owuld eat labels...verifyLabels too?
	// Rather than reading from registry/building out data structures, labels come from handle
	handle.MetricDescriptor.Labels // []string also makes sure not nil...MetricDescriptor

	handle.MetricDescriptor.OptionalLabels // []string

	verifyLabels(r.t, handle.MetricDescriptor.Labels, handle.MetricDescriptor.OptionalLabels, labels, optionalLabels)

	// Overall data structure of the stats handler...
	// map[name]->local would create a string comp

	// map[*MetricDescriptor]->local would just be a pointer comp...

	// record incr against data structure built out maybe map[name]->
	// No it's a map of metricdescriptor...
	// How to build this out?
	r.intValues[handle.MetricDescriptor] += incr // have the handle in main use that to verify...

}

func (r *fakeMetricsRecorder) RecordFloatCount(handle Float64CountHandle, labels []Label, optionalLabels []Label, incr float64) {
	verifyLabels(r.t, handle.MetricDescriptor.Labels, handle.MetricDescriptor.OptionalLabels, labels, optionalLabels)

	handle.MetricDescriptor // *MetricDescriptor - use as key to map if not found then fatalf

	r.floatValues[handle.MetricDescriptor] += incr
}

func (r *fakeMetricsRecorder) RecordIntHisto(handle Int64HistoHandle, labels []Label, optionalLabels []Label, incr int64) {
	verifyLabels(r.t, handle.MetricDescriptor.Labels, handle.MetricDescriptor.OptionalLabels, labels, optionalLabels)

	handle.MetricDescriptor // *MetricDescriptor - use as key to map if not found then fatalf

	r.intValues[handle.MetricDescriptor] += incr // after 5 of these, makes sure they don't collide
}

func (r *fakeMetricsRecorder) RecordFloatHisto(handle Float64HistoHandle, labels []Label, optionalLabels []Label, incr float64) {
	verifyLabels(r.t, handle.MetricDescriptor.Labels, handle.MetricDescriptor.OptionalLabels, labels, optionalLabels)

	handle.MetricDescriptor // *MetricDescriptor - use as key to map if not found then fatalf
}

func (r *fakeMetricsRecorder) RecordIntGauge(handle Int64GaugeHandle, labels []Label, optionalLabels []Label, incr int64) {
	verifyLabels(r.t, handle.MetricDescriptor.Labels, handle.MetricDescriptor.OptionalLabels, labels, optionalLabels)

	handle.MetricDescriptor // *MetricDescriptor - use as key to map if not found then fatalf
}

// If run out of time just push implementation...otel and metrics recorder list still come after I guess...
// just push the extra file....


// Tests sound good to Doug get this plumbing working...

// switch the labels to be variadic args based on position, length check on labels + optional labels

// optional labels are always plumbed up through otel, otel decides whether it
// wants the optional labels or not...

// on handle and metrics recorder


