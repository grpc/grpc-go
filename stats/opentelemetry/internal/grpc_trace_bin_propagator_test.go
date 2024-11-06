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

package internal

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/go-cmp/cmp"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// TODO: Move out of internal as part of open telemetry API

// validSpanContext is a valid OpenTelemetry span context.
var validSpanContext = oteltrace.SpanContext{}.WithTraceID(
	oteltrace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}).WithSpanID(
	oteltrace.SpanID{17, 18, 19, 20, 21, 22, 23, 24}).WithTraceFlags(
	oteltrace.TraceFlags(1))

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestInject verifies that the GRPCTraceBinPropagator correctly injects
// OpenTelemetry span context as `grpc-trace-bin` header in the provided
// carrier's context, for both fast and slow path, if span context is valid. If
// span context is invalid, carrier's context is not modified which is verified
// by retrieving zero value span context from carrier's context.
//
// For fast path, it passes `CustomCarrier` as carrier to `Inject()` which
// injects the span context using `CustomCarrier.SetBinary()`. It then
// retrieves the injected span context using `stats.OutgoingTrace()` for
// verification because `SetBinary()` does injection directly in binary format.
//
// For slow path, it passes `otel.MapCarrier` as carrier to `Inject()` which
// injects the span context after base64 encoding as string using
// `carrier.Set()`. It then retrieves the injected span context using
// `carrier.Get()` for verification because `Set()` does injection in string
// format.
func (s) TestInject(t *testing.T) {
	tests := []struct {
		name    string
		sc      oteltrace.SpanContext
		fast    bool // to indicate whether to follow fast path or slow path for injection verification
		validSC bool // to indicate whether to expect a valid span context or not
	}{
		{
			name:    "fast path, valid context",
			sc:      validSpanContext,
			fast:    true,
			validSC: true,
		},
		{
			name:    "fast path, invalid context",
			sc:      oteltrace.SpanContext{},
			fast:    true,
			validSC: false,
		},
		{
			name:    "slow path, valid context",
			sc:      validSpanContext,
			fast:    false,
			validSC: true,
		},
		{
			name:    "slow path, invalid context",
			sc:      oteltrace.SpanContext{},
			fast:    false,
			validSC: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := GRPCTraceBinPropagator{}
			tCtx, tCancel := context.WithCancel(context.Background())
			tCtx = oteltrace.ContextWithSpanContext(tCtx, test.sc)
			defer tCancel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var c otelpropagation.TextMapCarrier
			if test.fast {
				c = itracing.NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{}))
			} else {
				c = otelpropagation.MapCarrier{}
			}
			p.Inject(tCtx, c)

			var gotSC oteltrace.SpanContext
			var gotValidSC bool
			if test.fast {
				if gotSC, gotValidSC = fromBinary(stats.OutgoingTrace(c.(*itracing.CustomCarrier).Context())); test.validSC != gotValidSC {
					t.Fatalf("got invalid span context in CustomCarrier's context from grpc-trace-bin header: %v, want valid span context", stats.OutgoingTrace(c.(*itracing.CustomCarrier).Context()))
				}
			} else {
				b, err := base64.StdEncoding.DecodeString(c.Get(itracing.GRPCTraceBinHeaderKey))
				if err != nil {
					t.Fatalf("failed to decode MapCarrier's grpc-trace-bin base64 string header %s to binary: %v", c.Get(itracing.GRPCTraceBinHeaderKey), err)
				}
				if gotSC, gotValidSC = fromBinary(b); test.validSC != gotValidSC {
					t.Fatalf("got invalid span context in MapCarrier's context from grpc-trace-bin header: %v, want valid span context", b)
				}
			}
			if test.sc.TraceID() != gotSC.TraceID() && test.sc.SpanID() != gotSC.SpanID() && test.sc.TraceFlags() != gotSC.TraceFlags() {
				t.Fatalf("got span context = %v, want span contexts %v", gotSC, test.sc)
			}
		})
	}
}

// TestExtract verifies that the GRPCTraceBinPropagator correctly extracts
// OpenTelemetry span context data for fast and slow path, if valid span
// context was injected. If invalid context was injected, it verifies that a
// zero value span context was retrieved.
//
// For fast path, it uses the CustomCarrier and sets the span context in the
// binary format using `stats.SetIncomingTrace` in its context and then
// verifies that same span context is extracted directly in binary foramt.
//
// For slow path, it uses a MapCarrier and sets base64 encoded span context in
// its context and then verifies that same span context is extracted after
// base64 decoding.
func (s) TestExtract(t *testing.T) {
	tests := []struct {
		name string
		sc   oteltrace.SpanContext
		fast bool // to indicate whether to follow fast path or slow path for extraction verification
	}{
		{
			name: "fast path, valid context",
			sc:   validSpanContext.WithRemote(true),
			fast: true,
		},
		{
			name: "fast path, invalid context",
			sc:   oteltrace.SpanContext{},
			fast: true,
		},
		{
			name: "slow path, valid context",
			sc:   validSpanContext.WithRemote(true),
			fast: false,
		},
		{
			name: "slow path, invalid context",
			sc:   oteltrace.SpanContext{},
			fast: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := GRPCTraceBinPropagator{}
			bd := binary(test.sc)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var c otelpropagation.TextMapCarrier
			if test.fast {
				c = itracing.NewCustomCarrier(stats.SetIncomingTrace(ctx, bd))
			} else {
				c = otelpropagation.MapCarrier{itracing.GRPCTraceBinHeaderKey: base64.StdEncoding.EncodeToString(bd)}
			}
			tCtx := p.Extract(ctx, c)
			got := oteltrace.SpanContextFromContext(tCtx)
			if !got.Equal(test.sc) {
				t.Fatalf("got = %v, want %v", got, test.sc)
			}
		})
	}
}

// TestBinary verifies that the binary() function correctly serializes a valid
// OpenTelemetry span context into its binary format representation. If span
// context is invalid, it verifies that serialization is nil.
func (s) TestBinary(t *testing.T) {
	tests := []struct {
		name string
		sc   oteltrace.SpanContext
		want []byte
	}{
		{
			name: "valid context",
			sc:   validSpanContext,
			want: binary(validSpanContext),
		},
		{
			name: "zero value context",
			sc:   oteltrace.SpanContext{},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := binary(test.sc); !cmp.Equal(got, test.want) {
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
