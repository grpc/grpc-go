/*
 * Copyright 2022 gRPC authors.
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

// Package opencensus implements opencensus instrumentation code for gRPC-Go
// clients and servers.
package opencensus

import (
	"context"
	"strings"
	"time"

	ocstats "go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

var (
	joinDialOptions = internal.JoinDialOptions.(func(...grpc.DialOption) grpc.DialOption)
)

// TraceOptions are the tracing options for opencensus instrumentation.
type TraceOptions struct {
	// TS is the Sampler used for tracing.
	TS trace.Sampler
	// DisableTrace determines whether traces are disabled for an OpenCensus
	// Dial or Server option. will overwrite any global option setting.
	DisableTrace bool
}

// DialOption returns a dial option which enables OpenCensus instrumentation
// code for a grpc.ClientConn.
//
// Client applications interested in instrumenting their grpc.ClientConn should
// pass the dial option returned from this function as the first dial option to
// grpc.Dial().
//
// Using this option will always lead to instrumentation, however in order to
// use the data an exporter must be registered with the OpenCensus trace package
// for traces and the OpenCensus view package for metrics. Client side has
// retries, so a Unary and Streaming Interceptor are registered to handle per
// RPC traces/metrics, and a Stats Handler is registered to handle per RPC
// attempt trace/metrics. These three components registered work together in
// conjunction, and do not work standalone. It is not supported to use this
// alongside another stats handler dial option.
func DialOption(to TraceOptions) grpc.DialOption {
	csh := &clientStatsHandler{to: to}
	return joinDialOptions(grpc.WithChainUnaryInterceptor(csh.unaryInterceptor), grpc.WithChainStreamInterceptor(csh.streamInterceptor), grpc.WithStatsHandler(csh))
}

// ServerOption returns a server option which enables OpenCensus instrumentation
// code for a grpc.Server.
//
// Server applications interested in instrumenting their grpc.Server should
// pass the server option returned from this function as the first argument to
// grpc.NewServer().
//
// Using this option will always lead to instrumentation, however in order to
// use the data an exporter must be registered with the OpenCensus trace package
// for traces and the OpenCensus view package for metrics. Server side does not
// have retries, so a registered Stats Handler is the only option that is
// returned. It is not supported to use this alongside another stats handler
// server option.
func ServerOption(to TraceOptions) grpc.ServerOption {
	return grpc.StatsHandler(&serverStatsHandler{to: to})
}

// createCallSpan creates a call span if tracing is enabled, which will be put
// in the context provided if created.
func (csh *clientStatsHandler) createCallSpan(ctx context.Context, method string) (context.Context, *trace.Span) {
	var span *trace.Span
	if !csh.to.DisableTrace {
		mn := strings.ReplaceAll(removeLeadingSlash(method), "/", ".")
		ctx, span = trace.StartSpan(ctx, mn, trace.WithSampler(csh.to.TS), trace.WithSpanKind(trace.SpanKindClient))
	}
	return ctx, span
}

// perCallTracesAndMetrics records per call spans and metrics.
func perCallTracesAndMetrics(err error, span *trace.Span, startTime time.Time, method string) {
	s := status.Convert(err)
	if span != nil {
		span.SetStatus(trace.Status{Code: int32(s.Code()), Message: s.Message()})
		span.End()
	}
	callLatency := float64(time.Since(startTime)) / float64(time.Millisecond)
	ocstats.RecordWithOptions(context.Background(),
		ocstats.WithTags(
			tag.Upsert(keyClientMethod, removeLeadingSlash(method)),
			tag.Upsert(keyClientStatus, canonicalString(s.Code())),
		),
		ocstats.WithMeasurements(
			clientAPILatency.M(callLatency),
		),
	)
}

// unaryInterceptor handles per RPC context management. It also handles per RPC
// tracing and stats by creating a top level call span and recording the latency
// for the full RPC call.
func (csh *clientStatsHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	startTime := time.Now()
	ctx, span := csh.createCallSpan(ctx, method)
	err := invoker(ctx, method, req, reply, cc, opts...)
	perCallTracesAndMetrics(err, span, startTime, method)
	return err
}

// streamInterceptor handles per RPC context management. It also handles per RPC
// tracing and stats by creating a top level call span and recording the latency
// for the full RPC call.
func (csh *clientStatsHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	startTime := time.Now()
	ctx, span := csh.createCallSpan(ctx, method)
	callback := func(err error) {
		perCallTracesAndMetrics(err, span, startTime, method)
	}
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}
	return s, nil
}

type rpcInfo struct {
	mi *metricsInfo
	ti *traceInfo
}

type rpcInfoKey struct{}

func setRPCInfo(ctx context.Context, ri *rpcInfo) context.Context {
	return context.WithValue(ctx, rpcInfoKey{}, ri)
}

// getRPCInfo returns the rpcInfo stored in the context, or nil
// if there isn't one.
func getRPCInfo(ctx context.Context) *rpcInfo {
	ri, _ := ctx.Value(rpcInfoKey{}).(*rpcInfo)
	return ri
}

// SpanContextFromContext returns the Span Context about the Span in the
// context. Returns false if no Span in the context.
func SpanContextFromContext(ctx context.Context) (trace.SpanContext, bool) {
	ri, ok := ctx.Value(rpcInfoKey{}).(*rpcInfo)
	if !ok {
		return trace.SpanContext{}, false
	}
	if ri.ti == nil || ri.ti.span == nil {
		return trace.SpanContext{}, false
	}
	sc := ri.ti.span.SpanContext()
	return sc, true
}

type clientStatsHandler struct {
	to TraceOptions
}

// TagConn exists to satisfy stats.Handler.
func (csh *clientStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (csh *clientStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC attempt context management.
func (csh *clientStatsHandler) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	ctx, mi := csh.statsTagRPC(ctx, rti)
	var ti *traceInfo
	if !csh.to.DisableTrace {
		ctx, ti = csh.traceTagRPC(ctx, rti)
	}
	ri := &rpcInfo{
		mi: mi,
		ti: ti,
	}
	return setRPCInfo(ctx, ri)
}

func (csh *clientStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		// Shouldn't happen because TagRPC populates this information.
		return
	}
	recordRPCData(ctx, rs, ri.mi)
	if !csh.to.DisableTrace {
		populateSpan(ctx, rs, ri.ti)
	}
}

type serverStatsHandler struct {
	to TraceOptions
}

// TagConn exists to satisfy stats.Handler.
func (ssh *serverStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (ssh *serverStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC context management.
func (ssh *serverStatsHandler) TagRPC(ctx context.Context, rti *stats.RPCTagInfo) context.Context {
	ctx, mi := ssh.statsTagRPC(ctx, rti)
	var ti *traceInfo
	if !ssh.to.DisableTrace {
		ctx, ti = ssh.traceTagRPC(ctx, rti)
	}
	ri := &rpcInfo{
		mi: mi,
		ti: ti,
	}
	return setRPCInfo(ctx, ri)
}

// HandleRPC implements per RPC tracing and stats implementation.
func (ssh *serverStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		// Shouldn't happen because TagRPC populates this information.
		return
	}
	recordRPCData(ctx, rs, ri.mi)
	if !ssh.to.DisableTrace {
		populateSpan(ctx, rs, ri.ti)
	}
}
