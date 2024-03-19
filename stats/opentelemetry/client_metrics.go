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
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type clientStatsHandler struct {
	mo MetricsOptions

	registeredMetrics registeredMetrics
}

func (csh *clientStatsHandler) initializeMetrics() {
	// Will set no metrics to record, logically making this stats handler a
	// no-op.
	if csh.mo.MeterProvider == nil {
		return
	}

	meter := csh.mo.MeterProvider.Meter("gRPC-Go")
	if meter == nil {
		return
	}

	setOfMetrics := csh.mo.Metrics.metrics

	registeredMetrics := registeredMetrics{}

	if _, ok := setOfMetrics["grpc.client.attempt.started"]; ok {
		asc, err := meter.Int64Counter("grpc.client.attempt.started", metric.WithUnit("attempt"), metric.WithDescription("The total number of RPC attempts started, including those that have not completed."))
		if err != nil {
			logger.Errorf("failed to register metric \"grpc.client.attempt.started\", will not record")
		} else {
			registeredMetrics.clientAttemptStarted = asc
		}
	}

	if _, ok := setOfMetrics["grpc.client.attempt.duration"]; ok {
		cad, err := meter.Float64Histogram("grpc.client.attempt.duration", metric.WithUnit("s"), metric.WithDescription("End-to-end time taken to complete an RPC attempt including the time it takes to pick a subchannel."), metric.WithExplicitBucketBoundaries(DefaultLatencyBounds...))
		if err != nil {
			logger.Errorf("failed to register metric \"grpc.client.attempt.started\", will not record")
		} else {
			registeredMetrics.clientAttemptDuration = cad
		}
	}

	if _, ok := setOfMetrics["grpc.client.attempt.sent_total_compressed_message_size"]; ok {
		cas, err := meter.Int64Histogram("grpc.client.attempt.sent_total_compressed_message_size", metric.WithUnit("By"), metric.WithDescription("Total bytes (compressed but not encrypted) sent across all request messages (metadata excluded) per RPC attempt; does not include grpc or transport framing bytes."), metric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
		if err != nil {
			logger.Errorf("failed to register metric \"grpc.client.attempt.sent_total_compressed_message_size\", will not record")
		} else {
			registeredMetrics.clientAttemptSentTotalCompressedMessageSize = cas
		}
	}

	if _, ok := setOfMetrics["grpc.client.attempt.rcvd_total_compressed_message_size"]; ok {
		car, err := meter.Int64Histogram("grpc.client.attempt.rcvd_total_compressed_message_size", metric.WithUnit("By"), metric.WithDescription("Total bytes (compressed but not encrypted) received across all response messages (metadata excluded) per RPC attempt; does not include grpc or transport framing bytes."), metric.WithExplicitBucketBoundaries(DefaultSizeBounds...))
		if err != nil {
			logger.Errorf("failed to register metric \"grpc.client.rcvd.sent_total_compressed_message_size\", will not record")
		} else {
			registeredMetrics.clientAttemptRcvdTotalCompressedMessageSize = car
		}
	}

	if _, ok := setOfMetrics["grpc.client.call.duration"]; ok {
		ccs, err := meter.Float64Histogram("grpc.client.call.duration", metric.WithUnit("s"), metric.WithDescription("This metric aims to measure the end-to-end time the gRPC library takes to complete an RPC from the application’s perspective."), metric.WithExplicitBucketBoundaries(DefaultLatencyBounds...))
		if err != nil {
			logger.Errorf("failed to register metric \"grpc.client.call.duration\", will not record")
		} else {
			registeredMetrics.clientCallDuration = ccs
		}
	}
	csh.registeredMetrics = registeredMetrics
}

func (csh *clientStatsHandler) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ci := &callInfo{
		target: csh.determineTarget(cc),
		method: csh.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)

	startTime := time.Now()
	err := invoker(ctx, method, req, reply, cc, opts...)
	csh.perCallMetrics(ctx, err, startTime, ci)
	return err
}

// determineTarget determines the target to record attributes with. This will be
// "other" if target filter is set and specifies, the target name as is
// otherwise.
func (csh *clientStatsHandler) determineTarget(cc *grpc.ClientConn) string {
	target := cc.CanonicalTarget()
	if csh.mo.TargetAttributeFilter != nil {
		if !csh.mo.TargetAttributeFilter(target) {
			target = "other"
		}
	}
	return target
}

// determineMethod determines the method to record attributes with. This will be
// "other" if StaticMethod isn't specified or if method filter is set and
// specifies, the method name as is otherwise.
func (csh *clientStatsHandler) determineMethod(method string, opts ...grpc.CallOption) string {
	if csh.mo.MethodAttributeFilter != nil {
		if !csh.mo.MethodAttributeFilter(method) {
			return "other"
		}
	}
	for _, opt := range opts {
		if _, ok := opt.(grpc.StaticMethodCallOption); ok {
			return method
		}
	}
	return "other"
}

func (csh *clientStatsHandler) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ci := &callInfo{
		target: csh.determineTarget(cc),
		method: csh.determineMethod(method, opts...),
	}
	ctx = setCallInfo(ctx, ci)
	startTime := time.Now()

	callback := func(err error) {
		csh.perCallMetrics(ctx, err, startTime, ci)
	}
	opts = append([]grpc.CallOption{grpc.OnFinish(callback)}, opts...)
	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (csh *clientStatsHandler) perCallMetrics(ctx context.Context, err error, startTime time.Time, ci *callInfo) {
	s := status.Convert(err)
	callLatency := float64(time.Since(startTime)) / float64(time.Second)
	if csh.registeredMetrics.clientCallDuration != nil {
		csh.registeredMetrics.clientCallDuration.Record(ctx, callLatency, metric.WithAttributes(attribute.String("grpc.method", removeLeadingSlash(ci.method)), attribute.String("grpc.target", ci.target), attribute.String("grpc.status", canonicalString(s.Code())))) // needs method target and status should I persist this?
	}
}

// TagConn exists to satisfy stats.Handler.
func (csh *clientStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (csh *clientStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TagRPC implements per RPC attempt context management.
func (csh *clientStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	mi := &metricsInfo{ // populates information about RPC start.
		startTime: time.Now(),
	}
	ri := &rpcInfo{
		mi: mi,
	}
	return setRPCInfo(ctx, ri)
}

func (csh *clientStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	ri := getRPCInfo(ctx)
	if ri == nil {
		// Shouldn't happen because TagRPC populates this information.
		return
	}
	csh.processRPCEvent(ctx, rs, ri.mi)
}

func (csh *clientStatsHandler) processRPCEvent(ctx context.Context, s stats.RPCStats, mi *metricsInfo) {
	switch st := s.(type) {
	case *stats.InHeader, *stats.OutHeader, *stats.InTrailer, *stats.OutTrailer:
	case *stats.Begin:
		ci := getCallInfo(ctx)
		if ci == nil {
			// Shouldn't happen, set by interceptor, defensive programming. Log it won't record?
			return
		}

		if csh.registeredMetrics.clientAttemptStarted != nil {
			csh.registeredMetrics.clientAttemptStarted.Add(ctx, 1, metric.WithAttributes(attribute.String("grpc.method", removeLeadingSlash(ci.method)), attribute.String("grpc.target", ci.target))) // Add records a change to the counter...attributeset for efficiency
		}
	case *stats.OutPayload:
		atomic.AddInt64(&mi.sentCompressedBytes, int64(st.CompressedLength))
	case *stats.InPayload:
		atomic.AddInt64(&mi.recvCompressedBytes, int64(st.CompressedLength))
	case *stats.End:
		csh.processRPCEnd(ctx, mi, st)
	default:
		// Shouldn't happen. gRPC calls into stats handler, and will never not
		// be one of the types above.
		logger.Errorf("Received unexpected stats type (%T) with data: %v", s, s)
	}
}

func (csh *clientStatsHandler) processRPCEnd(ctx context.Context, mi *metricsInfo, e *stats.End) {
	ci := getCallInfo(ctx)
	if ci == nil {
		// Shouldn't happen, set by interceptor, defensive programming.
		return
	}
	latency := float64(time.Since(mi.startTime)) / float64(time.Second)
	var st string
	if e.Error != nil {
		s, _ := status.FromError(e.Error)
		st = canonicalString(s.Code())
	} else {
		st = "OK"
	}
	clientAttributeOption := metric.WithAttributes(attribute.String("grpc.method", removeLeadingSlash(ci.method)), attribute.String("grpc.target", ci.target), attribute.String("grpc.status", st))
	if csh.registeredMetrics.clientAttemptDuration != nil {
		csh.registeredMetrics.clientAttemptDuration.Record(ctx, latency, clientAttributeOption)
	}

	if csh.registeredMetrics.clientAttemptSentTotalCompressedMessageSize != nil {
		csh.registeredMetrics.clientAttemptSentTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&mi.sentCompressedBytes), clientAttributeOption)
	}

	if csh.registeredMetrics.clientAttemptRcvdTotalCompressedMessageSize != nil {
		csh.registeredMetrics.clientAttemptRcvdTotalCompressedMessageSize.Record(ctx, atomic.LoadInt64(&mi.recvCompressedBytes), clientAttributeOption)
	}
}

// DefaultClientMetrics are the default client metrics provided by this module.
var DefaultClientMetrics = Metrics{
	metrics: map[string]bool{
		"grpc.client.attempt.started":                            true,
		"grpc.client.attempt.duration":                           true,
		"grpc.client.attempt.sent_total_compressed_message_size": true,
		"grpc.client.attempt.rcvd_total_compressed_message_size": true,
		"grpc.client.call.duration":                              true,
	},
}
