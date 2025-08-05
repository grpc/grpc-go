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
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestPanic tests that registering two metrics with the same name across any
// type of metric triggers a panic.
func (s) TestPanic(t *testing.T) {
	cleanup := snapshotMetricsRegistryForTesting()
	defer cleanup()

	want := "metric simple counter already registered"
	defer func() {
		if r := recover(); !strings.Contains(fmt.Sprint(r), want) {
			t.Errorf("expected panic contains %q, got %q", want, r)
		}
	}()
	desc := MetricDescriptor{
		// Type is not expected to be set from the registerer, but meant to be
		// set by the metric registry.
		Name:        "simple counter",
		Description: "number of times recorded on tests",
		Unit:        "{call}",
	}
	RegisterInt64Count(desc)
	RegisterInt64Gauge(desc)
}

// TestInstrumentRegistry tests the metric registry. It registers testing only
// metrics using the metric registry, and creates a fake metrics recorder which
// uses these metrics. Using the handles returned from the metric registry, this
// test records stats using the fake metrics recorder. Then, the test verifies
// the persisted metrics data in the metrics recorder is what is expected. Thus,
// this tests the interactions between the metrics recorder and the metrics
// registry.
func (s) TestMetricRegistry(t *testing.T) {
	cleanup := snapshotMetricsRegistryForTesting()
	defer cleanup()

	intCountHandle1 := RegisterInt64Count(MetricDescriptor{
		Name:           "simple counter",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	floatCountHandle1 := RegisterFloat64Count(MetricDescriptor{
		Name:           "float counter",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float counter label"},
		OptionalLabels: []string{"float counter optional label"},
		Default:        false,
	})
	intHistoHandle1 := RegisterInt64Histo(MetricDescriptor{
		Name:           "int histo",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int histo label"},
		OptionalLabels: []string{"int histo optional label"},
		Default:        false,
	})
	floatHistoHandle1 := RegisterFloat64Histo(MetricDescriptor{
		Name:           "float histo",
		Description:    "sum of all emissions from tests",
		Unit:           "float",
		Labels:         []string{"float histo label"},
		OptionalLabels: []string{"float histo optional label"},
		Default:        false,
	})
	intGaugeHandle1 := RegisterInt64Gauge(MetricDescriptor{
		Name:           "simple gauge",
		Description:    "the most recent int emitted by test",
		Unit:           "int",
		Labels:         []string{"int gauge label"},
		OptionalLabels: []string{"int gauge optional label"},
		Default:        false,
	})

	fmr := newFakeMetricsRecorder(t)

	intCountHandle1.Record(fmr, 1, []string{"some label value", "some optional label value"}...)
	// The Metric Descriptor in the handle should be able to identify the metric
	// information. This is the key passed to metrics recorder to identify
	// metric.
	if got := fmr.intValues[intCountHandle1.Descriptor()]; got != 1 {
		t.Fatalf("fmr.intValues[intCountHandle1.MetricDescriptor] got %v, want: %v", got, 1)
	}

	floatCountHandle1.Record(fmr, 1.2, []string{"some label value", "some optional label value"}...)
	if got := fmr.floatValues[floatCountHandle1.Descriptor()]; got != 1.2 {
		t.Fatalf("fmr.floatValues[floatCountHandle1.MetricDescriptor] got %v, want: %v", got, 1.2)
	}

	intHistoHandle1.Record(fmr, 3, []string{"some label value", "some optional label value"}...)
	if got := fmr.intValues[intHistoHandle1.Descriptor()]; got != 3 {
		t.Fatalf("fmr.intValues[intHistoHandle1.MetricDescriptor] got %v, want: %v", got, 3)
	}

	floatHistoHandle1.Record(fmr, 4.3, []string{"some label value", "some optional label value"}...)
	if got := fmr.floatValues[floatHistoHandle1.Descriptor()]; got != 4.3 {
		t.Fatalf("fmr.floatValues[floatHistoHandle1.MetricDescriptor] got %v, want: %v", got, 4.3)
	}

	intGaugeHandle1.Record(fmr, 7, []string{"some label value", "some optional label value"}...)
	if got := fmr.intValues[intGaugeHandle1.Descriptor()]; got != 7 {
		t.Fatalf("fmr.intValues[intGaugeHandle1.MetricDescriptor] got %v, want: %v", got, 7)
	}
}

// TestNumerousIntCounts tests numerous int count metrics registered onto the
// metric registry. A component (simulated by test) should be able to record on
// the different registered int count metrics.
func TestNumerousIntCounts(t *testing.T) {
	cleanup := snapshotMetricsRegistryForTesting()
	defer cleanup()

	intCountHandle1 := RegisterInt64Count(MetricDescriptor{
		Name:           "int counter",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	intCountHandle2 := RegisterInt64Count(MetricDescriptor{
		Name:           "int counter 2",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})
	intCountHandle3 := RegisterInt64Count(MetricDescriptor{
		Name:           "int counter 3",
		Description:    "sum of all emissions from tests",
		Unit:           "int",
		Labels:         []string{"int counter label"},
		OptionalLabels: []string{"int counter optional label"},
		Default:        false,
	})

	fmr := newFakeMetricsRecorder(t)

	intCountHandle1.Record(fmr, 1, []string{"some label value", "some optional label value"}...)
	got := []int64{fmr.intValues[intCountHandle1.Descriptor()], fmr.intValues[intCountHandle2.Descriptor()], fmr.intValues[intCountHandle3.Descriptor()]}
	want := []int64{1, 0, 0}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("fmr.intValues (-got, +want): %v", diff)
	}

	intCountHandle2.Record(fmr, 1, []string{"some label value", "some optional label value"}...)
	got = []int64{fmr.intValues[intCountHandle1.Descriptor()], fmr.intValues[intCountHandle2.Descriptor()], fmr.intValues[intCountHandle3.Descriptor()]}
	want = []int64{1, 1, 0}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("fmr.intValues (-got, +want): %v", diff)
	}

	intCountHandle3.Record(fmr, 1, []string{"some label value", "some optional label value"}...)
	got = []int64{fmr.intValues[intCountHandle1.Descriptor()], fmr.intValues[intCountHandle2.Descriptor()], fmr.intValues[intCountHandle3.Descriptor()]}
	want = []int64{1, 1, 1}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("fmr.intValues (-got, +want): %v", diff)
	}

	intCountHandle3.Record(fmr, 1, []string{"some label value", "some optional label value"}...)
	got = []int64{fmr.intValues[intCountHandle1.Descriptor()], fmr.intValues[intCountHandle2.Descriptor()], fmr.intValues[intCountHandle3.Descriptor()]}
	want = []int64{1, 1, 2}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("fmr.intValues (-got, +want): %v", diff)
	}
}

type fakeMetricsRecorder struct {
	t *testing.T

	intValues   map[*MetricDescriptor]int64
	floatValues map[*MetricDescriptor]float64
}

// newFakeMetricsRecorder returns a fake metrics recorder based off the current
// state of global metric registry.
func newFakeMetricsRecorder(t *testing.T) *fakeMetricsRecorder {
	fmr := &fakeMetricsRecorder{
		t:           t,
		intValues:   make(map[*MetricDescriptor]int64),
		floatValues: make(map[*MetricDescriptor]float64),
	}

	for _, desc := range metricsRegistry {
		switch desc.Type {
		case MetricTypeIntCount:
		case MetricTypeIntHisto:
		case MetricTypeIntGauge:
			fmr.intValues[desc] = 0
		case MetricTypeFloatCount:
		case MetricTypeFloatHisto:
			fmr.floatValues[desc] = 0
		}
	}
	return fmr
}

// verifyLabels verifies that the labels received are of the expected length.
func verifyLabels(t *testing.T, labelsWant []string, optionalLabelsWant []string, labelsGot []string) {
	if len(labelsWant)+len(optionalLabelsWant) != len(labelsGot) {
		t.Fatalf("length of optional labels expected did not match got %v, want %v", len(labelsGot), len(labelsWant)+len(optionalLabelsWant))
	}
}

func (r *fakeMetricsRecorder) RecordInt64Count(handle *Int64CountHandle, incr int64, labels ...string) {
	verifyLabels(r.t, handle.Descriptor().Labels, handle.Descriptor().OptionalLabels, labels)
	r.intValues[handle.Descriptor()] += incr
}

func (r *fakeMetricsRecorder) RecordFloat64Count(handle *Float64CountHandle, incr float64, labels ...string) {
	verifyLabels(r.t, handle.Descriptor().Labels, handle.Descriptor().OptionalLabels, labels)
	r.floatValues[handle.Descriptor()] += incr
}

func (r *fakeMetricsRecorder) RecordInt64Histo(handle *Int64HistoHandle, incr int64, labels ...string) {
	verifyLabels(r.t, handle.Descriptor().Labels, handle.Descriptor().OptionalLabels, labels)
	r.intValues[handle.Descriptor()] += incr
}

func (r *fakeMetricsRecorder) RecordFloat64Histo(handle *Float64HistoHandle, incr float64, labels ...string) {
	verifyLabels(r.t, handle.Descriptor().Labels, handle.Descriptor().OptionalLabels, labels)
	r.floatValues[handle.Descriptor()] += incr
}

func (r *fakeMetricsRecorder) RecordInt64Gauge(handle *Int64GaugeHandle, incr int64, labels ...string) {
	verifyLabels(r.t, handle.Descriptor().Labels, handle.Descriptor().OptionalLabels, labels)
	r.intValues[handle.Descriptor()] += incr
}
