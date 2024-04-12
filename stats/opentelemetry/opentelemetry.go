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

// Package opentelemetry implements opentelemetry instrumentation code for
// gRPC-Go clients and servers.
package opentelemetry

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"

	"go.opentelemetry.io/otel/metric"
)

var logger = grpclog.Component("otel-plugin")

var canonicalString = internal.CanonicalString.(func(codes.Code) string)

var joinDialOptions = internal.JoinDialOptions.(func(...grpc.DialOption) grpc.DialOption)

// EmptyMetrics represents no metrics. To start from a clean slate if the
// intended effect is to pick a subset of metrics, use this and add onto it.
var EmptyMetrics = Metrics{}

// MetricName is a name of a metric.
type MetricName string

// Metrics is a set of metrics to record. Once created, Metrics is immutable,
// however Add and Remove can make copies with specific metrics added or
// removed, respectively.
type Metrics struct {
	// metrics are the set of metrics to initialize.
	metrics map[MetricName]bool
}

// Add adds the metrics to the metrics set and returns a new copy with the
// additional metrics.
func (m *Metrics) Add(metrics ...MetricName) *Metrics {
	newMetrics := make(map[MetricName]bool)
	for metric := range m.metrics {
		newMetrics[metric] = true
	}

	for _, metric := range metrics {
		newMetrics[metric] = true
	}
	return &Metrics{
		metrics: newMetrics,
	}
}

// Remove removes the metrics from the metrics set and returns a new copy with
// the metrics removed.
func (m *Metrics) Remove(metrics ...MetricName) *Metrics {
	newMetrics := make(map[MetricName]bool)
	for metric := range m.metrics {
		newMetrics[metric] = true
	}

	for _, metric := range metrics {
		delete(newMetrics, metric)
	}
	return &Metrics{
		metrics: newMetrics,
	}
}

// MetricsOptions are the metrics options for OpenTelemetry instrumentation.
type MetricsOptions struct {
	// MeterProvider is the MeterProvider instance that will be used for access
	// to Named Meter instances to instrument an application. To enable metrics
	// collection, set a meter provider. If unset, no metrics will be recorded.
	// Any implementation knobs (i.e. views, bounds) set in the passed in object
	// take precedence over the API calls from the interface in this component
	// (i.e. it will create default views for unset views).
	MeterProvider metric.MeterProvider
	// Metrics are the metrics to instrument. Will turn on the corresponding
	// metric supported by the client and server instrumentation components if
	// applicable.
	Metrics *Metrics
	// TargetAttributeFilter is a callback that takes the target string of the
	// channel and returns a bool representing whether to use target as a label
	// value or use the string "other". If unset, will use the target string as
	// is. This only applies for client side metrics.
	TargetAttributeFilter func(string) bool

	// MethodAttributeFilter is to record the method name of RPCs handled by
	// grpc.UnknownServiceHandler, but take care to limit the values allowed, as
	// allowing too many will increase cardinality and could cause severe memory
	// or performance problems. On Client Side, pass a
	// grpc.StaticMethodCallOption as a call option into Invoke or NewStream.
	// This only applies for server side metrics.
	MethodAttributeFilter func(string) bool
}

// DialOption returns a dial option which enables OpenTelemetry instrumentation
// code for a grpc.ClientConn.
//
// Client applications interested in instrumenting their grpc.ClientConn should
// pass the dial option returned from this function as a dial option to
// grpc.NewClient().
//
// For the metrics supported by this instrumentation code, specify the client
// metrics to record in metrics options. Also provide an implementation of a
// MeterProvider. If the passed in Meter Provider does not have the view
// configured for an individual metric turned on, the API call in this component
// will create a default view for that metric.
func DialOption(mo MetricsOptions) grpc.DialOption {
	csh := &clientStatsHandler{mo: mo}
	csh.initializeMetrics()
	return joinDialOptions(grpc.WithChainUnaryInterceptor(csh.unaryInterceptor), grpc.WithChainStreamInterceptor(csh.streamInterceptor), grpc.WithStatsHandler(csh))
}

// ServerOption returns a server option which enables OpenTelemetry
// instrumentation code for a grpc.Server.
//
// Server applications interested in instrumenting their grpc.Server should pass
// the server option returned from this function as an argument to
// grpc.NewServer().
//
// For the metrics supported by this instrumentation code, specify the client
// metrics to record in metrics options. Also provide an implementation of a
// MeterProvider. If the passed in Meter Provider does not have the view
// configured for an individual metric turned on, the API call in this component
// will create a default view for that metric.
func ServerOption(mo MetricsOptions) grpc.ServerOption {
	ssh := &serverStatsHandler{mo: mo}
	ssh.initializeMetrics()
	return grpc.StatsHandler(ssh)
}

// callInfo is information pertaining to the lifespan of the RPC client side.
type callInfo struct {
	target string

	method string
}

type callInfoKey struct{}

func setCallInfo(ctx context.Context, ci *callInfo) context.Context {
	return context.WithValue(ctx, callInfoKey{}, ci)
}

// getCallInfo returns the callInfo stored in the context, or nil
// if there isn't one.
func getCallInfo(ctx context.Context) *callInfo {
	ci, _ := ctx.Value(callInfoKey{}).(*callInfo)
	return ci
}

// rpcInfo is RPC information scoped to the RPC attempt life span client side,
// and the RPC life span server side.
type rpcInfo struct {
	mi *metricsInfo
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

func removeLeadingSlash(mn string) string {
	return strings.TrimLeft(mn, "/")
}

// metricsInfo is RPC information scoped to the RPC attempt life span client
// side, and the RPC life span server side.
type metricsInfo struct {
	// access these counts atomically for hedging in the future:
	// number of bytes after compression (within each message) from side (client
	// || server).
	sentCompressedBytes int64
	// number of compressed bytes received (within each message) received on
	// side (client || server).
	recvCompressedBytes int64

	startTime time.Time
	method    string
	authority string
}

type clientMetrics struct {
	// "grpc.client.attempt.started"
	attemptStarted metric.Int64Counter
	// "grpc.client.attempt.duration"
	attemptDuration metric.Float64Histogram
	// "grpc.client.attempt.sent_total_compressed_message_size"
	attemptSentTotalCompressedMessageSize metric.Int64Histogram
	// "grpc.client.attempt.rcvd_total_compressed_message_size"
	attemptRcvdTotalCompressedMessageSize metric.Int64Histogram

	// "grpc.client.call.duration"
	callDuration metric.Float64Histogram
}

type serverMetrics struct {
	// "grpc.server.call.started"
	callStarted metric.Int64Counter
	// "grpc.server.call.sent_total_compressed_message_size"
	callSentTotalCompressedMessageSize metric.Int64Histogram
	// "grpc.server.call.rcvd_total_compressed_message_size"
	callRcvdTotalCompressedMessageSize metric.Int64Histogram
	// "grpc.server.call.duration"
	callDuration metric.Float64Histogram
}

func createInt64Counter(setOfMetrics map[MetricName]bool, metricName MetricName, meter metric.Meter, options ...metric.Int64CounterOption) metric.Int64Counter {
	if _, ok := setOfMetrics[metricName]; !ok {
		return nil
	}
	ret, err := meter.Int64Counter(string(metricName), options...)
	if err != nil {
		logger.Errorf("failed to register metric \"%v\", will not record", metricName)
		return nil
	}
	return ret
}

func createInt64Histogram(setOfMetrics map[MetricName]bool, metricName MetricName, meter metric.Meter, options ...metric.Int64HistogramOption) metric.Int64Histogram {
	if _, ok := setOfMetrics[metricName]; !ok {
		return nil
	}
	ret, err := meter.Int64Histogram(string(metricName), options...)
	if err != nil {
		logger.Errorf("failed to register metric \"%v\", will not record", metricName)
		return nil
	}
	return ret
}

func createFloat64Histogram(setOfMetrics map[MetricName]bool, metricName MetricName, meter metric.Meter, options ...metric.Float64HistogramOption) metric.Float64Histogram {
	if _, ok := setOfMetrics[metricName]; !ok {
		return nil
	}
	ret, err := meter.Float64Histogram(string(metricName), options...)
	if err != nil {
		logger.Errorf("failed to register metric \"%v\", will not record", metricName)
		return nil
	}
	return ret
}

// Users of this component should use these bucket boundaries as part of their
// SDK MeterProvider passed in. This component sends this as "advice" to the
// API, which works, however this stability is not guaranteed, so for safety the
// SDK Meter Provider should set these bounds.
var (
	// DefaultLatencyBounds are the default bounds for latency metrics. Users of
	// this component should set these bucket boundaries as part of their SDK
	// MeterProvider passed in for desired latency metrics.
	DefaultLatencyBounds = []float64{0, 0.00001, 0.00005, 0.0001, 0.0003, 0.0006, 0.0008, 0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.008, 0.01, 0.013, 0.016, 0.02, 0.025, 0.03, 0.04, 0.05, 0.065, 0.08, 0.1, 0.13, 0.16, 0.2, 0.25, 0.3, 0.4, 0.5, 0.65, 0.8, 1, 2, 5, 10, 20, 50, 100} // provide "advice" through API, SDK should set this too
	// DefaultSizeBounds are the default bounds for metrics which record size.
	// Users of this component should set these bucket boundaries as part of
	// their SDK MeterProvider passed in for desired size metrics.
	DefaultSizeBounds = []float64{0, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296}
)