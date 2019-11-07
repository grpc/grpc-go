/*
 *
 * Copyright 2019 gRPC authors.
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

package profiling

import (
	internal "google.golang.org/grpc/internal/profiling"
)

// SetEnabled turns profiling on and off. This operation is safe for access
// concurrently from different goroutines.
//
// Note that this is the only operation that's accessible through a publicly
// exposed profiling package. Everything else (such as retrieving stats) must
// be done throught the profiling service. This is allowed programmatic access
// in case your application has heuristics to turn profiling on and off.
func SetEnabled(enabled bool) {
	internal.SetEnabled(enabled)
}
