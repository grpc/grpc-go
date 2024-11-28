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
	"testing"

	"github.com/google/go-cmp/cmp"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

var validSpanContext = oteltrace.SpanContext{}.WithTraceID(
	oteltrace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}).WithSpanID(
	oteltrace.SpanID{17, 18, 19, 20, 21, 22, 23, 24}).WithTraceFlags(
	oteltrace.TraceFlags(1))

// TestInject_ValidSpanContext verifies that the GRPCTraceBinPropagator
// correctly injects a valid OpenTelemetry span context as `grpc-trace-bin`
// header in the provided carrier's context metadata.
//
// It verifies that if a valid span context is injected, same span context can
// can be retreived from the carrier's context metadata.
func (s) TestInject_ValidSpanContext(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := itracing.NewOutgoingCarrier(ctx)
	ctx = oteltrace.ContextWithSpanContext(ctx, validSpanContext)

	p.Inject(ctx, c)

	md, _ := metadata.FromOutgoingContext(c.Context())
	gotH := md.Get(grpcTraceBinHeaderKey)
	if gotH[len(gotH)-1] == "" {
		t.Fatalf("got empty value from Carrier's context metadata grpc-trace-bin header, want valid span context: %v", validSpanContext)
	}
	gotSC, ok := fromBinary([]byte(gotH[len(gotH)-1]))
	if !ok {
		t.Fatalf("got invalid span context %v from Carrier's context metadata grpc-trace-bin header, want valid span context: %v", gotSC, validSpanContext)
	}
	if cmp.Equal(validSpanContext, gotSC) {
		t.Fatalf("got span context = %v, want span contexts %v", gotSC, validSpanContext)
	}
}

// TestInject_InvalidSpanContext verifies that the GRPCTraceBinPropagator does
// not inject an invalid OpenTelemetry span context as `grpc-trace-bin` header
// in the provided carrier's context metadata.
//
// If an invalid span context is injected, it verifies that `grpc-trace-bin`
// header is not set in the carrier's context metadata.
func (s) TestInject_InvalidSpanContext(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := itracing.NewOutgoingCarrier(ctx)
	ctx = oteltrace.ContextWithSpanContext(ctx, oteltrace.SpanContext{})

	p.Inject(ctx, c)

	md, _ := metadata.FromOutgoingContext(c.Context())
	if gotH := md.Get(grpcTraceBinHeaderKey); len(gotH) > 0 {
		t.Fatalf("got %v value from Carrier's context metadata grpc-trace-bin header, want empty", gotH)
	}
}

// TestExtract verifies that the GRPCTraceBinPropagator correctly extracts
// OpenTelemetry span context data from the provided context using carrier.
//
// If a valid span context was injected, it verifies same trace span context
// is extracted from carrier's metadata for `grpc-trace-bin` header key.
//
// If invalid span context was injected, it verifies that valid trace span
// context is not extracted.
func (s) TestExtract(t *testing.T) {
	tests := []struct {
		name   string
		wantSC oteltrace.SpanContext // expected span context from carrier
	}{
		{
			name:   "valid OpenTelemetry span context",
			wantSC: validSpanContext.WithRemote(true),
		},
		{
			name:   "invalid OpenTelemetry span context",
			wantSC: oteltrace.SpanContext{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := GRPCTraceBinPropagator{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx = metadata.NewIncomingContext(ctx, metadata.MD{grpcTraceBinHeaderKey: []string{string(toBinary(test.wantSC))}})

			c := itracing.NewIncomingCarrier(ctx)

			tCtx := p.Extract(ctx, c)
			got := oteltrace.SpanContextFromContext(tCtx)
			if !got.Equal(test.wantSC) {
				t.Fatalf("got span context: %v, want span context: %v", got, test.wantSC)
			}
		})
	}
}

// TestBinary verifies that the toBinary() function correctly serializes a valid
// OpenTelemetry span context into its binary format representation. If span
// context is invalid, it verifies that serialization is nil.
func (s) TestToBinary(t *testing.T) {
	tests := []struct {
		name string
		sc   oteltrace.SpanContext
		want []byte
	}{
		{
			name: "valid context",
			sc:   validSpanContext,
			want: toBinary(validSpanContext),
		},
		{
			name: "zero value context",
			sc:   oteltrace.SpanContext{},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := toBinary(test.sc); !cmp.Equal(got, test.want) {
				t.Fatalf("binary() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestFromBinary verifies that the fromBinary() function correctly
// deserializes a binary format representation of a valid OpenTelemetry span
// context into its corresponding span context format. If span context's binary
// representation is invalid, it verifies that deserialization is zero value
// span context.
func (s) TestFromBinary(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		want oteltrace.SpanContext
		ok   bool
	}{
		{
			name: "valid",
			b:    []byte{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 17, 18, 19, 20, 21, 22, 23, 24, 2, 1},
			want: validSpanContext.WithRemote(true),
			ok:   true,
		},
		{
			name: "invalid length",
			b:    []byte{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 17, 18, 19, 20, 21, 22, 23, 24, 2},
			want: oteltrace.SpanContext{},
			ok:   false,
		},
		{
			name: "invalid version",
			b:    []byte{1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 17, 18, 19, 20, 21, 22, 23, 24, 2, 1},
			want: oteltrace.SpanContext{},
			ok:   false,
		},
		{
			name: "invalid traceID field ID",
			b:    []byte{0, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 17, 18, 19, 20, 21, 22, 23, 24, 2, 1},
			want: oteltrace.SpanContext{},
			ok:   false,
		},
		{
			name: "invalid spanID field ID",
			b:    []byte{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 0, 17, 18, 19, 20, 21, 22, 23, 24, 2, 1},
			want: oteltrace.SpanContext{},
			ok:   false,
		},
		{
			name: "invalid traceFlags field ID",
			b:    []byte{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 17, 18, 19, 20, 21, 22, 23, 24, 1, 1},
			want: oteltrace.SpanContext{},
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := fromBinary(test.b)
			if ok != test.ok {
				t.Fatalf("fromBinary() ok = %v, want %v", ok, test.ok)
				return
			}
			if !got.Equal(test.want) {
				t.Fatalf("fromBinary() got = %v, want %v", got, test.want)
			}
		})
	}
}
