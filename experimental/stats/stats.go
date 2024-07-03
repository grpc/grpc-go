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

// Package stats contains experimental metrics/stats API's.
package stats


// MetricsRecorder records on metrics derived from instrument registry.
type MetricsRecorder interface {
	// RecordIntCount records the measurement alongside labels on the int count
	// associated with the provided handle.
	RecordIntCount(Int64CountHandle, []Label, []Label, int64)
	// RecordFloatCount records the measurement alongside labels on the float count
	// associated with the provided handle.
	RecordFloatCount(Float64CountHandle, []Label, []Label, float64)
	// RecordIntHisto records the measurement alongside labels on the int histo
	// associated with the provided handle.
	RecordIntHisto(Int64HistoHandle, []Label, []Label, int64)
	// RecordFloatHisto records the measurement alongside labels on the float
	// histo associated with the provided handle.
	RecordFloatHisto(Float64HistoHandle, []Label, []Label, float64)
	// RecordIntGauge records the measurement alongside labels on the int gauge
	// associated with the provided handle.
	RecordIntGauge(Int64GaugeHandle, []Label, []Label, int64)
}

// Label represents a string attribute/label to attach to metrics.
type Label struct {
	// Key is the key of the label.
	Key string
	// Value is the value of the label.
	Value string
}
