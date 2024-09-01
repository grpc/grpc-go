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

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const GrpcTraceBinHeaderKey = "grpc-trace-bin"

// GrpcTraceBinPropagator is TextMapPropagator to propagate cross-cutting
// concerns as both text and binary key-value pairs within a carrier that
// travels in-band across process boundaries.
type GrpcTraceBinPropagator struct{}

// Inject set cross-cutting concerns from the Context into the carrier.
//
// If carrier is carrier.CustomMapCarrier then SetBinary (fast path) is used,
// otherwise Set (slow path) with encoding is used.
func (p GrpcTraceBinPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}

	binaryData := Binary(span.SpanContext())
	if binaryData == nil {
		return
	}

	if customCarrier, ok := carrier.(CustomMapCarrier); ok {
		customCarrier.SetBinary(GrpcTraceBinHeaderKey, binaryData) // fast path: set the binary data without encoding
	} else {
		carrier.Set(GrpcTraceBinHeaderKey, base64.StdEncoding.EncodeToString(binaryData)) // slow path: set the binary data with encoding
	}
}

// Extract reads cross-cutting concerns from the carrier into a Context.
//
// If carrier is carrier.CustomMapCarrier then GetBinary (fast path) is used,
// otherwise Get (slow path) with decoding is used.
func (p GrpcTraceBinPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	var binaryData []byte

	if customCarrier, ok := carrier.(CustomMapCarrier); ok {
		binaryData, _ = customCarrier.GetBinary(GrpcTraceBinHeaderKey)
	} else {
		binaryData, _ = base64.StdEncoding.DecodeString(carrier.Get(GrpcTraceBinHeaderKey))
	}
	if binaryData == nil {
		return ctx
	}

	spanContext, ok := FromBinary([]byte(binaryData))
	if !ok {
		return ctx
	}

	return trace.ContextWithRemoteSpanContext(ctx, spanContext)
}

// Fields returns the keys whose values are set with Inject.
//
// GrpcTraceBinPropagator will only have `grpc-trace-bin` field.
func (p GrpcTraceBinPropagator) Fields() []string {
	return []string{GrpcTraceBinHeaderKey}
}

// Binary returns the binary format representation of a SpanContext.
//
// If sc is the zero value, Binary returns nil.
func Binary(sc trace.SpanContext) []byte {
	if sc.Equal(trace.SpanContext{}) {
		return nil
	}
	var b [29]byte
	traceID := trace.TraceID(sc.TraceID())
	copy(b[2:18], traceID[:])
	b[18] = 1
	spanID := trace.SpanID(sc.SpanID())
	copy(b[19:27], spanID[:])
	b[27] = 2
	b[28] = uint8(trace.TraceFlags(sc.TraceFlags()))
	return b[:]
}

// FromBinary returns the SpanContext represented by b.
//
// If b has an unsupported version ID or contains no TraceID, FromBinary
// returns with ok==false.
func FromBinary(b []byte) (sc trace.SpanContext, ok bool) {
	if len(b) == 0 || b[0] != 0 {
		return trace.SpanContext{}, false
	}
	b = b[1:]

	if len(b) >= 17 && b[0] == 0 {
		sc = sc.WithTraceID(trace.TraceID(b[1:17]))
		b = b[17:]
	} else {
		return trace.SpanContext{}, false
	}
	if len(b) >= 9 && b[0] == 1 {
		sc = sc.WithSpanID(trace.SpanID(b[1:9]))
		b = b[9:]
	}
	if len(b) >= 2 && b[0] == 2 {
		sc = sc.WithTraceFlags(trace.TraceFlags(b[1]))
	}
	return sc, true
}
