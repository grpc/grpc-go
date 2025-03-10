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
	"log"
	"strings"

	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"google.golang.org/grpc/status"
)

type clientTracingHandler struct {
	options Options
	tracer  trace.Tracer
}

// initializeTraces initializes the tracer for client tracing handler.
func (h *clientTracingHandler) initializeTraces() {
	if h.options.TraceOptions.TracerProvider == nil {
		log.Printf("TraceProvider is not provided in trace options")
		return
	}
	h.tracer = h.options.TraceOptions.TracerProvider.Tracer("grpc-open-telemetry")
}

// unaryInterceptor implements the UnaryClientInterceptor to record traces
// for unary calls.
func (h *clientTracingHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ci := getCallInfo(ctx)
	if ci == nil {
		logger.Error("ctx passed into client tracing handler unary interceptor has no call info data present")
		ci = &callInfo{
			target: cc.CanonicalTarget(),
			method: determineMethod(method, opts...),
		}
		ctx = setCallInfo(ctx, ci)
	}

	var span trace.Span
	ctx, span = h.createCallTraceSpan(ctx, method)
	err := invoker(ctx, method, req, reply, cc, opts...)
	h.perCallTraces(err, span)
	return err
}

// streamInterceptor implements the StreamClientInterceptor to record traces
// for stream calls.
func (h *clientTracingHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ci := getCallInfo(ctx)
	if ci == nil {
		logger.Error("ctx passed into client tracing handler stream interceptor has no call info data present")
		ci = &callInfo{
			target: cc.CanonicalTarget(),
			method: determineMethod(method, opts...),
		}
		ctx = setCallInfo(ctx, ci)
	}

	var span trace.Span
	ctx, span = h.createCallTraceSpan(ctx, method)
	callback := func(err error) {
		h.perCallTraces(err, span)
	}
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	return streamer(ctx, desc, cc, method, opts...)
}

// perCallTraces records per call trace spans for both unary and stream calls.
func (h *clientTracingHandler) perCallTraces(err error, ts trace.Span) {
	s := status.Convert(err)
	if s.Code() == grpccodes.OK {
		ts.SetStatus(otelcodes.Ok, s.Message())
	} else {
		ts.SetStatus(otelcodes.Error, s.Message())
	}
	ts.End()
}

// traceTagRPC starts a new span for an RPC attempt and propagates its context.
// A new span is started using the configured Tracer. If a TextMapPropagator
// is configured in TraceOptions, the span's context is injected into the
// outgoing gRPC metadata using an internal carrier for cross-process propagation.
func (h *clientTracingHandler) traceTagRPC(ctx context.Context, ai *attemptInfo) (context.Context, *attemptInfo) {
	mn := "Attempt." + strings.Replace(ai.method, "/", ".", -1)
	ctx, span := h.tracer.Start(ctx, mn)
	carrier := otelinternaltracing.NewOutgoingCarrier(ctx)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	ai.traceSpan = span
	return carrier.Context(), ai
}

// createCallTraceSpan creates a call span to put in the provided context using
// provided TraceProvider. If TraceProvider is nil, it returns context as is.
func (h *clientTracingHandler) createCallTraceSpan(ctx context.Context, method string) (context.Context, trace.Span) {
	if h.options.TraceOptions.TracerProvider == nil {
		logger.Error("TraceProvider is not provided in trace options")
		return ctx, nil
	}
	mn := strings.Replace(removeLeadingSlash(method), "/", ".", -1)
	ctx, span := h.tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindClient))
	return ctx, span
}

// TagConn exists to satisfy stats.Handler for tracing.
func (h *clientTracingHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler for tracing.
func (h *clientTracingHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC attempt context management for tracing.
// Fetches rpcInfo from context. This handler requires a preceding stats handler
// in the interceptor chain to have already created and set the rpcInfo.
func (h *clientTracingHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	ri := getRPCInfo(ctx)
	if ri == nil {
		logger.Error("ctx passed into client side stats handler metrics event handling has no client attempt data present")
		return ctx
	}
	ctx, ai := h.traceTagRPC(ctx, ri.ai)
	return setRPCInfo(ctx, &rpcInfo{ai: ai})
}

// HandleRPC implements per RPC attempt stats handling for tracing.
func (h *clientTracingHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		logger.Error("ctx passed into client side stats handler metrics event handling has no client attempt data present")
		return
	}
	populateSpan(rs, ri.ai)
}
