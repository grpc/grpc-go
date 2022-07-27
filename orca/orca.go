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
// Experimental
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
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

var logger = grpclog.Component("orca-backend-metrics")

// CallMetricsServerOption retuns a server option which enables the reporting of
// per-RPC custom backend metrics for unary RPCs.
//
// Server applications interested in injecting custom backend metrics should
// pass the server option returned from this function as the first argument to
// grpc.NewServer().
//
// Subsequently, server RPC handlers can retrieve a reference to the RPC
// specific custom metrics recorder [CallMetricRecorder] to be used, via a call
// to GetCallMetricRecorder(), and inject custom metrics at any time during the
// RPC lifecycle.
//
// The injected custom metrics will be sent as part of trailer metadata, as a
// binary-encoded [ORCA LoadReport] protobuf message, with the metadata key
// being set be "endpoint-load-metrics-bin".
//
// [ORCA LoadReport]: (https://github.com/cncf/xds/blob/main/xds/data/orca/v3/orca_load_report.proto#L15)
func CallMetricsServerOption() grpc.ServerOption {
	unaryInt := func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		r := &CallMetricRecorder{recorder: newMetricRecorder()}
		ctxWithRecorder := setCallMetricRecorder(ctx, r)
		resp, err := handler(ctxWithRecorder, req)
		if err2 := r.recorder.setTrailerMetadata(ctx); err2 != nil {
			logger.Warning(err2)
		}
		return resp, err
	}
	streamInt := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		r := &CallMetricRecorder{recorder: newMetricRecorder()}
		ctxWithRecorder := setCallMetricRecorder(ss.Context(), r)
		err := handler(srv, &wrappedStream{
			ServerStream: ss,
			ctx:          ctxWithRecorder,
		})
		if err2 := r.recorder.setTrailerMetadata(ss.Context()); err2 != nil {
			logger.Warning(err2)
		}
		return err
	}
	return internal.JoinServerOptions.(func(...grpc.ServerOption) grpc.ServerOption)(grpc.ChainUnaryInterceptor(unaryInt), grpc.ChainStreamInterceptor(streamInt))
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

// ToLoadReport unmarshals a binary encoded [ORCA LoadReport] protobuf message
// from md and returns the corresponding struct. The load report is expected to
// be stored as the value for key "endpoint-load-metrics-bin".
//
// [ORCA LoadReport]: (https://github.com/cncf/xds/blob/main/xds/data/orca/v3/orca_load_report.proto#L15)
func ToLoadReport(md metadata.MD) (*v3orcapb.OrcaLoadReport, error) {
	vs := md.Get(trailerMetadataKey)
	if len(vs) == 0 {
		return nil, errors.New("orca load report missing in provided metadata")
	}
	ret := new(v3orcapb.OrcaLoadReport)
	if err := proto.Unmarshal([]byte(vs[0]), ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal load report found in metadata: %v", err)
	}
	return ret, nil
}
