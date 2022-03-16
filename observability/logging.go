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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	binlogpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	iblog "google.golang.org/grpc/internal/binarylog"
	"google.golang.org/grpc/metadata"
	configpb "google.golang.org/grpc/observability/internal/config"
	grpclogrecordpb "google.golang.org/grpc/observability/internal/logging"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func setMetadataToGrpcLogRecord(lr *grpclogrecordpb.GrpcLogRecord, m metadata.MD, mlc *iblog.MethodLoggerConfig) {
	mdPb := iblog.MdToMetadataProto(m)
	lr.PayloadTruncated = iblog.TruncateMetadata(mlc, mdPb)
	lr.Metadata = &grpclogrecordpb.GrpcLogRecord_Metadata{
		Entry: make([]*grpclogrecordpb.GrpcLogRecord_MetadataEntry, len(mdPb.GetEntry())),
	}
	for i, e := range mdPb.GetEntry() {
		lr.Metadata.Entry[i] = &grpclogrecordpb.GrpcLogRecord_MetadataEntry{
			Key:   e.Key,
			Value: e.Value,
		}
	}
}

func setPeerToGrpcLogRecord(lr *grpclogrecordpb.GrpcLogRecord, peer net.Addr) {
	if peer != nil {
		peerPb := iblog.AddrToProto(peer)
		lr.PeerAddress = &grpclogrecordpb.GrpcLogRecord_Address{
			Type:    grpclogrecordpb.GrpcLogRecord_Address_Type(peerPb.GetType()),
			Address: peerPb.GetAddress(),
			IpPort:  peerPb.GetIpPort(),
		}
	}
}

func setMessageToGrpcLogRecord(lr *grpclogrecordpb.GrpcLogRecord, msg interface{}, mlc *iblog.MethodLoggerConfig) {
	var (
		data []byte
		err  error
	)
	if m, ok := msg.(proto.Message); ok {
		data, err = proto.Marshal(m)
		if err != nil {
			logger.Infof("failed to marshal proto message: %v", err)
		}
	} else if b, ok := msg.([]byte); ok {
		data = b
	} else {
		logger.Infof("message to log is neither proto.message nor []byte")
	}
	msgPb := &binlogpb.Message{
		Length: uint32(len(data)),
		Data:   data,
	}
	lr.PayloadTruncated = iblog.TruncateMessage(mlc, msgPb)
	lr.Message = msgPb.Data
	lr.PayloadSize = msgPb.Length
}

func getEventLogger(isClient bool) grpclogrecordpb.GrpcLogRecord_EventLogger {
	if isClient {
		return grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT
	}
	return grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER
}

type observabilityBinaryMethodLogger struct {
	rpcID, serviceName, methodName string
	sequenceIDGen                  iblog.CallIDGenerator
	originalMethodLogger           iblog.MethodLogger
	methodLoggerConfig             *iblog.MethodLoggerConfig
}

func (ml *observabilityBinaryMethodLogger) Log(c iblog.LogEntryConfig) {
	// Invoke the original MethodLogger to maintain backward compatibility
	if ml.originalMethodLogger != nil {
		ml.originalMethodLogger.Log(c)
	}

	timestamp, err := ptypes.TimestampProto(time.Now())
	if err != nil {
		logger.Errorf("Failed to convert time.Now() to TimestampProto: %v", err)
	}
	grpcLogRecord := &grpclogrecordpb.GrpcLogRecord{
		Timestamp:  timestamp,
		RpcId:      ml.rpcID,
		SequenceId: ml.sequenceIDGen.Next(),
		// Making DEBUG the default LogLevel
		LogLevel: grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_DEBUG,
	}
	switch v := c.(type) {
	case *iblog.ClientHeader:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_HEADER
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
		methodName := v.MethodName
		if strings.Contains(methodName, "/") {
			tokens := strings.Split(methodName, "/")
			if len(tokens) == 3 {
				// Example method name: /grpc.testing.TestService/UnaryCall
				ml.serviceName = tokens[1]
				ml.methodName = tokens[2]
			} else if len(tokens) == 2 {
				// Example method name: grpc.testing.TestService/UnaryCall
				ml.serviceName = tokens[0]
				ml.methodName = tokens[1]
			} else {
				logger.Errorf("Malformed method name: %v", methodName)
			}
		}
		grpcLogRecord.Timeout = ptypes.DurationProto(v.Timeout)
		grpcLogRecord.Authority = v.Authority
		setMetadataToGrpcLogRecord(grpcLogRecord, v.Header, ml.methodLoggerConfig)
		setPeerToGrpcLogRecord(grpcLogRecord, v.PeerAddr)
	case *iblog.ServerHeader:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_HEADER
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
		setMetadataToGrpcLogRecord(grpcLogRecord, v.Header, ml.methodLoggerConfig)
		setPeerToGrpcLogRecord(grpcLogRecord, v.PeerAddr)
	case *iblog.ClientMessage:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_MESSAGE
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
		setMessageToGrpcLogRecord(grpcLogRecord, v.Message, ml.methodLoggerConfig)
	case *iblog.ServerMessage:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_MESSAGE
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
		setMessageToGrpcLogRecord(grpcLogRecord, v.Message, ml.methodLoggerConfig)
	case *iblog.ClientHalfClose:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_HALF_CLOSE
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
	case *iblog.ServerTrailer:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_TRAILER
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
		setMetadataToGrpcLogRecord(grpcLogRecord, v.Trailer, ml.methodLoggerConfig)
		setPeerToGrpcLogRecord(grpcLogRecord, v.PeerAddr)
		st, ok := status.FromError(v.Err)
		if !ok {
			logger.Info("Error in trailer is not a status error")
		}
		stProto := st.Proto()
		if stProto != nil && len(stProto.Details) != 0 {
			detailsBytes, err := proto.Marshal(stProto)
			if err != nil {
				logger.Infof("Failed to marshal status proto: %v", err)
			} else {
				grpcLogRecord.StatusDetails = detailsBytes
			}
		}
		grpcLogRecord.StatusCode = uint32(st.Code())
		grpcLogRecord.StatusMessage = st.Message()
	case *iblog.Cancel:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_CANCEL
		grpcLogRecord.EventLogger = getEventLogger(v.OnClientSide)
	default:
		logger.Errorf("Unexpected LogEntryConfig: %+v", c)
		return
	}

	if ml.serviceName != "" {
		grpcLogRecord.ServiceName = ml.serviceName
	}
	if ml.methodName != "" {
		grpcLogRecord.MethodName = ml.methodName
	}

	// CloudLogging client doesn't return error on entry write. Entry writes
	// don't mean the data will be uploaded immediately.
	globalLoggingExporter.EmitGrpcLogRecord(grpcLogRecord)
}

type observabilityBinaryLogger struct {
	// originalLogger is needed to ensure binary logging users won't be impacted
	// by this plugin. Users are allowed to subscribe to a completely different
	// set of methods.
	originalLogger iblog.Logger
	// wrappedLogger is needed to reuse the control string parsing, logger
	// config managing logic
	wrappedLogger iblog.Logger
}

func (l *observabilityBinaryLogger) GetMethodLoggerConfig(methodName string) *iblog.MethodLoggerConfig {
	if l.wrappedLogger == nil {
		return nil
	}
	return l.wrappedLogger.GetMethodLoggerConfig(methodName)
}

func (l *observabilityBinaryLogger) GetMethodLogger(methodName string) iblog.MethodLogger {
	var ol iblog.MethodLogger
	if l.originalLogger != nil {
		ol = l.originalLogger.GetMethodLogger(methodName)
	}

	mlc := l.GetMethodLoggerConfig(methodName)
	if mlc == nil {
		return ol
	}
	return &observabilityBinaryMethodLogger{
		originalMethodLogger: ol,
		methodLoggerConfig:   mlc,
		rpcID:                uuid.NewString(),
	}
}

func newObservabilityBinaryLogger(iblogger iblog.Logger) *observabilityBinaryLogger {
	return &observabilityBinaryLogger{
		originalLogger: iblogger,
	}
}

func compileBinaryLogControlString(config *configpb.ObservabilityConfig) string {
	if len(config.LogFilters) == 0 {
		return ""
	}

	entries := make([]string, 0, len(config.LogFilters)+1)
	for _, logFilter := range config.LogFilters {
		// With undefined HeaderBytes or MessageBytes, the intended behavior is
		// logging zero payload. This detail is different than binary logging.
		entries = append(entries, fmt.Sprintf("%v{h:%v;m:%v}", logFilter.Pattern, logFilter.HeaderBytes, logFilter.MessageBytes))
	}
	// If user specify a "*" pattern, binarylog will log every single call and
	// content. This means the exporting RPC's events will be captured. Even if
	// we batch up the uploads in the exporting RPC, the message content of that
	// RPC will be logged. Without this exclusion, we may end up with an ever
	// expanding message field in log entries, and crash the process with OOM.
	entries = append(entries, "-google.logging.v2.LoggingServiceV2/WriteLogEntries")
	return strings.Join(entries, ",")
}

var defaultLogger *observabilityBinaryLogger

func prepareLogging() {
	defaultLogger = newObservabilityBinaryLogger(iblog.GetLogger())
	iblog.SetLogger(defaultLogger)
}

func startLogging(config *configpb.ObservabilityConfig) {
	if config == nil {
		return
	}
	binlogConfig := compileBinaryLogControlString(config)
	defaultLogger.wrappedLogger = iblog.NewLoggerFromConfigString(binlogConfig)
	logger.Infof("Start logging with config [%v]", binlogConfig)
}
