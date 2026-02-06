/*
 *
 * Copyright 2026 gRPC authors.
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
	"context"

	"google.golang.org/grpc/grpclog"
)

// LabelCallback is a function that is executed when telemetry
// label keys are updated
type LabelCallback func(map[string]string)

type telemetryLabelCallbackKey struct{}

// WithTelemetryLabelCallback registers a callback function that is executed whenever
// telemetry labels will be updated. This does _not_ require opentelemetry instrumentation
// to be configured on the client or server.
//
// WARNING: The callback is executed on the RPC path so users should be mindful of the
// potential performance impact when this is eventually executed.
func WithTelemetryLabelCallback(ctx context.Context, callback LabelCallback) context.Context {
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, telemetryLabelCallbackKey{}, callback)
}

// ExecuteTelemetryLabelCallback runs the registered callback on the context with the provided
// key and value. If no callback is registered it does nothing.
//
// If the registered callback panics it will be swallowed and logged
func ExecuteTelemetryLabelCallback(ctx context.Context, labels map[string]string) {
	if ctx == nil {
		return
	}
	if callback, ok := ctx.Value(telemetryLabelCallbackKey{}).(LabelCallback); ok {
		defer func() {
			if r := recover(); r != nil {
				grpclog.Component("experimental-stats").Warningf("LabelCallback panicked: %v", r)
			}
		}()
		callback(labels)
	}
}
