/*
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
 */

// Package opentelemetry is EXPERIMENTAL and may be moved to stats/opentelemetry
// package in a later release.
package opentelemetry

import (
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceOptions are the tracing options for OpenTelemetry instrumentation.
type TraceOptions struct {
	// TracerProvider is the OpenTelemetry tracer which is required to
	// record traces/trace spans for instrumentation.  If unset, tracing
	// will not be recorded.
	TracerProvider trace.TracerProvider

	// TextMapPropagator propagates span context through text map carrier.
	// If unset, context propagation will not occur, which may result in
	// loss of trace context across service boundaries.
	TextMapPropagator propagation.TextMapPropagator
}
