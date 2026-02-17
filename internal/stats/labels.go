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
	"maps"

	"google.golang.org/grpc/grpclog"
)

// LabelCallback is a function that is executed when telemetry
// label keys are updated
type LabelCallback func(map[string]string)

// Labels are the labels for metrics.
type Labels struct {
	// TelemetryLabels are the telemetry labels to record.
	TelemetryLabels map[string]string
}

type labelsKey struct{}
type telemetryLabelCallbackKey struct{}

// GetLabels returns the Labels stored in the context, or nil if there is one.
func GetLabels(ctx context.Context) *Labels {
	labels, _ := ctx.Value(labelsKey{}).(*Labels)
	return labels
}

// SetLabels sets the Labels in the context.
func SetLabels(ctx context.Context, labels *Labels) context.Context {
	// could also append
	return context.WithValue(ctx, labelsKey{}, labels)
}

// UpdateLabels copies the key-values from update into the existing context labels if they
// have been set. When a key is already present in the context labels, it is
// overwritten.
func UpdateLabels(ctx context.Context, update map[string]string) {
	if ctx == nil {
		return
	}

	labels := GetLabels(ctx)
	if labels != nil {
		maps.Copy(labels.TelemetryLabels, update)
	}

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
