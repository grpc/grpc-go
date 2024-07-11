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

// MetricsRecorder records on metrics derived from metric registry.
type MetricsRecorder interface {
	// RecordIntCount records the measurement alongside labels on the int count
	// associated with the provided handle.
	RecordIntCount(handle *Int64CountHandle, incr int64, labels ...string)
	// RecordFloatCount records the measurement alongside labels on the float count
	// associated with the provided handle.
	RecordFloatCount(handle *Float64CountHandle, incr float64, labels ...string)
	// RecordIntHisto records the measurement alongside labels on the int histo
	// associated with the provided handle.
	RecordIntHisto(handle *Int64HistoHandle, incr int64, labels ...string)
	// RecordFloatHisto records the measurement alongside labels on the float
	// histo associated with the provided handle.
	RecordFloatHisto(handle *Float64HistoHandle, incr float64, labels ...string)
	// RecordIntGauge records the measurement alongside labels on the int gauge
	// associated with the provided handle.
	RecordIntGauge(handle *Int64GaugeHandle, incr int64, labels ...string)
}
