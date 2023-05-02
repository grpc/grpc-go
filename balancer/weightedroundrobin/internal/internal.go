/*
 *
 * Copyright 2023 gRPC authors.
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

// Package internal allows for easier testing of the weightedroundrobin
// package.
package internal

import (
	"time"
)

// AllowAnyWeightUpdatePeriod permits any setting of WeightUpdatePeriod for
// testing.  Normally a minimum of 100ms is applied.
var AllowAnyWeightUpdatePeriod bool

// LBConfig allows tests to produce a JSON form of the config from the struct
// instead of using a string.
type LBConfig struct {
	EnableOOBLoadReport     *bool          `json:"enableOobLoadReport,omitempty"`
	OOBReportingPeriod      *time.Duration `json:"oobReportingPeriod,omitempty"`
	BlackoutPeriod          *time.Duration `json:"blackoutPeriod,omitempty"`
	WeightExpirationPeriod  *time.Duration `json:"weightExpirationPeriod,omitempty"`
	WeightUpdatePeriod      *time.Duration `json:"weightUpdatePeriod,omitempty"`
	ErrorUtilizationPenalty *float64       `json:"errorUtilizationPenalty,omitempty"`
}

// TimeNow can be overridden by tests to return a different value for the
// current time.
var TimeNow = time.Now
