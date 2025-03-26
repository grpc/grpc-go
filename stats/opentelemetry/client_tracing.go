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

	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"google.golang.org/grpc/status"
)

const tracerName = "grpc-go"

type clientTracingHandler struct {
	options Options
}

func (h *clientTracingHandler) initializeTraces() {
	if h.options.TraceOptions.TracerProvider == nil {
		log.Printf("TracerProvider is not provided in client TraceOptions")
		return
	}
}

func (h *clientTracingHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ci := getCallInfo(ctx)
	if ci == nil {
		if logger.V(2) {
			logger.Info("Creating new CallInfo since its not present in context in clientTracingHandler unaryInterceptor")
		}
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

func (h *clientTracingHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ci := getCallInfo(ctx)
	if ci == nil {
		if logger.V(2) {
			logger.Info("Creating new CallInfo since its not present in context in clientTracingHandler streamInterceptor")
		}
		ci = &callInfo{
			target: cc.CanonicalTarget(),
			method: determineMethod(method, opts...),
		}
		ctx = setCallInfo(ctx, ci)
	}

	var span trace.Span
	ctx, span = h.createCallTraceSpan(ctx, method)
	callback := func(err error) { h.perCallTraces(err, span) }
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	return streamer(ctx, desc, cc, method, opts...)
}

// perCallTraces sets the span status based on the RPC result and ends the span.
// It is used to finalize tracing for both unary and streaming calls.
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
	tracer := h.options.TraceOptions.TracerProvider.Tracer(tracerName, trace.WithInstrumentationVersion(grpc.Version))
	ctx, span := tracer.Start(ctx, mn)
	carrier := otelinternaltracing.NewOutgoingCarrier(ctx)
	h.options.TraceOptions.TextMapPropagator.Inject(ctx, carrier)
	ai.traceSpan = span
	return carrier.Context(), ai
}

// createCallTraceSpan creates a call span to put in the provided context using
// provided TraceProvider. If TraceProvider is nil, it returns context as is.
func (h *clientTracingHandler) createCallTraceSpan(ctx context.Context, method string) (context.Context, trace.Span) {
	mn := strings.Replace(removeLeadingSlash(method), "/", ".", -1)
	tracer := h.options.TraceOptions.TracerProvider.Tracer(tracerName, trace.WithInstrumentationVersion(grpc.Version))
	ctx, span := tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindClient))
	return ctx, span
}

// TagConn exists to satisfy stats.Handler for tracing.
func (h *clientTracingHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler for tracing.
func (h *clientTracingHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC attempt context management for traces.
func (h *clientTracingHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	ri := getRPCInfo(ctx)
	var ai *attemptInfo
	if ri == nil {
		ai = &attemptInfo{}
	} else {
		ai = ri.ai
	}
	ctx, ai = h.traceTagRPC(ctx, ai)
	return setRPCInfo(ctx, &rpcInfo{ai: ai})
}

// HandleRPC handles per RPC tracing implementation.
func (h *clientTracingHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		logger.Error("ctx passed into client side tracing handler trace event handling has no client attempt data present")
		return
	}
	populateSpan(rs, ri.ai)
}
