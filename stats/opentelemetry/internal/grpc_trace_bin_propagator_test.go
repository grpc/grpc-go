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
	internaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// TODO: Move out of internal as part of open telemetry API

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestInject(t *testing.T) {
	propagator := GRPCTraceBinPropagator{}
	spanContext := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
	})
	traceCtx, traceCancel := context.WithCancel(context.Background())
	traceCtx = oteltrace.ContextWithSpanContext(traceCtx, spanContext)

	t.Run("Fast path with CustomCarrier", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		carrier := internaltracing.NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{}))
		propagator.Inject(traceCtx, carrier)

		got := stats.OutgoingTrace(*carrier.Context())
		want := Binary(spanContext)
		if string(got) != string(want) {
			t.Fatalf("got = %v, want %v", got, want)
		}
		cancel()
	})

	t.Run("Slow path with TextMapCarrier", func(t *testing.T) {
		carrier := otelpropagation.MapCarrier{}
		propagator.Inject(traceCtx, carrier)

		got := carrier.Get(internaltracing.GRPCTraceBinHeaderKey)
		want := base64.StdEncoding.EncodeToString(Binary(spanContext))
		if got != want {
			t.Fatalf("got = %v, want %v", got, want)
		}
	})

	traceCancel()
}

func (s) TestExtract(t *testing.T) {
	propagator := GRPCTraceBinPropagator{}
	spanContext := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	binaryData := Binary(spanContext)

	t.Run("Fast path with CustomCarrier", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		carrier := internaltracing.NewCustomCarrier(stats.SetIncomingTrace(ctx, binaryData))
		traceCtx := propagator.Extract(ctx, carrier)
		got := oteltrace.SpanContextFromContext(traceCtx)

		if !got.Equal(spanContext) {
			t.Fatalf("got = %v, want %v", got, spanContext)
		}
		cancel()
	})

	t.Run("Slow path with TextMapCarrier", func(t *testing.T) {
		carrier := otelpropagation.MapCarrier{
			internaltracing.GRPCTraceBinHeaderKey: base64.StdEncoding.EncodeToString(binaryData),
		}
		ctx, cancel := context.WithCancel(context.Background())
		traceCtx := propagator.Extract(ctx, carrier)
		got := oteltrace.SpanContextFromContext(traceCtx)

		if !got.Equal(spanContext) {
			t.Fatalf("got = %v, want %v", got, spanContext)
		}
		cancel()
	})
}
