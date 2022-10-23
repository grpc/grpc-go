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

// Package orca implements Open Request Cost Aggregation, which is an open
// standard for request cost aggregation and reporting by backends and the
// corresponding aggregation of such reports by L7 load balancers (such as
// Envoy) on the data plane. In a proxyless world with gRPC enabled
// applications, aggregation of such reports will be done by the gRPC client.
//
// # Experimental
//
// Notice: All APIs is this package are EXPERIMENTAL and may be changed or
// removed in a later release.
package orca

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancerload"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

var (
	logger            = grpclog.Component("orca-backend-metrics")
	joinServerOptions = internal.JoinServerOptions.(func(...grpc.ServerOption) grpc.ServerOption)
)

const trailerMetadataKey = "endpoint-load-metrics-bin"

// CallMetricsServerOption returns a server option which enables the reporting
// of per-RPC custom backend metrics for unary and streaming RPCs.
//
// Server applications interested in injecting custom backend metrics should
// pass the server option returned from this function as the first argument to
// grpc.NewServer().
//
// Subsequently, server RPC handlers can retrieve a reference to the RPC
// specific custom metrics recorder [CallMetricRecorder] to be used, via a call
// to CallMetricRecorderFromContext(), and inject custom metrics at any time
// during the RPC lifecycle.
//
// The injected custom metrics will be sent as part of trailer metadata, as a
// binary-encoded [ORCA LoadReport] protobuf message, with the metadata key
// being set be "endpoint-load-metrics-bin".
//
// [ORCA LoadReport]: https://github.com/cncf/xds/blob/main/xds/data/orca/v3/orca_load_report.proto#L15
func CallMetricsServerOption() grpc.ServerOption {
	return joinServerOptions(grpc.ChainUnaryInterceptor(unaryInt), grpc.ChainStreamInterceptor(streamInt))
}

func unaryInt(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// We don't allocate the metric recorder here. It will be allocated the
	// first time the user calls CallMetricRecorderFromContext().
	rw := &recorderWrapper{}
	ctxWithRecorder := newContextWithRecorderWrapper(ctx, rw)

	resp, err := handler(ctxWithRecorder, req)

	// It is safe to access the underlying metric recorder inside the wrapper at
	// this point, as the user's RPC handler is done executing, and therefore
	// there will be no more calls to CallMetricRecorderFromContext(), which is
	// where the metric recorder is lazy allocated.
	if rw.r == nil {
		return resp, err
	}
	setTrailerMetadata(ctx, rw.r)
	return resp, err
}

func streamInt(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// We don't allocate the metric recorder here. It will be allocated the
	// first time the user calls CallMetricRecorderFromContext().
	rw := &recorderWrapper{}
	ws := &wrappedStream{
		ServerStream: ss,
		ctx:          newContextWithRecorderWrapper(ss.Context(), rw),
	}

	err := handler(srv, ws)

	// It is safe to access the underlying metric recorder inside the wrapper at
	// this point, as the user's RPC handler is done executing, and therefore
	// there will be no more calls to CallMetricRecorderFromContext(), which is
	// where the metric recorder is lazy allocated.
	if rw.r == nil {
		return err
	}
	setTrailerMetadata(ss.Context(), rw.r)
	return err
}

// setTrailerMetadata adds a trailer metadata entry with key being set to
// `trailerMetadataKey` and value being set to the binary-encoded
// orca.OrcaLoadReport protobuf message.
//
// This function is called from the unary and streaming interceptors defined
// above. Any errors encountered here are not propagated to the caller because
// they are ignored there. Hence we simply log any errors encountered here at
// warning level, and return nothing.
func setTrailerMetadata(ctx context.Context, r *CallMetricRecorder) {
	b, err := proto.Marshal(r.toLoadReportProto())
	if err != nil {
		logger.Warningf("failed to marshal load report: %v", err)
		return
	}
	if err := grpc.SetTrailer(ctx, metadata.Pairs(trailerMetadataKey, string(b))); err != nil {
		logger.Warningf("failed to set trailer metadata: %v", err)
	}
}

// wrappedStream wraps the grpc.ServerStream received by the streaming
// interceptor. Overrides only the Context() method to return a context which
// contains a reference to the CallMetricRecorder corresponding to this stream.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

// ErrLoadReportMissing indicates no ORCA load report was found in trailers.
var ErrLoadReportMissing = errors.New("orca load report missing in provided metadata")

// ToLoadReport unmarshals a binary encoded [ORCA LoadReport] protobuf message
// from md and returns the corresponding struct. The load report is expected to
// be stored as the value for key "endpoint-load-metrics-bin".
//
// If no load report was found in the provided metadata, ErrLoadReportMissing is
// returned.
//
// [ORCA LoadReport]: (https://github.com/cncf/xds/blob/main/xds/data/orca/v3/orca_load_report.proto#L15)
func ToLoadReport(md metadata.MD) (*v3orcapb.OrcaLoadReport, error) {
	vs := md.Get(trailerMetadataKey)
	if len(vs) == 0 {
		return nil, ErrLoadReportMissing
	}
	ret := new(v3orcapb.OrcaLoadReport)
	if err := proto.Unmarshal([]byte(vs[0]), ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal load report found in metadata: %v", err)
	}
	return ret, nil
}

// loadParser implements the Parser interface defined in `internal/balancerload`
// package. This interface is used by the client stream to parse load reports
// sent by the server in trailer metadata. The parsed loads are then sent to
// balancers via balancer.DoneInfo.
//
// The grpc package cannot directly call orca.ToLoadReport() as that would cause
// an import cycle. Hence this roundabout method is used.
type loadParser struct{}

func (loadParser) Parse(md metadata.MD) interface{} {
	lr, err := ToLoadReport(md)
	if err != nil {
		logger.Errorf("Parse(%v) failed: %v", err)
	}
	return lr
}

func init() {
	balancerload.SetParser(loadParser{})
}
