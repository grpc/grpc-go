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

	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	estats "google.golang.org/grpc/experimental/stats"
	istats "google.golang.org/grpc/internal/stats"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	otelattribute "go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type clientStatsHandler struct {
	estats.MetricsRecorder
	options Options
	metrics metricsHandler
}

type metricsHandler struct {
	clientMetrics clientMetrics
}

func (h *clientStatsHandler) initializeMetrics() {
	// Will set no metrics to record, logically making this stats handler a
	// no-op.
	if h.options.MetricsOptions.MeterProvider == nil {
		return
	}

	meter := h.options.MetricsOptions.MeterProvider.Meter("grpc-go", otelmetric.WithInstrumentationVersion(grpc.Version))
	if meter == nil {
		return
	}

	metrics := h.options.MetricsOptions.Metrics
	if metrics == nil {
		metrics = DefaultMetrics()
	}

	h.metrics.clientMetrics.attemptStarted = createInt64Counter(metrics.Metrics(), "grpc.client.attempt.started", meter, otelmetric.WithUnit("attempt"), otelmetric.WithDescription("Number of client call attempts started."))
	h.metrics.clientMetrics.attemptDuration = createFloat64Histogram(metrics.Metrics(), "grpc.client.attempt.duration", meter, otelmetric.WithUnit("s"), otelmetric.WithDescription("End-to-end time taken to complete a client call attempt."), otelmetric.WithExplicitBucketBoundaries(DefaultLatencyBounds...))
	h.metrics.clientMetrics.attemptSentTotalCompressedMessageSize = createInt64Histogram(metrics.Metrics(), "grpc.client.attempt.sent_total_compressed_message_size", meter, otelmetric.WithUnit("By"), otelmetric.WithDescription("Compressed message bytes sent per client call attempt."), otelmetric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
	h.metrics.clientMetrics.attemptRcvdTotalCompressedMessageSize = createInt64Histogram(metrics.Metrics(), "grpc.client.attempt.rcvd_total_compressed_message_size", meter, otelmetric.WithUnit("By"), otelmetric.WithDescription("Compressed message bytes received per call attempt."), otelmetric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
	h.metrics.clientMetrics.callDuration = createFloat64Histogram(metrics.Metrics(), "grpc.client.call.duration", meter, otelmetric.WithUnit("s"), otelmetric.WithDescription("Time taken by gRPC to complete an RPC from application's perspective."), otelmetric.WithExplicitBucketBoundaries(DefaultLatencyBounds...))

	rm := &registryMetrics{
		optionalLabels: h.options.MetricsOptions.OptionalLabels,
	}
	h.MetricsRecorder = rm
	rm.registerMetrics(metrics, meter)
}

func (h *clientStatsHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ci := &callInfo{
		target: cc.CanonicalTarget(),
		method: h.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)

	metricsEnabled := h.options.isMetricsEnabled()
	tracingEnabled := h.options.isTracingEnabled()

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
	if tracingEnabled {
		ctx, span = h.createCallTraceSpan(ctx, method)
	}
	err := invoker(ctx, method, req, reply, cc, opts...)

	if metricsEnabled {
		h.metrics.perCallMetrics(ctx, err, startTime, ci)
	}
	if tracingEnabled {
		h.perCallTraces(ctx, err, startTime, ci, span)
	}
	return err
}

// determineMethod determines the method to record attributes with. This will be
// "other" if StaticMethod isn't specified or if method filter is set and
// specifies, the method name as is otherwise.
func (h *clientStatsHandler) determineMethod(method string, opts ...grpc.CallOption) string {
	for _, opt := range opts {
		if _, ok := opt.(grpc.StaticMethodCallOption); ok {
			return removeLeadingSlash(method)
		}
	}
	return "other"
}

func (h *clientStatsHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ci := &callInfo{
		target: cc.CanonicalTarget(),
		method: h.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)
	metricsEnabled := h.options.isMetricsEnabled()
	tracingEnabled := h.options.isTracingEnabled()
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
	if tracingEnabled {
		ctx, span = h.createCallTraceSpan(ctx, method)
	}
	callback := func(err error) {
		if metricsEnabled {
			h.metrics.perCallMetrics(ctx, err, startTime, ci)
		}
		if tracingEnabled {
			h.perCallTraces(ctx, err, startTime, ci, span)
		}
	}
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	return streamer(ctx, desc, cc, method, opts...)
}

// perCallTraces records per call trace spans.
func (h *clientStatsHandler) perCallTraces(_ context.Context, err error, _ time.Time, _ *callInfo, ts trace.Span) {
	if h.options.isTracingEnabled() {
		s := status.Convert(err)
		if s.Code() == grpccodes.OK {
			ts.SetStatus(otelcodes.Ok, s.Message())
		} else {
			ts.SetStatus(otelcodes.Error, s.Message())
		}
		ts.End()
	}
}

// perCallMetrics records per call metrics.
func (h *metricsHandler) perCallMetrics(ctx context.Context, err error, startTime time.Time, ci *callInfo) {
	if h.clientMetrics.callDuration != nil {
		callLatency := float64(time.Since(startTime)) / float64(time.Second)
		attrs := otelattribute.NewSet(
			otelattribute.String("grpc.method", ci.method),
			otelattribute.String("grpc.target", ci.target),
			otelattribute.String("grpc.status", canonicalString(status.Code(err))),
		)
		h.clientMetrics.callDuration.Record(ctx, callLatency, otelmetric.WithAttributeSet(attrs))
	}
}

// TagConn exists to satisfy stats.Handler.
func (h *clientStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (h *clientStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC attempt context management.
func (h *clientStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	// Numerous stats handlers can be used for the same channel. The cluster
	// impl balancer which writes to this will only write once, thus have this
	// stats handler's per attempt scoped context point to the same optional
	// labels map if set.
	var labels *istats.Labels
	if labels = istats.GetLabels(ctx); labels == nil {
		labels = &istats.Labels{
			// The defaults for all the per call labels from a plugin that
			// executes on the callpath that this OpenTelemetry component
			// currently supports.
			TelemetryLabels: map[string]string{
				"grpc.lb.locality": "",
			},
		}
		ctx = istats.SetLabels(ctx, labels)
	}
	ai := &attemptInfo{
		startTime: time.Now(),
		xdsLabels: labels.TelemetryLabels,
		method:    removeLeadingSlash(info.FullMethodName),
	}
	if h.options.isTracingEnabled() {
		ctx, ai = h.traceTagRPC(ctx, ai)
	}
	return setRPCInfo(ctx, &rpcInfo{
		ai: ai,
	})
}

func (h *clientStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		logger.Error("ctx passed into client side stats handler metrics event handling has no client attempt data present")
		return
	}
	if h.options.isMetricsEnabled() {
		h.processRPCEvent(ctx, rs, ri.ai)
	}
	if h.options.isTracingEnabled() {
		populateSpan(rs, ri.ai)
	}
}

func (h *clientStatsHandler) processRPCEvent(ctx context.Context, s stats.RPCStats, ai *attemptInfo) {
	switch st := s.(type) {
	case *stats.Begin:
		ci := getCallInfo(ctx)
		if ci == nil {
			logger.Error("ctx passed into client side stats handler metrics event handling has no metrics data present")
			return
		}

		attrs := otelmetric.WithAttributeSet(otelattribute.NewSet(
			otelattribute.String("grpc.method", ci.method),
			otelattribute.String("grpc.target", ci.target),
		))
		h.metrics.clientMetrics.attemptStarted.Add(ctx, 1, attrs)
	case *stats.OutPayload:
		atomic.AddInt64(&ai.sentCompressedBytes, int64(st.CompressedLength))
	case *stats.InPayload:
		atomic.AddInt64(&ai.recvCompressedBytes, int64(st.CompressedLength))
	case *stats.InHeader:
		h.setLabelsFromPluginOption(ai, st.Header)
	case *stats.InTrailer:
		h.setLabelsFromPluginOption(ai, st.Trailer)
	case *stats.End:
		h.processRPCEnd(ctx, ai, st)
	default:
	}
}

func (h *clientStatsHandler) setLabelsFromPluginOption(ai *attemptInfo, incomingMetadata metadata.MD) {
	if ai.pluginOptionLabels == nil && h.options.MetricsOptions.pluginOption != nil {
		labels := h.options.MetricsOptions.pluginOption.GetLabels(incomingMetadata)
		if labels == nil {
			labels = map[string]string{} // Shouldn't return a nil map. Make it empty if so to ignore future Get Calls for this Attempt.
		}
		ai.pluginOptionLabels = labels
	}
}

func (h *clientStatsHandler) processRPCEnd(ctx context.Context, ai *attemptInfo, e *stats.End) {
	ci := getCallInfo(ctx)
	if ci == nil {
		logger.Error("ctx passed into client side stats handler metrics event handling has no metrics data present")
		return
	}
	latency := float64(time.Since(ai.startTime)) / float64(time.Second)
	st := "OK"
	if e.Error != nil {
		s, _ := status.FromError(e.Error)
		st = canonicalString(s.Code())
	}

	attributes := []otelattribute.KeyValue{
		otelattribute.String("grpc.method", ci.method),
		otelattribute.String("grpc.target", ci.target),
		otelattribute.String("grpc.status", st),
	}

	for k, v := range ai.pluginOptionLabels {
		attributes = append(attributes, otelattribute.String(k, v))
	}

	for _, o := range h.options.MetricsOptions.OptionalLabels {
		// TODO: Add a filter for converting to unknown if not present in the
		// CSM Plugin Option layer by adding an optional labels API.
		if val, ok := ai.xdsLabels[o]; ok {
			attributes = append(attributes, otelattribute.String(o, val))
		}
	}

	// Allocate vararg slice once.
	opts := []otelmetric.RecordOption{otelmetric.WithAttributeSet(otelattribute.NewSet(attributes...))}
	h.metrics.clientMetrics.attemptDuration.Record(ctx, latency, opts...)
	h.metrics.clientMetrics.attemptSentTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&ai.sentCompressedBytes), opts...)
	h.metrics.clientMetrics.attemptRcvdTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&ai.recvCompressedBytes), opts...)
}

const (
	// ClientAttemptStartedMetricName is the number of client call attempts
	// started.
	ClientAttemptStartedMetricName string = "grpc.client.attempt.started"
	// ClientAttemptDurationMetricName is the end-to-end time taken to complete
	// a client call attempt.
	ClientAttemptDurationMetricName string = "grpc.client.attempt.duration"
	// ClientAttemptSentCompressedTotalMessageSizeMetricName is the compressed
	// message bytes sent per client call attempt.
	ClientAttemptSentCompressedTotalMessageSizeMetricName string = "grpc.client.attempt.sent_total_compressed_message_size"
	// ClientAttemptRcvdCompressedTotalMessageSizeMetricName is the compressed
	// message bytes received per call attempt.
	ClientAttemptRcvdCompressedTotalMessageSizeMetricName string = "grpc.client.attempt.rcvd_total_compressed_message_size"
	// ClientCallDurationMetricName is the time taken by gRPC to complete an RPC
	// from application's perspective.
	ClientCallDurationMetricName string = "grpc.client.call.duration"
)
