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
	"fmt"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/google/uuid"
	binlogpb "google.golang.org/grpc/binarylog/grpc_binarylog_v1"
	iblog "google.golang.org/grpc/internal/binarylog"
	"google.golang.org/grpc/internal/grpcutil"
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

func setPeerIfPresent(binlogEntry *binlogpb.GrpcLogEntry, grpcLogRecord *grpclogrecordpb.GrpcLogRecord) {
	if binlogEntry.GetPeer() != nil {
		grpcLogRecord.PeerAddress = &grpclogrecordpb.GrpcLogRecord_Address{
			Type:    grpclogrecordpb.GrpcLogRecord_Address_Type(binlogEntry.Peer.Type),
			Address: binlogEntry.Peer.Address,
			IpPort:  binlogEntry.Peer.IpPort,
		}
	}
}

var loggerTypeToEventLogger = map[binlogpb.GrpcLogEntry_Logger]grpclogrecordpb.GrpcLogRecord_EventLogger{
	binlogpb.GrpcLogEntry_LOGGER_UNKNOWN: grpclogrecordpb.GrpcLogRecord_LOGGER_UNKNOWN,
	binlogpb.GrpcLogEntry_LOGGER_CLIENT:  grpclogrecordpb.GrpcLogRecord_LOGGER_CLIENT,
	binlogpb.GrpcLogEntry_LOGGER_SERVER:  grpclogrecordpb.GrpcLogRecord_LOGGER_SERVER,
}

type binaryMethodLogger struct {
	rpcID, serviceName, methodName string
	originalMethodLogger           iblog.MethodLogger
	wrappedMethodLogger            iblog.MethodLogger
	exporter                       loggingExporter
}

func (ml *binaryMethodLogger) Log(c iblog.LogEntryConfig) {
	// Invoke the original MethodLogger to maintain backward compatibility
	if ml.originalMethodLogger != nil {
		ml.originalMethodLogger.Log(c)
	}

	// Fetch the compiled binary logging log entry
	if ml.wrappedMethodLogger == nil {
		logger.Info("No wrapped method logger found")
		return
	}
	var binlogEntry *binlogpb.GrpcLogEntry
	o, ok := ml.wrappedMethodLogger.(interface {
		Build(iblog.LogEntryConfig) *binlogpb.GrpcLogEntry
	})
	if !ok {
		logger.Error("Failed to locate the Build method in wrapped method logger")
		return
	}
	binlogEntry = o.Build(c)

	// Translate to GrpcLogRecord
	grpcLogRecord := &grpclogrecordpb.GrpcLogRecord{
		Timestamp:   binlogEntry.GetTimestamp(),
		RpcId:       ml.rpcID,
		SequenceId:  binlogEntry.GetSequenceIdWithinCall(),
		EventLogger: loggerTypeToEventLogger[binlogEntry.Logger],
		// Making DEBUG the default LogLevel
		LogLevel: grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_DEBUG,
	}

	switch binlogEntry.GetType() {
	case binlogpb.GrpcLogEntry_EVENT_TYPE_UNKNOWN:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_UNKNOWN
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HEADER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_HEADER
		if binlogEntry.GetClientHeader() != nil {
			methodName := binlogEntry.GetClientHeader().MethodName
			// Example method name: /grpc.testing.TestService/UnaryCall
			if strings.Contains(methodName, "/") {
				tokens := strings.Split(methodName, "/")
				if len(tokens) == 3 {
					// Record service name and method name for all events
					ml.serviceName = tokens[1]
					ml.methodName = tokens[2]
				} else {
					logger.Infof("Malformed method name: %v", methodName)
				}
			}
			grpcLogRecord.Timeout = binlogEntry.GetClientHeader().Timeout
			grpcLogRecord.Authority = binlogEntry.GetClientHeader().Authority
			grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetClientHeader().Metadata)
		}
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
		setPeerIfPresent(binlogEntry, grpcLogRecord)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_HEADER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_HEADER
		grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetServerHeader().Metadata)
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
		setPeerIfPresent(binlogEntry, grpcLogRecord)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_MESSAGE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_REQUEST_MESSAGE
		grpcLogRecord.Message = binlogEntry.GetMessage().GetData()
		grpcLogRecord.PayloadSize = binlogEntry.GetMessage().GetLength()
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_MESSAGE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_RESPONSE_MESSAGE
		grpcLogRecord.Message = binlogEntry.GetMessage().GetData()
		grpcLogRecord.PayloadSize = binlogEntry.GetMessage().GetLength()
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CLIENT_HALF_CLOSE:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_HALF_CLOSE
	case binlogpb.GrpcLogEntry_EVENT_TYPE_SERVER_TRAILER:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_TRAILER
		grpcLogRecord.Metadata = translateMetadata(binlogEntry.GetTrailer().Metadata)
		grpcLogRecord.StatusCode = binlogEntry.GetTrailer().GetStatusCode()
		grpcLogRecord.StatusMessage = binlogEntry.GetTrailer().GetStatusMessage()
		grpcLogRecord.StatusDetails = binlogEntry.GetTrailer().GetStatusDetails()
		grpcLogRecord.PayloadTruncated = binlogEntry.GetPayloadTruncated()
		setPeerIfPresent(binlogEntry, grpcLogRecord)
	case binlogpb.GrpcLogEntry_EVENT_TYPE_CANCEL:
		grpcLogRecord.EventType = grpclogrecordpb.GrpcLogRecord_GRPC_CALL_CANCEL
	default:
		logger.Infof("Unknown event type: %v", binlogEntry.Type)
		return
	}
	grpcLogRecord.ServiceName = ml.serviceName
	grpcLogRecord.MethodName = ml.methodName
	ml.exporter.EmitGrpcLogRecord(grpcLogRecord)
}

type binaryLogger struct {
	// originalLogger is needed to ensure binary logging users won't be impacted
	// by this plugin. Users are allowed to subscribe to a completely different
	// set of methods.
	originalLogger iblog.Logger
	// exporter is a loggingExporter and the handle for uploading collected data to backends
	exporter unsafe.Pointer // loggingExporter
	// filters is []*configpb.ObservabilityConfig_LogFilter used to set config on methods
	filters unsafe.Pointer // []*configpb.ObservabilityConfig_LogFilter
}

func (l *binaryLogger) loadExporter() loggingExporter {
	ptrPtr := atomic.LoadPointer(&l.exporter)
	if ptrPtr == nil {
		return nil
	}
	exporterPtr := (*loggingExporter)(ptrPtr)
	return *exporterPtr
}

func (l *binaryLogger) loadFilters() []*configpb.ObservabilityConfig_LogFilter {
	ptrPtr := atomic.LoadPointer(&l.filters)
	if ptrPtr == nil {
		return nil
	}
	filterPtr := (*[]*configpb.ObservabilityConfig_LogFilter)(ptrPtr)
	return *filterPtr
}

func matchLogFilter(serviceMethod string, filters []*configpb.ObservabilityConfig_LogFilter) *configpb.ObservabilityConfig_LogFilter {
	// Split input serviceMethod
	s, _, err := grpcutil.ParseMethod(serviceMethod)
	if err != nil {
		// This service method string is provided by upstream code, if
		// it's malformed, we are in bigger trouble.
		logger.Errorf("Invalid service method: %v", err)
		return nil
	}

	// Try matching the filters one by one, pick the first match. The
	// correctness of the log filter pattern is ensured by config.go.
	for _, filter := range filters {
		if filter.Pattern == "*" {
			// Match a "*"
			return filter
		}
		tokens := strings.SplitN(filter.Pattern, "/", 2)
		filterService := tokens[0]
		filterMethod := tokens[1]
		if filterMethod == "*" {
			// Handle "p.s/*" case
			if s == filterService {
				return filter
			}
			return nil
		}
		if serviceMethod == filter.Pattern {
			// Exact match of "p.s/m"
			return filter
		}
	}
	return nil
}

func (l *binaryLogger) GetMethodLogger(methodName string) iblog.MethodLogger {
	var ol, ml iblog.MethodLogger

	if l.originalLogger != nil {
		ol = l.originalLogger.GetMethodLogger(methodName)
	}

	// If no exporter is specified, there is no point creating a method
	// logger. We don't have any chance to inject exporter after its
	// creation.
	exporter := l.loadExporter()
	if exporter == nil {
		return ol
	}

	// If user specify a "*" pattern, binarylog will log every single call and
	// content. This means the exporting RPC's events will be captured. Even if
	// we batch up the uploads in the exporting RPC, the message content of that
	// RPC will be logged. Without this exclusion, we may end up with an ever
	// expanding message field in log entries, and crash the process with OOM.
	if methodName == "google.logging.v2.LoggingServiceV2/WriteLogEntries" {
		return ol
	}

	filter := matchLogFilter(methodName, l.loadFilters())
	if filter == nil {
		// No matching filter found
		return ol
	}
	ml = iblog.NewMethodLogger(uint64(filter.HeaderBytes), uint64(filter.MessageBytes))
	return &binaryMethodLogger{
		originalMethodLogger: ol,
		wrappedMethodLogger:  ml,
		rpcID:                uuid.NewString(),
		exporter:             exporter,
	}
}

func (l *binaryLogger) Close() {
	if l == nil {
		return
	}
	ePtr := atomic.LoadPointer(&l.exporter)
	if ePtr != nil {
		exporter := (*loggingExporter)(ePtr)
		if err := (*exporter).Close(); err != nil {
			logger.Infof("Failed to close logging exporter: %v", err)
		}
	}
}

// start is the core logic for setting up the custom binary logging logger, and
// it's also useful for testing.
func (l *binaryLogger) start(config *configpb.ObservabilityConfig, exporter loggingExporter) error {
	filters := config.GetLogFilters()
	if len(filters) == 0 || exporter == nil {
		// Doing nothing is allowed
		if exporter != nil {
			// The exporter is owned by binaryLogger, so we should close it if
			// we are not planning to use it.
			exporter.Close()
		}
		logger.Info("Skipping gRPC Observability logger: no config")
		return nil
	}
	atomic.StorePointer(&l.filters, unsafe.Pointer(&filters))
	atomic.StorePointer(&l.exporter, unsafe.Pointer(&exporter))
	logger.Info("Start gRPC Observability logger")
	return nil
}

func (l *binaryLogger) Start(ctx context.Context, config *configpb.ObservabilityConfig) error {
	if config == nil || !config.GetEnableCloudLogging() {
		return nil
	}
	if config.GetDestinationProjectId() == "" {
		return fmt.Errorf("failed to enable CloudLogging: empty destination_project_id")
	}
	exporter, err := newCloudLoggingExporter(ctx, config.DestinationProjectId)
	if err != nil {
		return fmt.Errorf("unable to create CloudLogging exporter: %v", err)
	}
	l.start(config, exporter)
	return nil
}

func newBinaryLogger(iblogger iblog.Logger) *binaryLogger {
	return &binaryLogger{
		originalLogger: iblogger,
	}
}

var defaultLogger *binaryLogger

func prepareLogging() {
	defaultLogger = newBinaryLogger(iblog.GetLogger())
	iblog.SetLogger(defaultLogger)
}
