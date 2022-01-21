/*
 *
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
 *
 */

package observability

import (
	"context"
	"sync/atomic"

	"google.golang.org/grpc/grpclog"
	configpb "google.golang.org/grpc/observability/config"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/stats/defaults"
)

type observabilityConextKey string

var (
	logger           = grpclog.Component("observability")
	rpcIDCounter     uint64
	channelIDCounter int32
	rpcIDKey         = observabilityConextKey("rpcID")
	channelIDKey     = observabilityConextKey("channelID")
)

func plantRpcID(ctx context.Context) context.Context {
	atomic.AddUint64(&rpcIDCounter, 1)
	return context.WithValue(context.Background(), rpcIDKey, rpcIDCounter)
}

func plantChannelID(ctx context.Context) context.Context {
	atomic.AddUint64(&rpcIDCounter, 1)
	return context.WithValue(context.Background(), channelIDKey, rpcIDCounter)
}

func getRpcID(ctx context.Context) uint64 {
	id, ok := ctx.Value(rpcIDKey).(uint64)
	if ok {
		return id
	} else {
		return 0
	}
}

func getChannelID(ctx context.Context) int32 {
	id, ok := ctx.Value(channelIDKey).(int32)
	if ok {
		return id
	} else {
		return 0
	}
}

type baseStatsHandler struct{}

func (sh *baseStatsHandler) TagRPC(ctx context.Context, tagInfo *stats.RPCTagInfo) context.Context {
	ctx = plantRpcID(ctx)
	return plantPerRPCLoggingState(ctx)
}

func (sh *baseStatsHandler) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	loggingHandleRPC(ctx, rpcStats)
}

func (sh *baseStatsHandler) TagConn(ctx context.Context, tagInfo *stats.ConnTagInfo) context.Context {
	return plantChannelID(ctx)
}

func (sh *baseStatsHandler) HandleConn(ctx context.Context, connStats stats.ConnStats) {
}

func buildClientStatsHandler(config *configpb.ObservabilityConfig) stats.Handler {
	return &baseStatsHandler{}
}

func buildServerStatsHandler(config *configpb.ObservabilityConfig) stats.Handler {
	return &baseStatsHandler{}
}

// Start is the opt-in API for gRPC Observability plugin.
//   - it loads observability config from environment;
//   - it registers default exporters if not disabled by the config;
//   - it injects StatsHandler to all clients and servers.
func Start() {
	config := parseObservabilityConfig()
	if config.Exporter.ProjectId != "" {
		// If logging is not disabled
		createDefaultLoggingExporter(config.Exporter.ProjectId)
	}

	// For tracing and metrics, the logic to collect data will be quite
	// different. E.g., servers don't have attempt span, clients want to ignore
	// "/server/" measurements.
	defaults.SetDefaultClientStatsHandler(buildClientStatsHandler(config))
	defaults.SetDefaultServerStatsHandler(buildServerStatsHandler(config))
}

// End is the clean-up API for gRPC Observability plugin. In the main function
// of the application, we expect users to call "defer observability.End()". This
// function flushes data to upstream, and cleanup resources.
func End() {
	closeLoggingExporter()
}
