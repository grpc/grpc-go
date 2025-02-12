/*
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
 */

package opentelemetry

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"google.golang.org/grpc/status"
)

type clientTracingStatsHandler struct {
	*clientStatsHandler
}

// traceTagRPC populates provided context with a new span using the
// TextMapPropagator supplied in trace options and internal itracing.carrier.
// It creates a new outgoing carrier which serializes information about this
// span into gRPC Metadata, if TextMapPropagator is provided in the trace
// options. if TextMapPropagator is not provided, it returns the context as is.
func (h *clientStatsHandler) traceTagRPC(ctx context.Context, ai *attemptInfo) (context.Context, *attemptInfo) {
	mn := "Attempt." + strings.Replace(ai.method, "/", ".", -1)
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx, span := tracer.Start(ctx, mn)
	carrier := otelinternaltracing.NewOutgoingCarrier(ctx)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	ai.traceSpan = span
	return carrier.Context(), ai
}

// createCallTraceSpan creates a call span to put in the provided context using
// provided TraceProvider. If TraceProvider is nil, it returns context as is.
func (h *clientStatsHandler) createCallTraceSpan(ctx context.Context, method string) (context.Context, trace.Span) {
	if h.options.TraceOptions.TracerProvider == nil {
		logger.Error("TraceProvider is not provided in trace options")
		return ctx, nil
	}
	mn := strings.Replace(removeLeadingSlash(method), "/", ".", -1)
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx, span := tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindClient))
	return ctx, span
}

func (h *clientTracingStatsHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ci := &callInfo{
		target: cc.CanonicalTarget(),
		method: h.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)
	if h.options.MetricsOptions.pluginOption != nil {
		md := h.options.MetricsOptions.pluginOption.GetMetadata()
		for k, vs := range md {
			for _, v := range vs {
				ctx = metadata.AppendToOutgoingContext(ctx, k, v)
			}
		}
	}

	startTime := time.Now()
	var span trace.Span
	ctx, span = h.createCallTraceSpan(ctx, method)
	err := invoker(ctx, method, req, reply, cc, opts...)
	h.perCallTraces(ctx, err, startTime, ci, span)
	return err
}

func (h *clientTracingStatsHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ci := &callInfo{
		target: cc.CanonicalTarget(),
		method: h.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)

	if h.options.MetricsOptions.pluginOption != nil {
		md := h.options.MetricsOptions.pluginOption.GetMetadata()
		for k, vs := range md {
			for _, v := range vs {
				ctx = metadata.AppendToOutgoingContext(ctx, k, v)
			}
		}
	}

	startTime := time.Now()
	var span trace.Span
	ctx, span = h.createCallTraceSpan(ctx, method)
	callback := func(err error) {
		h.perCallTraces(ctx, err, startTime, ci, span)
	}
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	return streamer(ctx, desc, cc, method, opts...)
}

// perCallTraces records per call trace spans and metrics.
func (h *clientTracingStatsHandler) perCallTraces(_ context.Context, err error, _ time.Time, _ *callInfo, ts trace.Span) {
	s := status.Convert(err)
	if s.Code() == grpccodes.OK {
		ts.SetStatus(otelcodes.Ok, s.Message())
	} else {
		ts.SetStatus(otelcodes.Error, s.Message())
	}
	ts.End()
}
