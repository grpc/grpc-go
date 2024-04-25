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
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type serverStatsHandler struct {
	o Options

	serverMetrics serverMetrics
}

func (ssh *serverStatsHandler) initializeMetrics() {
	// Will set no metrics to record, logically making this stats handler a
	// no-op.
	if ssh.o.MetricsOptions.MeterProvider == nil {
		return
	}

	meter := ssh.o.MetricsOptions.MeterProvider.Meter("grpc-go " + grpc.Version)
	if meter == nil {
		return
	}
	setOfMetrics := ssh.o.MetricsOptions.Metrics.metrics

	ssh.serverMetrics.callStarted = createInt64Counter(setOfMetrics, "grpc.server.call.started", meter, metric.WithUnit("call"), metric.WithDescription("Number of server calls started."))
	ssh.serverMetrics.callSentTotalCompressedMessageSize = createInt64Histogram(setOfMetrics, "grpc.server.call.sent_total_compressed_message_size", meter, metric.WithUnit("By"), metric.WithDescription("Compressed message bytes sent per server call."), metric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
	ssh.serverMetrics.callRcvdTotalCompressedMessageSize = createInt64Histogram(setOfMetrics, "grpc.server.call.rcvd_total_compressed_message_size", meter, metric.WithUnit("By"), metric.WithDescription("Compressed message bytes received per server call."), metric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
	ssh.serverMetrics.callDuration = createFloat64Histogram(setOfMetrics, "grpc.server.call.duration", meter, metric.WithUnit("s"), metric.WithDescription("End-to-end time taken to complete a call from server transport's perspective."), metric.WithExplicitBucketBoundaries(DefaultLatencyBounds...))
}

// TagConn exists to satisfy stats.Handler.
func (ssh *serverStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (ssh *serverStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC context management.
func (ssh *serverStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	method := info.FullMethodName
	if ssh.o.MetricsOptions.MethodAttributeFilter != nil {
		if !ssh.o.MetricsOptions.MethodAttributeFilter(method) {
			method = "other"
		}
	}
	server := internal.ServerFromContext.(func(context.Context) *grpc.Server)(ctx)
	if server == nil { // Shouldn't happen, defensive programming.
		logger.Error("ctx passed into server side stats handler has no grpc server ref")
		method = "other"
	} else {
		isRegisteredMethod := internal.IsRegisteredMethod.(func(*grpc.Server, string) bool)
		if !isRegisteredMethod(server, method) {
			method = "other"
		}
	}

	mi := &metricsInfo{
		startTime: time.Now(),
		method:    removeLeadingSlash(method),
	}
	ri := &rpcInfo{
		mi: mi,
	}
	return setRPCInfo(ctx, ri)
}

// HandleRPC implements per RPC tracing and stats implementation.
func (ssh *serverStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		logger.Error("ctx passed into server side stats handler metrics event handling has no server call data present")
		return
	}
	ssh.processRPCData(ctx, rs, ri.mi)
}

func (ssh *serverStatsHandler) processRPCData(ctx context.Context, s stats.RPCStats, mi *metricsInfo) {
	switch st := s.(type) {
	case *stats.InHeader:
		ssh.serverMetrics.callStarted.Add(ctx, 1, metric.WithAttributes(attribute.String("grpc.method", mi.method)))
	case *stats.OutPayload:
		atomic.AddInt64(&mi.sentCompressedBytes, int64(st.CompressedLength))
	case *stats.InPayload:
		atomic.AddInt64(&mi.recvCompressedBytes, int64(st.CompressedLength))
	case *stats.End:
		ssh.processRPCEnd(ctx, mi, st)
	default:
	}
}

func (ssh *serverStatsHandler) processRPCEnd(ctx context.Context, mi *metricsInfo, e *stats.End) {
	latency := float64(time.Since(mi.startTime)) / float64(time.Second)
	st := "OK"
	if e.Error != nil {
		s, _ := status.FromError(e.Error)
		st = canonicalString(s.Code())
	}
	serverAttributeOption := metric.WithAttributes(attribute.String("grpc.method", mi.method), attribute.String("grpc.status", st))

	ssh.serverMetrics.callDuration.Record(ctx, latency, serverAttributeOption)
	ssh.serverMetrics.callSentTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&mi.sentCompressedBytes), serverAttributeOption)
	ssh.serverMetrics.callRcvdTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&mi.recvCompressedBytes), serverAttributeOption)
}

const (
	// ServerCallStarted is the number of server calls started.
	ServerCallStarted Metric = "grpc.server.call.started"
	// ServerCallSentCompressedTotalMessageSize is the compressed message bytes
	// sent per server call.
	ServerCallSentCompressedTotalMessageSize Metric = "grpc.server.call.sent_total_compressed_message_size"
	// ServerCallRcvdCompressedTotalMessageSize is the compressed message bytes
	// received per server call.
	ServerCallRcvdCompressedTotalMessageSize Metric = "grpc.server.call.rcvd_total_compressed_message_size"
	// ServerCallDuration is the end-to-end time taken to complete a call from
	// server transport's perspective.
	ServerCallDuration Metric = "grpc.server.call.duration"
)
