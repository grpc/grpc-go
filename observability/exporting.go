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
	"encoding/json"
	"strconv"

	coptlogging "cloud.google.com/go/logging"
	"google.golang.org/grpc/observability/grpclogrecord"
	"google.golang.org/protobuf/encoding/protojson"
)

// genericLoggingExporter is the interface of logging exporter for gRPC
// Observability. Ideally, we should use what OTEL provides, but their Golang
// implementation is in "frozen" state. So, this plugin provides a minimum
// interface to satisfy testing purposes.
type genericLoggingExporter interface {
	// EmitGrpcLogRecord writes a gRPC LogRecord to cache without blocking.
	EmitGrpcLogRecord(*grpclogrecord.GrpcLogRecord)
	// Close flushes all pending data and closes the exporter.
	Close() error
}

var (
	// loggingExporter is the global logging exporter, may be nil.
	loggingExporter genericLoggingExporter
)

// cloudLoggingExporter is the exporter for CloudLogging.
type cloudLoggingExporter struct {
	client *coptlogging.Client
	logger *coptlogging.Logger
}

func newCloudLoggingExporter(projectID string) *cloudLoggingExporter {
	ctx := context.Background()
	c, err := coptlogging.NewClient(ctx, projectID)
	if err != nil {
		logger.Errorf("failed to create cloudLoggingExporter: %v", err)
		return nil
	}
	logger.Infof("successfully created cloudLoggingExporter")
	return &cloudLoggingExporter{
		client: c,
		logger: c.Logger("observability"),
	}
}

// mapLogLevelToSeverity maps the gRPC defined log level to Cloud Logging's
// Severity. The canonical definition can be found at
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity.
func mapLogLevelToSeverity(l grpclogrecord.LogLevel) coptlogging.Severity {
	switch l {
	case grpclogrecord.LogLevel_LOG_LEVEL_UNKNOWN:
		return 0
	case grpclogrecord.LogLevel_LOG_LEVEL_TRACE:
		// Cloud Logging doesn't have a trace level, treated as DEBUG.
		return 100
	case grpclogrecord.LogLevel_LOG_LEVEL_DEBUG:
		return 100
	case grpclogrecord.LogLevel_LOG_LEVEL_INFO:
		return 200
	case grpclogrecord.LogLevel_LOG_LEVEL_WARN:
		return 400
	case grpclogrecord.LogLevel_LOG_LEVEL_ERROR:
		return 500
	case grpclogrecord.LogLevel_LOG_LEVEL_FATAL:
		return 600
	default:
		logger.Errorf("unknown LogLevel: %v", l)
		return -1
	}
}

func (cle *cloudLoggingExporter) EmitGrpcLogRecord(l *grpclogrecord.GrpcLogRecord) {
	body, err := protojson.Marshal(l)
	if err != nil {
		logger.Errorf("failed to marshal GrpcLogRecord: %v", l)
		return
	}
	entry := coptlogging.Entry{
		Timestamp: l.Timestamp.AsTime(),
		Trace:     strconv.FormatUint(l.TraceId, 10),
		SpanID:    strconv.FormatUint(l.SpanId, 10),
		Severity:  mapLogLevelToSeverity(l.LogLevel),
		Payload:   string(body),
	}
	cle.logger.Log(entry)
	if logger.V(2) {
		eventJSON, _ := json.Marshal(entry)
		logger.Infof("Uploading event to CloudLogging: %s", eventJSON)
	}
}

func (cle *cloudLoggingExporter) Close() error {
	if cle.logger != nil {
		if err := cle.logger.Flush(); err != nil {
			return err
		}
	}
	if cle.client != nil {
		if err := cle.client.Close(); err != nil {
			return err
		}
	}
	logger.Infof("closed CloudLogging exporter")
	return nil
}

func createDefaultLoggingExporter(projectID string) {
	loggingExporter = newCloudLoggingExporter(projectID)
}

func closeLoggingExporter() {
	if loggingExporter != nil {
		if err := loggingExporter.Close(); err != nil {
			logger.Infof("failed to close logging exporter: %v", err)
		}
	}
}

// registerLoggingExporter allows custom logging exporter, currently only
// available to testing inside this package.
func registerLoggingExporter(e genericLoggingExporter) {
	loggingExporter = e
}

// Emit is the wrapper for producing a log entry, hiding all the abstraction details.
func Emit(l *grpclogrecord.GrpcLogRecord) {
	if loggingExporter != nil {
		loggingExporter.EmitGrpcLogRecord(l)
	}
}
