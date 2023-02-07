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

	"go.opencensus.io/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats"
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
	return joinDialOptions(grpc.WithChainUnaryInterceptor(unaryInterceptor), grpc.WithChainStreamInterceptor(streamInterceptor), grpc.WithStatsHandler(&clientStatsHandler{to: to}))
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

// unaryInterceptor handles per RPC context management. It also handles per RPC
// tracing and stats.
func unaryInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(ctx, method, req, reply, cc, opts...)
}

// streamInterceptor handles per RPC context management. It also handles per RPC
// tracing and stats.
func streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(ctx, desc, cc, method, opts...)
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
	ctx = csh.statsTagRPC(ctx, rti)
	return ctx
}

func (csh *clientStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	recordRPCData(ctx, rs)
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
	ctx = ssh.statsTagRPC(ctx, rti)
	return ctx
}

// HandleRPC implements per RPC tracing and stats implementation.
func (ssh *serverStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	recordRPCData(ctx, rs)
}
