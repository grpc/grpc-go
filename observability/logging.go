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
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/grpc/binarylog"
	binlogpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	iblog "google.golang.org/grpc/internal/binarylog"
	configpb "google.golang.org/grpc/observability/internal/config"
	grpclogrecordpb "google.golang.org/grpc/observability/internal/logging"
)

// translateMetadata translates the metadata from Binary Logging format to
// its GrpcLogRecord equivalent.
func translateMetadata(m *binlogpb.Metadata) *grpclogrecordpb.GrpcLogRecord_Metadata {
	var res grpclogrecordpb.GrpcLogRecord_Metadata
	res.Entry = make([]*grpclogrecordpb.GrpcLogRecord_MetadataEntry, len(m.Entry))
	for i, e := range m.Entry {
		res.Entry[i] = &grpclogrecordpb.GrpcLogRecord_MetadataEntry{
			Key:   e.Key,
			Value: e.Value,
		}
	}
	return &res
}

type cloudLoggingSink struct {
	callIDToUUID map[uint64]string
	lock         sync.RWMutex
	exporter     genericLoggingExporter
}

var defaultCloudLoggingSink *cloudLoggingSink

func (cls *cloudLoggingSink) getUUID(callID uint64) string {
	var (
		u  string
		ok bool
	)
	cls.lock.RLocker().Lock()
	u, ok = cls.callIDToUUID[callID]
	cls.lock.RLocker().Unlock()
	if !ok {
		cls.lock.Lock()
		u = uuid.NewString()
		cls.callIDToUUID[callID] = u
		cls.lock.Unlock()
	}
	return u
}

func (cls *cloudLoggingSink) removeEntry(callID uint64) {
	cls.lock.Lock()
	defer cls.lock.Unlock()
	delete(cls.callIDToUUID, callID)
}

func (cls *cloudLoggingSink) SetExporter(exporter genericLoggingExporter) {
	cls.exporter = exporter
}

// Write translates a Binary Logging log entry to a GrpcLogEntry used by the gRPC
// Observability project and emits it.
func (cls *cloudLoggingSink) Write(binlogEntry *binlogpb.GrpcLogEntry) error {
	if cls.exporter == nil {
		return nil
	}

	var (
		grpcLogRecord grpclogrecordpb.GrpcLogRecord
		callEnded     bool
	)
	grpcLogRecord.Timestamp = binlogEntry.GetTimestamp()
	grpcLogRecord.RpcId = cls.getUUID(binlogEntry.GetCallId())
	grpcLogRecord.SequenceId = binlogEntry.GetSequenceIdWithinCall()
	// Making DEBUG the default LogLevel
	grpcLogRecord.LogLevel = grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_DEBUG
	switch binlogEntry.Type {
	case binlogpb.GrpcLogEntry_EVENT_TYPE_UNKNOWN:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_UNKNOWN
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HEADER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_HEADER
		if binlogEntry.GetClientHeader() != nil {
			methodName := binlogEntry.GetClientHeader().MethodName
			if strings.Contains(methodName, "/") {
				tokens := strings.Split(methodName, "/")
				if len(tokens) == 3 {
					// Example method name: /grpc.testing.TestService/UnaryCall
					grpcLogRecord.ServiceName = tokens[1]
					grpcLogRecord.MethodName = tokens[2]
				} else if len(tokens) == 2 {
					// Example method name: grpc.testing.TestService/UnaryCall
					grpcLogRecord.ServiceName = tokens[0]
					grpcLogRecord.MethodName = tokens[1]
				} else {
					logger.Errorf("malformed method name: %v", methodName)
				}
			}
			grpcLogRecord.Timeout = binlogEntry.GetClientHeader().Timeout
			grpcLogRecord.Authority = binlogEntry.GetClientHeader().Authority
			grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetClientHeader().Metadata)
		}
		if binlogEntry.GetPeer() != nil {
			grpcLogRecord.PeerAddress = &grpclogrecordpb.GrpcLogRecord_Address{
				Type:    grpclogrecordpb.GrpcLogRecord_Address_Type(binlogEntry.Peer.Type),
				Address: binlogEntry.Peer.Address,
				IpPort:  binlogEntry.Peer.IpPort,
			}
		}
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_HEADER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_HEADER
		if binlogEntry.Peer != nil {
			grpcLogRecord.PeerAddress = &grpclogrecordpb.GrpcLogRecord_Address{
				Type:    grpclogrecordpb.GrpcLogRecord_Address_Type(binlogEntry.Peer.Type),
				Address: binlogEntry.Peer.Address,
				IpPort:  binlogEntry.Peer.IpPort,
			}
		}
		grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetServerHeader().Metadata)
		grpcLogRecord.PayloadTruncated = binlogEntry.PayloadTruncated
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_MESSAGE
		grpcLogRecord.Message = binlogEntry.GetMessage().Data
		grpcLogRecord.PayloadSize = binlogEntry.GetMessage().GetLength()
		grpcLogRecord.PayloadTruncated = binlogEntry.PayloadTruncated
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_MESSAGE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_MESSAGE
		grpcLogRecord.Message = binlogEntry.GetMessage().Data
		grpcLogRecord.PayloadSize = binlogEntry.GetMessage().GetLength()
		grpcLogRecord.PayloadTruncated = binlogEntry.PayloadTruncated
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HALF_CLOSE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_HALF_CLOSE
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_TRAILER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_TRAILER
		grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetTrailer().Metadata)
		grpcLogRecord.StatusCode = binlogEntry.GetTrailer().StatusCode
		grpcLogRecord.StatusMessage = binlogEntry.GetTrailer().StatusMessage
		grpcLogRecord.StatusDetails = binlogEntry.GetTrailer().StatusDetails
		grpcLogRecord.PayloadTruncated = binlogEntry.PayloadTruncated
		callEnded = true
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CANCEL:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_CANCEL
		callEnded = true
	default:
		return fmt.Errorf("unknown event type: %v", binlogEntry.Type)
	}
	switch binlogEntry.Logger {
	case binlogpb.GrpcLogEntry_LOGGER_CLIENT:
		grpcLogRecord.EventLogger = grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT
	case binlogpb.GrpcLogEntry_LOGGER_SERVER:
		grpcLogRecord.EventLogger = grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER
	default:
		grpcLogRecord.EventLogger = grpclogrecordpb.GrpcLogRecord_LOGGER_UNKNOWN
	}
	if callEnded {
		cls.removeEntry(binlogEntry.CallId)
	}
	// CloudLogging client doesn't return error on entry write. Entry writes
	// don't mean the data will be uploaded immediately.
	cls.exporter.EmitGrpcLogRecord(&grpcLogRecord)
	return nil
}

// Close closes the cloudLoggingSink, which is a noop due to no state is
// maintained by this object.
func (*cloudLoggingSink) Close() error {
	return nil
}

func newCloudLoggingSink() *cloudLoggingSink {
	return &cloudLoggingSink{
		callIDToUUID: make(map[uint64]string),
	}
}

func compileBinaryLogControlString(config *configpb.ObservabilityConfig) string {
	if config.LoggingConfig == nil {
		return ""
	}

	var entries []string
	for _, logFilter := range config.LoggingConfig.LogFilters {
		// With undefined HeaderBytes or MessageBytes, the intended behavior is
		// logging zero payload. This detail is different than binary logging.
		entries = append(entries, fmt.Sprintf("%v{h:%v;m:%v}", logFilter.Pattern, logFilter.HeaderBytes, logFilter.MessageBytes))
	}
	entries = append(entries, "-google.logging.v2.LoggingServiceV2/WriteLogEntries")
	return strings.Join(entries, ",")
}

func startLogging(config *configpb.ObservabilityConfig) {
	if config == nil {
		return
	}
	var binlogConfig = compileBinaryLogControlString(config)
	iblog.SetLogger(iblog.NewLoggerFromConfigString(binlogConfig))
	defaultCloudLoggingSink = newCloudLoggingSink()
	binarylog.SetSink(defaultCloudLoggingSink)
	logger.Infof("Start logging with config [%v] and sink [%p]", binlogConfig, &iblog.DefaultSink)
}
