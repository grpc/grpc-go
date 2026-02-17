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

// Package telemetry defines stats APIs for interacting with
// telemetry labels.
//
// All APIs in this package are experimental.
package telemetry

import (
	"context"

	stats "google.golang.org/grpc/internal/stats"
)

// WithTelemetryLabelCallback registers a callback function that is executed whenever
// telemetry labels will be updated. This does _not_ require opentelemetry instrumentation
// to be configured on the client or server.
//
// Callbacks are appended to the context when registered and do not modify the original context.
//
// WARNING: Callbacks are executed on the RPC path so users should be mindful of the
// potential performance impact when this is eventually executed.
func WithTelemetryLabelCallback(ctx context.Context, callback func(map[string]string)) context.Context {
	return stats.RegisterTelemetryLabelCallback(ctx, callback)
}
