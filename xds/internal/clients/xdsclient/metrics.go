/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

// Metric is type of metric to be reported by XDSClient.
type Metric interface {
	Target() string
}

// MetricResourceUpdateValid is a Metric to report valid resource updates from
// the xDS management server for a given resource type.
type MetricResourceUpdateValid struct {
	ServerURI    string // ServerURI of the xDS management server.
	Incr         int64  // Count to be incremented.
	ResourceType string // Resource type.

	target string
}

// Target returns the target of the metric.
func (m MetricResourceUpdateValid) Target() string {
	return m.target
}

// MetricResourceUpdateInvalid is a Metric to report invalid resource updates
// from the xDS management server for a given resource type.
type MetricResourceUpdateInvalid struct {
	ServerURI    string // ServerURI of the xDS management server.
	Incr         int64  // Count to be incremented.
	ResourceType string // Resource type.

	target string
}

// Target returns the target of the metric.
func (m MetricResourceUpdateInvalid) Target() string {
	return m.target
}

// MetricServerFailure is a Metric to report server failures of the xDS
// management server.
type MetricServerFailure struct {
	ServerURI string // ServerURI of the xDS management server.
	Incr      int64  // Count to be incremented.

	target string
}

// Target returns the target of the metric.
func (m MetricServerFailure) Target() string {
	return m.target
}

// MetricsReporter is used by the XDSClient to report metrics.
type MetricsReporter interface {
	// ReportMetric reports a metric.
	ReportMetric(Metric)
}
