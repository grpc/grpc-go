/*
 *
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
 *
 */

// Package internal contains orca-internal code, for testing purposes and to
// avoid polluting the godoc of the top-level orca package.
package internal

import "time"

// MinReportingIntervalForTesting returns the absolute minimum value supported
// for out-of-band metrics reporting from the ORCA service implementation
// provided by the orca package.
//
// For testing purposes only.
var MinReportingIntervalForTesting func() time.Duration
