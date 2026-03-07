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

// Package stats provides internal stats related functionality.
package stats

import (
	"context"

	"google.golang.org/grpc/grpclog"
)

// LabelCallback is a function that is executed when telemetry
// label keys are updated
type LabelCallback func(map[string]string)
type telemetryLabelCallbackKey struct{}

// UpdateLabels executes registered telemetry callbacks with the update labels.
//
// It is the responsibility of the registrant to handle conflicts or label resets.
func UpdateLabels(ctx context.Context, update map[string]string) {
	executeTelemetryLabelCallbacks(ctx, update)
}

// RegisterTelemetryLabelCallback registers a callback function that is executed whenever
// telemetry labels will be updated.
func RegisterTelemetryLabelCallback(ctx context.Context, callback LabelCallback) context.Context {
	if callback == nil {
		return ctx
	}

	if callbacks, ok := ctx.Value(telemetryLabelCallbackKey{}).([]LabelCallback); ok {
		return context.WithValue(ctx, telemetryLabelCallbackKey{}, append(callbacks, callback))
	}
	return context.WithValue(ctx, telemetryLabelCallbackKey{}, []LabelCallback{callback})
}

// executeTelemetryLabelCallback runs the registered callbacks in the order they were
// registered on the context with the provided labels. If no callbacks are registered
// it does nothing. If any registered callback panics it will be swallowed and logged and
// continue running any other registered callbacks.
func executeTelemetryLabelCallbacks(ctx context.Context, labels map[string]string) {
	if ctx == nil {
		return
	}
	if callbacks, ok := ctx.Value(telemetryLabelCallbackKey{}).([]LabelCallback); ok {
		for _, callback := range callbacks {
			runWithRecovery(callback, labels)
		}

	}
}

// runWithRecovery ensures that callbacks that panic don't prevent other callback
// functions from executing.
func runWithRecovery(callback LabelCallback, labels map[string]string) {
	defer func() {
		if r := recover(); r != nil {
			grpclog.Component("stats").Warningf("LabelCallback panicked: %v", r)
		}
	}()
	callback(labels)
}
