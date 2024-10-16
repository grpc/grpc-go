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
	"testing"

	otelpropagation "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	itracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// TODO: Move out of internal as part of open telemetry API

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestInject_FastPath verifies that the GRPCTraceBinPropagator correctly
// injects OpenTelemetry trace context data using the CustomCarrier.
//
// It is called the fast path because it injects the trace context directly in
// binary format.
func (s) TestInject_FastPath(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
	})
	tCtx, tCancel := context.WithCancel(context.Background())
	tCtx = oteltrace.ContextWithSpanContext(tCtx, sc)
	defer tCancel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := itracing.NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{}))
	p.Inject(tCtx, c)

	got := stats.OutgoingTrace(c.Context())
	want := Binary(sc)
	if string(got) != string(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
}

// TestInject_SlowPath verifies that the GRPCTraceBinPropagator correctly
// injects OpenTelemetry trace context data using any other text based carrier.
//
// It is called the slow path because it base64 encodes the binary trace
// context before injecting it.
func (s) TestInject_SlowPath(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
	})
	tCtx, tCancel := context.WithCancel(context.Background())
	tCtx = oteltrace.ContextWithSpanContext(tCtx, sc)
	defer tCancel()

	c := otelpropagation.MapCarrier{}
	p.Inject(tCtx, c)

	got := c.Get(itracing.GRPCTraceBinHeaderKey)
	want := base64.StdEncoding.EncodeToString(Binary(sc))
	if got != want {
		t.Fatalf("got = %v, want %v", got, want)
	}
}

// TestExtract_FastPath verifies that the GRPCTraceBinPropagator correctly
// extracts OpenTelemetry trace context data using the CustomCarrier.
//
// It is called the fast path because it extracts the trace context directly
// in the binary format.
func (s) TestExtract_FastPath(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	bd := Binary(sc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := itracing.NewCustomCarrier(stats.SetIncomingTrace(ctx, bd))
	tCtx := p.Extract(ctx, c)
	got := oteltrace.SpanContextFromContext(tCtx)

	if !got.Equal(sc) {
		t.Fatalf("got = %v, want %v", got, sc)
	}
}

// TestExtract_SlowPath verifies that the GRPCTraceBinPropagator correctly
// extracts OpenTelemetry trace context data using any other text based carrier.
//
// It is called the slow path because it base64 decodes the binary trace
// context before extracting it.
func (s) TestExtract_SlowPath(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	bd := Binary(sc)

	c := otelpropagation.MapCarrier{
		itracing.GRPCTraceBinHeaderKey: base64.StdEncoding.EncodeToString(bd),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tCtx := p.Extract(ctx, c)
	got := oteltrace.SpanContextFromContext(tCtx)

	if !got.Equal(sc) {
		t.Fatalf("got = %v, want %v", got, sc)
	}
}
