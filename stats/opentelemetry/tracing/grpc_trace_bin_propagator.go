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

package tracing

import (
	"context"
	"encoding/base64"

	otelpropagation "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// TODO: Move out of internal as part of open telemetry API

// GRPCTraceBinPropagator is an OpenTelemetry TextMapPropagator which is used
// to extract and inject trace context data from and into messages exchanged by
// gRPC applications. It propagates trace data in binary format using the
// 'grpc-trace-bin' header.
type GRPCTraceBinPropagator struct{}

// Inject sets OpenTelemetry trace context information from the Context into
// the carrier.
//
// If the carrier is a CustomCarrier, trace data is directly injected in a
// binary format using the 'grpc-trace-bin' header (fast path). Otherwise,
// the trace data is base64 encoded and injected using the same header in
// text format (slow path).
func (p GRPCTraceBinPropagator) Inject(ctx context.Context, carrier otelpropagation.TextMapCarrier) {
	span := oteltrace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	bd := Binary(span.SpanContext())
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
// format from the 'grpc-trace-bin' header (fast path). Otherwise, the trace
// data is base64 decoded from the same header in text format (slow path).
func (p GRPCTraceBinPropagator) Extract(ctx context.Context, carrier otelpropagation.TextMapCarrier) context.Context {
	var bd []byte

	if cc, ok := carrier.(*itracing.CustomCarrier); ok {
		bd = cc.GetBinary()
	} else {
		bd, _ = base64.StdEncoding.DecodeString(carrier.Get(itracing.GRPCTraceBinHeaderKey))
	}
	if bd == nil {
		return ctx
	}

	sc, ok := FromBinary([]byte(bd))
	if !ok {
		return ctx
	}

	return oteltrace.ContextWithRemoteSpanContext(ctx, sc)
}

// Fields returns the keys whose values are set with Inject.
//
// GRPCTraceBinPropagator always returns a slice containing only
// `grpc-trace-bin` key because it only sets the 'grpc-trace-bin' header for
// propagating trace context.
func (p GRPCTraceBinPropagator) Fields() []string {
	return []string{itracing.GRPCTraceBinHeaderKey}
}

// Binary returns the binary format representation of a SpanContext.
//
// If sc is the zero value, returns nil.
func Binary(sc oteltrace.SpanContext) []byte {
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
	b[28] = uint8(oteltrace.TraceFlags(sc.TraceFlags()))
	return b[:]
}

// FromBinary returns the SpanContext represented by b.
//
// If b has an unsupported version ID or contains no TraceID, FromBinary
// returns with zero value SpanContext and false.
func FromBinary(b []byte) (oteltrace.SpanContext, bool) {
	if len(b) == 0 || b[0] != 0 {
		return oteltrace.SpanContext{}, false
	}
	b = b[1:]
	if len(b) < 17 || b[0] != 0 {
		return oteltrace.SpanContext{}, false
	}

	sc := oteltrace.SpanContext{}
	sc = sc.WithTraceID(oteltrace.TraceID(b[1:17]))
	b = b[17:]
	if len(b) >= 9 && b[0] == 1 {
		sc = sc.WithSpanID(oteltrace.SpanID(b[1:9]))
		b = b[9:]
	}
	if len(b) >= 2 && b[0] == 2 {
		sc = sc.WithTraceFlags(oteltrace.TraceFlags(b[1]))
	}
	return sc, true
}
