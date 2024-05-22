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
	"google.golang.org/grpc/metadata"
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

// attachLabelsTransport stream intercepts SetHeader and SendHeader calls of the
// underlying ServerTransportStream to attach metadataExchangeLabels.
type attachLabelsTransportStream struct {
	grpc.ServerTransportStream

	attachedLabels         atomic.Bool
	metadataExchangeLabels metadata.MD
}

func (alts *attachLabelsTransportStream) SetHeader(md metadata.MD) error {
	if !alts.attachedLabels.Swap(true) {
		val := alts.metadataExchangeLabels.Get("x-envoy-peer-metadata")
		md.Append("x-envoy-peer-metadata", val...)
	}
	return alts.ServerTransportStream.SetHeader(md)
}

func (alts *attachLabelsTransportStream) SendHeader(md metadata.MD) error {
	if !alts.attachedLabels.Swap(true) {
		val := alts.metadataExchangeLabels.Get("x-envoy-peer-metadata")
		md.Append("x-envoy-peer-metadata", val...)
	}

	return alts.ServerTransportStream.SendHeader(md)
}

func (ssh *serverStatsHandler) unaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	var metadataExchangeLabels metadata.MD
	if ssh.o.MetricsOptions.pluginOption != nil {
		metadataExchangeLabels = ssh.o.MetricsOptions.pluginOption.GetMetadata()
	}

	sts := grpc.ServerTransportStreamFromContext(ctx)

	alts := &attachLabelsTransportStream{
		ServerTransportStream:  sts,
		metadataExchangeLabels: metadataExchangeLabels,
	}
	ctx = grpc.NewContextWithServerTransportStream(ctx, alts)

	any, err := handler(ctx, req)
	if err != nil { // error returned, so trailers only
		if !alts.attachedLabels.Swap(true) {
			alts.SetTrailer(alts.metadataExchangeLabels)
		}
		logger.Infof("RPC failed with error: %v", err)
	} else { // headers will be written; a message was sent
		if !alts.attachedLabels.Swap(true) {
			alts.SetHeader(alts.metadataExchangeLabels)
		}
	}

	return any, err
}

// attachLabelsStream embeds a grpc.ServerStream, and intercepts the
// SetHeader/SendHeader/SendMsg/SendTrailer call to attach metadata exchange
// labels.
type attachLabelsStream struct {
	grpc.ServerStream

	attachedLabels         atomic.Bool
	metadataExchangeLabels metadata.MD
}

func (als *attachLabelsStream) SetHeader(md metadata.MD) error {
	if !als.attachedLabels.Swap(true) {
		val := als.metadataExchangeLabels.Get("x-envoy-peer-metadata")
		md.Append("x-envoy-peer-metadata", val...)
	}

	return als.ServerStream.SetHeader(md)
}

func (als *attachLabelsStream) SendHeader(md metadata.MD) error {
	if !als.attachedLabels.Swap(true) {
		val := als.metadataExchangeLabels.Get("x-envoy-peer-metadata")
		md.Append("x-envoy-peer-metadata", val...)
	}

	return als.ServerStream.SendHeader(md)
}

func (als *attachLabelsStream) SendMsg(m any) error {
	if !als.attachedLabels.Swap(true) {
		als.ServerStream.SetHeader(als.metadataExchangeLabels)
	}
	return als.ServerStream.SendMsg(m)
}

func (ssh *serverStatsHandler) streamInterceptor(srv any, ss grpc.ServerStream, ssi *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	var metadataExchangeLabels metadata.MD
	if ssh.o.MetricsOptions.pluginOption != nil {
		metadataExchangeLabels = ssh.o.MetricsOptions.pluginOption.GetMetadata()
	}
	als := &attachLabelsStream{
		ServerStream:           ss,
		metadataExchangeLabels: metadataExchangeLabels,
	}
	err := handler(srv, als)
	if err != nil {
		logger.Infof("RPC failed with error: %v", err)
	}

	// Add metadata exchange labels to trailers if never sent in headers,
	// irrespective of whether or not RPC failed.
	if !als.attachedLabels.Load() {
		als.SetTrailer(als.metadataExchangeLabels)
	}
	return err
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

	ai := &attemptInfo{
		startTime: time.Now(),
		method:    removeLeadingSlash(method),
	}
	ri := &rpcInfo{
		ai: ai,
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
	ssh.processRPCData(ctx, rs, ri.ai)
}

func (ssh *serverStatsHandler) processRPCData(ctx context.Context, s stats.RPCStats, ai *attemptInfo) {
	switch st := s.(type) {
	case *stats.InHeader:
		if ai.pluginOptionLabels == nil && ssh.o.MetricsOptions.pluginOption != nil {
			ai.pluginOptionLabels = ssh.o.MetricsOptions.pluginOption.GetLabels(st.Header)
		}
		ssh.serverMetrics.callStarted.Add(ctx, 1, metric.WithAttributes(attribute.String("grpc.method", ai.method)))
	case *stats.OutPayload:
		atomic.AddInt64(&ai.sentCompressedBytes, int64(st.CompressedLength))
	case *stats.InPayload:
		atomic.AddInt64(&ai.recvCompressedBytes, int64(st.CompressedLength))
	case *stats.End:
		ssh.processRPCEnd(ctx, ai, st)
	default:
	}
}

func (ssh *serverStatsHandler) processRPCEnd(ctx context.Context, ai *attemptInfo, e *stats.End) {
	latency := float64(time.Since(ai.startTime)) / float64(time.Second)
	st := "OK"
	if e.Error != nil {
		s, _ := status.FromError(e.Error)
		st = canonicalString(s.Code())
	}
	attributes := []attribute.KeyValue{
		attribute.String("grpc.method", ai.method),
		attribute.String("grpc.status", st),
	}
	for k, v := range ai.pluginOptionLabels {
		attributes = append(attributes, attribute.String(k, v))
	}

	serverAttributeOption := metric.WithAttributes(attributes...)
	ssh.serverMetrics.callDuration.Record(ctx, latency, serverAttributeOption)
	ssh.serverMetrics.callSentTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&ai.sentCompressedBytes), serverAttributeOption)
	ssh.serverMetrics.callRcvdTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&ai.recvCompressedBytes), serverAttributeOption)
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
