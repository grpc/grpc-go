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

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestInject(t *testing.T) {
	propagator := GRPCTraceBinPropagator{}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	t.Run("fast path with CustomMapCarrier", func(t *testing.T) {
		carrier := otelinternaltracing.CustomMapCarrier{MD: metadata.MD{}}
		propagator.Inject(ctx, carrier)

		got, error := carrier.GetBinary(otelinternaltracing.GRPCTraceBinHeaderKey)
		if error != nil {
			t.Fatalf("got non-nil error, want no error")
		}

		want := Binary(spanContext)
		if string(got) != string(want) {
			t.Errorf("got = %v, want %v", got, want)
		}
	})

	t.Run("slow path with TextMapCarrier", func(t *testing.T) {
		carrier := propagation.MapCarrier{}
		propagator.Inject(ctx, carrier)

		got := carrier.Get(otelinternaltracing.GRPCTraceBinHeaderKey)
		want := base64.StdEncoding.EncodeToString(Binary(spanContext))
		if got != want {
			t.Errorf("got = %v, want %v", got, want)
		}
	})
}

func (s) TestExtract(t *testing.T) {
	propagator := GRPCTraceBinPropagator{}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     [8]byte{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	binaryData := Binary(spanContext)

	t.Run("fast path with CustomMapCarrier", func(t *testing.T) {
		carrier := otelinternaltracing.CustomMapCarrier{MD: metadata.MD{
			otelinternaltracing.GRPCTraceBinHeaderKey: []string{string(binaryData)},
		}}
		ctx := propagator.Extract(context.Background(), carrier)
		got := trace.SpanContextFromContext(ctx)

		if !got.Equal(spanContext) {
			t.Errorf("got = %v, want %v", got, spanContext)
		}
	})

	t.Run("slow path with TextMapCarrier", func(t *testing.T) {
		carrier := propagation.MapCarrier{
			otelinternaltracing.GRPCTraceBinHeaderKey: base64.StdEncoding.EncodeToString(binaryData),
		}
		ctx := propagator.Extract(context.Background(), carrier)
		got := trace.SpanContextFromContext(ctx)

		if !got.Equal(spanContext) {
			t.Errorf("got = %v, want %v", got, spanContext)
		}
	})
}
