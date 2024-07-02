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

// Package instrumentregistry contains the experimental instrument registry data
// structures exposed to users.
package instrumentregistry

import "google.golang.org/grpc/stats"

// Int64CountHandle is a typed handle for a int count instrument. This handle is
// passed at the recording point in order to know which instrument to record on.
type Int64CountHandle struct {
	Index int
}

// Float64CountHandle is a typed handle for a float count instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Float64CountHandle struct {
	Index int
}

// Int64HistoHandle is a typed handle for a int histogram instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Int64HistoHandle struct {
	Index int
}

// Float64HistoHandle is a typed handle for a float histogram instrument. This
// handle is passed at the recording point in order to know which instrument to
// record on.
type Float64HistoHandle struct {
	Index int
}

// Int64GaugeHandle is a typed handle for a int gauge instrument. This handle is
// passed at the recording point in order to know which instrument to record on.
type Int64GaugeHandle struct {
	Index int
}

// DefaultMetrics are the default metrics registered through global instruments
// registry. This is written to at initialization time only, and is read
// only after initialization.
var DefaultMetrics = make(map[stats.Metric]bool)
