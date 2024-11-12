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

package opentelemetry

import (
	"context"

	otelpropagation "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
)

// GRPCTraceBinHeaderKey is the gRPC metadata header key `grpc-trace-bin` used
// to propagate trace context in binary format.
const GRPCTraceBinHeaderKey = "grpc-trace-bin"

// GRPCTraceBinPropagator is an OpenTelemetry TextMapPropagator which is used
// to extract and inject trace context data from and into headers exchanged by
// gRPC applications. It propagates trace data in binary format using the
// `grpc-trace-bin` header.
type GRPCTraceBinPropagator struct{}

// Inject sets OpenTelemetry trace context information from the Context into
// the carrier.
//
// It first attempts to retrieve any existing binary trace data from the
// provided context using `stats.Trace()`. If found, it means that a trace data
// was injected by a system using gRPC OpenCensus plugin. Hence, we inject this
// trace data into the carrier, allowing trace from system using gRPC
// OpenCensus plugin to propagate downstream. However, we set the value in
// string format against `grpc-trace-bin` key so that downstream systems which
// are using gRPC OpenTelemetry plugin are able to extract it using
// `GRPCTraceBinPropagator`.
//
// It then attempts to retrieve an OpenTelemetry span context from the provided
// context. If not found, that means either there is no trace context or previous
// system had injected it using gRPC OpenCensus plugin. Therefore, it returns
// early without doing anymore modification to carrier. If found, that means
// previous system had injected using OpenTelemetry plugin so it converts the
// span context to binary and set in carrier in string format against
// `grpc-trace-bin` key.
func (GRPCTraceBinPropagator) Inject(ctx context.Context, carrier otelpropagation.TextMapCarrier) {
	bd := stats.Trace(ctx)
	if bd != nil {
		carrier.Set(GRPCTraceBinHeaderKey, string(bd))
	}

	sc := oteltrace.SpanFromContext(ctx)
	if !sc.SpanContext().IsValid() {
		return
	}

	bd = binary(sc.SpanContext())
	carrier.Set(GRPCTraceBinHeaderKey, string(bd))
}

// Extract reads OpenTelemetry trace context information from the carrier into a
// Context.
//
// It first attempts to read `grpc-trace-bin` header value from carrier. If
// found, that means the trace data was injected using gRPC OpenTelemetry
// plugin using `GRPCTraceBinPropagator`. It then set the trace data into
// context using `stats.SetTrace` for downstream systems still using gRPC
// OpenCensus plugin to be able to use this context.
//
// It then also extracts the OpenTelemetry span context from binary header
// value. If span context is not valid, it just returns the context as parent.
// If span context is valid, it creates a new context containing the extracted
// OpenTelemetry span context marked as remote so that downstream systems using
// OpenTelemetry are able use this context.
func (GRPCTraceBinPropagator) Extract(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	h := carrier.Get(GRPCTraceBinHeaderKey)
	if h != "" {
		ctx = stats.SetTrace(ctx, []byte(h))
	}

	sc, ok := fromBinary([]byte(h))
	if !ok {
		return ctx
	}
	return oteltrace.ContextWithRemoteSpanContext(ctx, sc)
}

// Fields returns the keys whose values are set with Inject.
//
// GRPCTraceBinPropagator always returns a slice containing only
// `grpc-trace-bin` key because it only sets the `grpc-trace-bin` header for
// propagating trace context.
func (GRPCTraceBinPropagator) Fields() []string {
	return []string{GRPCTraceBinHeaderKey}
}

// Binary returns the binary format representation of a SpanContext.
//
// If sc is the zero value, returns nil.
func binary(sc oteltrace.SpanContext) []byte {
	if sc.Equal(oteltrace.SpanContext{}) {
		return nil
	}
	var b [29]byte
	traceID := oteltrace.TraceID(sc.TraceID())
	copy(b[2:18], traceID[:])
	b[18] = 1
	spanID := oteltrace.SpanID(sc.SpanID())
	copy(b[19:27], spanID[:])
	b[27] = 2
	b[28] = byte(oteltrace.TraceFlags(sc.TraceFlags()))
	return b[:]
}

// FromBinary returns the SpanContext represented by b with Remote set to true.
//
// It returns with zero value SpanContext and false, if any of the
// below condition is not satisfied:
// - Valid header: len(b) = 29
// - Valid version: b[0] = 0
// - Valid traceID prefixed with 0: b[1] = 0
// - Valid spanID prefixed with 1: b[18] = 1
// - Valid traceFlags prefixed with 2: b[27] = 2
func fromBinary(b []byte) (oteltrace.SpanContext, bool) {
	if len(b) != 29 || b[0] != 0 || b[1] != 0 || b[18] != 1 || b[27] != 2 {
		return oteltrace.SpanContext{}, false
	}

	return oteltrace.SpanContext{}.WithTraceID(
		oteltrace.TraceID(b[2:18])).WithSpanID(
		oteltrace.SpanID(b[19:27])).WithTraceFlags(
		oteltrace.TraceFlags(b[28])).WithRemote(true), true
}
