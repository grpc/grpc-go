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
	"os"

	gcplogging "cloud.google.com/go/logging"
	grpclogrecordpb "google.golang.org/grpc/gcp/observability/internal/logging"
	"google.golang.org/protobuf/encoding/protojson"
)

// loggingExporter is the interface of logging exporter for gRPC Observability.
// In future, we might expose this to allow users provide custom exporters. But
// now, it exists for testing purposes.
type loggingExporter interface {
	// EmitGrpcLogRecord writes a gRPC LogRecord to cache without blocking.
	EmitGrpcLogRecord(*grpclogrecordpb.GrpcLogRecord)
	// Close flushes all pending data and closes the exporter.
	Close() error
}

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
	defer logger.Infof("Successfully created cloudLoggingExporter")
	customTags := getCustomTags(os.Environ())
	if len(customTags) != 0 {
		logger.Infof("Adding custom tags: %+v", customTags)
	}
	return &cloudLoggingExporter{
		projectID: projectID,
		client:    c,
		logger:    c.Logger("grpc", gcplogging.CommonLabels(customTags)),
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
	UseProtoNames:  true,
	UseEnumNumbers: false,
}

func (cle *cloudLoggingExporter) EmitGrpcLogRecord(l *grpclogrecordpb.GrpcLogRecord) {
	// Converts the log record content to a more readable format via protojson.
	jsonBytes, err := protoToJSONOptions.Marshal(l)
	if err != nil {
		logger.Infof("Unable to marshal log record: %v", l)
		return
	}
	var payload map[string]interface{}
	err = json.Unmarshal(jsonBytes, &payload)
	if err != nil {
		logger.Infof("Unable to unmarshal bytes to JSON: %v", jsonBytes)
		return
	}
	entry := gcplogging.Entry{
		Timestamp: l.Timestamp.AsTime(),
		Severity:  logLevelToSeverity[l.LogLevel],
		Payload:   payload,
	}
	cle.logger.Log(entry)
	if logger.V(2) {
		logger.Infof("Uploading event to CloudLogging: %+v", entry)
	}
}

func (cle *cloudLoggingExporter) Close() error {
	var errFlush, errClose error
	if cle.logger != nil {
		errFlush = cle.logger.Flush()
	}
	if cle.client != nil {
		errClose = cle.client.Close()
	}
	if errFlush != nil && errClose != nil {
		return fmt.Errorf("failed to close exporter. Flush failed: %v; Close failed: %v", errFlush, errClose)
	}
	if errFlush != nil {
		return errFlush
	}
	if errClose != nil {
		return errClose
	}
	cle.logger = nil
	cle.client = nil
	logger.Infof("Closed CloudLogging exporter")
	return nil
}
