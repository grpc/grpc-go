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
	"fmt"

	gcplogging "cloud.google.com/go/logging"
	grpclogrecordpb "google.golang.org/grpc/observability/internal/logging"
	"google.golang.org/protobuf/encoding/protojson"
)

// loggingExporter is the interface of logging exporter for gRPC
// Observability. Ideally, we should use what OTEL provides, but their Golang
// implementation is in "frozen" state. So, this plugin provides a minimum
// interface to satisfy testing purposes.
type loggingExporter interface {
	// EmitGrpcLogRecord writes a gRPC LogRecord to cache without blocking.
	EmitGrpcLogRecord(*grpclogrecordpb.GrpcLogRecord)
	// Close flushes all pending data and closes the exporter.
	Close() error
}

// globalLoggingExporter is the global logging exporter, may be nil.
var globalLoggingExporter loggingExporter

type cloudLoggingExporter struct {
	projectID string
	client    *gcplogging.Client
	logger    *gcplogging.Logger
}

func newCloudLoggingExporter(ctx context.Context, projectID string) (*cloudLoggingExporter, error) {
	c, err := gcplogging.NewClient(ctx, fmt.Sprintf("projects/%v", projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudLoggingExporter: %v", err)
	}
	logger.Infof("Successfully created cloudLoggingExporter")
	return &cloudLoggingExporter{
		projectID: projectID,
		client:    c,
		logger:    c.Logger("grpc"),
	}, nil
}

// mapLogLevelToSeverity maps the gRPC defined log level to Cloud Logging's
// Severity. The canonical definition can be found at
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity.
var logLevelToSeverity = map[grpclogrecordpb.GrpcLogRecord_LogLevel]gcplogging.Severity{
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_UNKNOWN:  0,
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_TRACE:    100, // Cloud Logging doesn't have a trace level, treated as DEBUG.
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_DEBUG:    100,
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_INFO:     200,
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_WARN:     400,
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_ERROR:    500,
	grpclogrecordpb.GrpcLogRecord_LOG_LEVEL_CRITICAL: 600,
}

var protoToJSONOptions = &protojson.MarshalOptions{
	UseProtoNames:  false,
	UseEnumNumbers: false,
}

func (cle *cloudLoggingExporter) EmitGrpcLogRecord(l *grpclogrecordpb.GrpcLogRecord) {
	// Converts the log record content to a more readable format via protojson.
	// This is technically a hack, will be removed once we removed our
	// dependencies to Cloud Logging SDK.
	jsonBytes, err := protoToJSONOptions.Marshal(l)
	if err != nil {
		logger.Errorf("Unable to marshal log record: %v", l)
	}
	var payload map[string]interface{}
	err = json.Unmarshal(jsonBytes, &payload)
	if err != nil {
		logger.Errorf("Unable to unmarshal bytes to JSON: %v", jsonBytes)
	}
	// Converts severity from log level
	var severity, ok = logLevelToSeverity[l.LogLevel]
	if !ok {
		logger.Errorf("Invalid log level: %v", l.LogLevel)
		severity = 0
	}
	entry := gcplogging.Entry{
		Timestamp: l.Timestamp.AsTime(),
		Severity:  severity,
		Payload:   payload,
	}
	cle.logger.Log(entry)
	if logger.V(2) {
		logger.Infof("Uploading event to CloudLogging: %+v", entry)
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
	logger.Infof("Closed CloudLogging exporter")
	return nil
}

func createDefaultLoggingExporter(ctx context.Context, projectID string) error {
	var err error
	globalLoggingExporter, err = newCloudLoggingExporter(ctx, projectID)
	return err
}

func closeLoggingExporter() {
	if globalLoggingExporter != nil {
		if err := globalLoggingExporter.Close(); err != nil {
			logger.Infof("Failed to close logging exporter: %v", err)
		}
		globalLoggingExporter = nil
	}
}
