/*
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

package orca

// Global singleton out-of-band metric recorder, initialized in
// EnableOutOfBandMetricsReporting.
var oobRecorder *OutOfBandMetricRecorder

// GetOutOfBandMetricRecorder returns the global metrics recorder
// [OutOfBandMetricRecorder] used for reporting out-of-band custom metrics.
//
// If out-of-band metrics reporting was not initialized, via a call to
// EnableOutOfBandMetricsReporting, this function will return nil.
func GetOutOfBandMetricRecorder() *OutOfBandMetricRecorder {
	return oobRecorder
}

// OutOfBandMetricRecorder supports injection of out-of-band custom backend
// metrics from the server application.
//
// There exists a single global instance of this type, initialized when
// EnableOutOfBandMetricsReporting is called.  Server applications can retrieve
// a reference to this singleton using GetOutOfBandMetricRecorder().
//
// Safe for concurrent use.
type OutOfBandMetricRecorder struct {
	recorder *metricRecorder
}

// SetUtilizationMetric records a measurement for a utilization metric uniquely
// identifiable by name.
func (o *OutOfBandMetricRecorder) SetUtilizationMetric(name string, val float64) {
	o.recorder.setUtilization(name, val)
}

// DeleteUtilizationMetric deletes any previously recorded measurement for a
// utilization metric uniquely identifiable by name.
func (o *OutOfBandMetricRecorder) DeleteUtilizationMetric(name string) {
	o.recorder.deleteUtilization(name)
}

// SetAllUtilizationMetrics records a measurement for a bunch of utilization
// metrics specified by the provided map, where keys correspond to metric names
// and values to measurements.
func (o *OutOfBandMetricRecorder) SetAllUtilizationMetrics(pairs map[string]float64) {
	o.recorder.setAllUtilization(pairs)
}

// SetCPUUtilizationMetric records a measurement for the CPU utilization metric.
func (o *OutOfBandMetricRecorder) SetCPUUtilizationMetric(val float64) {
	o.recorder.setCPU(val)
}

// DeleteCPUUtilizationMetric deletes a previously recorded measurement for the
// CPU utilization metric.
func (o *OutOfBandMetricRecorder) DeleteCPUUtilizationMetric() {
	o.recorder.setCPU(0)
}

// SetMemoryUtilizationMetric records a measurement for the memory utilization
// metric.
func (o *OutOfBandMetricRecorder) SetMemoryUtilizationMetric(val float64) {
	o.recorder.setMemory(val)
}

// DeleteMemoryUtilizationMetric deletes a previously recorded measurement for
// the memory utilization metric.
func (o *OutOfBandMetricRecorder) DeleteMemoryUtilizationMetric() {
	o.recorder.setMemory(0)
}
