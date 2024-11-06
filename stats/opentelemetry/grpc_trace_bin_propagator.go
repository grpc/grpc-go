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
	"encoding/base64"

	otelpropagation "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// GRPCTraceBinPropagator is an OpenTelemetry TextMapPropagator which is used
// to extract and inject trace context data from and into headers exchanged by
// gRPC applications. It propagates trace data in binary format using the
// `grpc-trace-bin` header.
type GRPCTraceBinPropagator struct{}

// Inject sets OpenTelemetry trace context information from the Context into
// the carrier.
//
// If the carrier is a CustomCarrier, trace data is directly injected in a
// binary format using the `grpc-trace-bin` header (fast path). Otherwise,
// the trace data is base64 encoded and injected using the same header in
// text format (slow path). If span context is not valid or emptu, no data is
// injected.
func (GRPCTraceBinPropagator) Inject(ctx context.Context, carrier otelpropagation.TextMapCarrier) {
	span := oteltrace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	bd := binary(span.SpanContext())
	if bd == nil {
		return
	}

	if cc, ok := carrier.(*itracing.CustomCarrier); ok {
		cc.SetBinary(bd)
		return
	}
	carrier.Set(itracing.GRPCTraceBinHeaderKey, base64.StdEncoding.EncodeToString(bd))
}

// Extract reads OpenTelemetry trace context information from the carrier into a
// Context.
//
// If the carrier is a CustomCarrier, trace data is read directly in a binary
// format from the `grpc-trace-bin` header (fast path). Otherwise, the trace
// data is base64 decoded from the same header in text format (slow path).
//
// If a valid trace context is found, this function returns a new context
// derived from the input `ctx` containing the extracted span context. The
// extracted span context is marked as "remote", indicating that the trace
// originated from a different process or service. If trace context is invalid
// or not present, input `ctx` is returned as is.
func (GRPCTraceBinPropagator) Extract(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	var bd []byte
	if cc, ok := carrier.(*itracing.CustomCarrier); ok {
		bd = cc.GetBinary()
	} else {
		bd, _ = base64.StdEncoding.DecodeString(carrier.Get(itracing.GRPCTraceBinHeaderKey))
	}
	if bd == nil {
		return ctx
	}

	sc, ok := fromBinary([]byte(bd))
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
	return []string{itracing.GRPCTraceBinHeaderKey}
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
