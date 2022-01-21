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

	"google.golang.org/grpc/observability/grpclogrecord"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	perRPCLoggingStateKey = observabilityConextKey("perRPCLoggingState")
)

// perRPCLoggingState is a state object for each RPC logging purposes.
type perRPCLoggingState struct {
	// startLog is a special log that requires information across multiple
	// events and used by other log entries
	startLog *grpclogrecord.GrpcLogRecord
}

// newLogRecord creates a GrpcLogRecord with several mandatory fields
func newLogRecord(ctx context.Context, t grpclogrecord.EventType) *grpclogrecord.GrpcLogRecord {
	return &grpclogrecord.GrpcLogRecord{
		EventType: t,
		Timestamp: timestamppb.Now(),
		RpcId:     getRPCID(ctx),
		ChannelId: getChannelID(ctx),
	}
}

func plantPerRPCLoggingState(ctx context.Context) context.Context {
	return context.WithValue(context.Background(), perRPCLoggingStateKey, &perRPCLoggingState{
		startLog: newLogRecord(ctx, grpclogrecord.EventType_GRPC_CALL_START),
	})
}

func getPerRPCLoggingState(ctx context.Context) *perRPCLoggingState {
	p, ok := ctx.Value(perRPCLoggingStateKey).(*perRPCLoggingState)
	if ok {
		return p
	}
	return nil
}

// loggingHandleRPC is the main logic for generating RPC events. Currently, it
// only generates GRPC_CALL_START and GRPC_CALL_END, but we can easily extend to
// other types.
func loggingHandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	state := getPerRPCLoggingState(ctx)
	switch rpcStats.(type) {
	case *stats.Begin:
		b := rpcStats.(*stats.Begin)
		log := state.startLog
		if b.Client {
			log.EventLogger = grpclogrecord.EventLogger_LOGGER_CLIENT
		} else {
			log.EventLogger = grpclogrecord.EventLogger_LOGGER_SERVER
		}
		if !b.IsClientStream && !b.IsServerStream {
			log.MethodType = grpclogrecord.MethodType_METHOD_TYPE_UNARY
		} else if b.IsClientStream && !b.IsServerStream {
			log.MethodType = grpclogrecord.MethodType_METHOD_TYPE_CLIENT_STREAMING
		} else if !b.IsClientStream && b.IsServerStream {
			log.MethodType = grpclogrecord.MethodType_METHOD_TYPE_SERVER_STREAMING
		} else if b.IsClientStream && b.IsServerStream {
			log.MethodType = grpclogrecord.MethodType_METHOD_TYPE_BIDI_STREAMING
		}
	case *stats.InHeader:
		i := rpcStats.(*stats.InHeader)
		if !i.Client {
			log := state.startLog
			log.ClientPublicIp = i.RemoteAddr.String()
			log.ServerPrivateIp = i.LocalAddr.String()
			Emit(log)
		}
	case *stats.OutHeader:
		o := rpcStats.(*stats.OutHeader)
		if o.Client {
			log := state.startLog
			log.ClientPrivateIp = o.LocalAddr.String()
			log.ServerPublicIp = o.RemoteAddr.String()
			Emit(log)
		}
	case *stats.End:
		e := rpcStats.(*stats.End)
		log := newLogRecord(ctx, grpclogrecord.EventType_GRPC_CALL_END)
		if e.Client {
			log.EventLogger = grpclogrecord.EventLogger_LOGGER_CLIENT
		} else {
			log.EventLogger = grpclogrecord.EventLogger_LOGGER_SERVER
		}
		if e.Error == nil {
			// RPC succeeded, code=0, message=OK.
			log.StatusCode = 0
			log.StatusMessage = "OK"
		} else {
			// RPC failed, locate code, message, details
			grpcError, ok := status.FromError(e.Error)
			if !ok {
				logger.Errorf("failed to convert error %v to status", e.Error)
				break
			}
			log.StatusCode = int32(grpcError.Code())
			log.StatusMessage = grpcError.Message()
			if len(grpcError.Proto().Details) > 0 {
				for _, item := range grpcError.Proto().Details {
					content, err := proto.Marshal(item)
					if err != nil {
						logger.Errorf("failed to marshal status detail %v: err", item, err)
						break
					}
					log.StatusDetails = append(log.StatusDetails, content...)
				}
			}
		}
		log.Duration = durationpb.New(e.EndTime.Sub(e.BeginTime))
		Emit(log)
	}
}
