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

// TestInject verifies that the GRPCTraceBinPropagator correctly injects
// OpenTelemetry trace context data. It tests both the fast path (using a
// CustomCarrier) and the slow path (using any other TextMapCarrier).
//
// The fast path injects the trace context directly in binary format where as
// the slow path base64 encodes the trace context before injecting it.
func (s) TestInject(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
	})
	tCtx, tCancel := context.WithCancel(context.Background())
	tCtx = oteltrace.ContextWithSpanContext(tCtx, sc)
	defer tCancel()

	t.Run("Fast path with CustomCarrier", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := itracing.NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{}))
		p.Inject(tCtx, c)

		got := stats.OutgoingTrace(c.Context())
		want := Binary(sc)
		if string(got) != string(want) {
			t.Fatalf("got = %v, want %v", got, want)
		}
		cancel()
	})

	t.Run("Slow path with any Text Carrier", func(t *testing.T) {
		c := otelpropagation.MapCarrier{}
		p.Inject(tCtx, c)

		got := c.Get(itracing.GRPCTraceBinHeaderKey)
		want := base64.StdEncoding.EncodeToString(Binary(sc))
		if got != want {
			t.Fatalf("got = %v, want %v", got, want)
		}
	})
}

// TestExtract verifies that the GRPCTraceBinPropagator correctly extracts
// OpenTelemetry trace context data. It tests both the fast path (using a
// CustomCarrier) and the slow path (using any other TextMapCarrier).
//
// The fast path extracts the trace context directly from the binary format
// where as the slow path base64 decodes the trace context before extracting
// it.
func (s) TestExtract(t *testing.T) {
	p := GRPCTraceBinPropagator{}
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	bd := Binary(sc)

	t.Run("Fast path with CustomCarrier", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := itracing.NewCustomCarrier(stats.SetIncomingTrace(ctx, bd))
		tCtx := p.Extract(ctx, c)
		got := oteltrace.SpanContextFromContext(tCtx)

		if !got.Equal(sc) {
			t.Fatalf("got = %v, want %v", got, sc)
		}
		cancel()
	})

	t.Run("Slow path with any Text Carrier", func(t *testing.T) {
		c := otelpropagation.MapCarrier{
			itracing.GRPCTraceBinHeaderKey: base64.StdEncoding.EncodeToString(bd),
		}
		ctx, cancel := context.WithCancel(context.Background())
		tCtx := p.Extract(ctx, c)
		got := oteltrace.SpanContextFromContext(tCtx)

		if !got.Equal(sc) {
			t.Fatalf("got = %v, want %v", got, sc)
		}
		cancel()
	})
}
