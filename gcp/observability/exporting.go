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
	"time"

	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats/opencensus"

	gcplogging "cloud.google.com/go/logging"
)

// cOptsDisableLogTrace are client options for the go client libraries which are
// used to configure connections to GCP exporting backends. These disable global
// dial and server options set by this module, which configure logging, metrics,
// and tracing on all created grpc.ClientConn's and grpc.Server's. These options
// turn on only metrics, and also disable the client libraries behavior of
// plumbing in the older opencensus instrumentation code.
var cOptsDisableLogTrace = []option.ClientOption{
	option.WithTelemetryDisabled(),
	option.WithGRPCDialOption(internal.DisableGlobalDialOptions.(func() grpc.DialOption)()),
	option.WithGRPCDialOption(opencensus.DialOption(opencensus.TraceOptions{
		DisableTrace: true,
	})),
}

// loggingExporter is the interface of logging exporter for gRPC Observability.
// In future, we might expose this to allow users provide custom exporters. But
// now, it exists for testing purposes.
type loggingExporter interface {
	// EmitGrpcLogRecord writes a gRPC LogRecord to cache without blocking.
	EmitGcpLoggingEntry(entry gcplogging.Entry)
	// Close flushes all pending data and closes the exporter.
	Close() error
}

type cloudLoggingExporter struct {
	projectID string
	client    *gcplogging.Client
	logger    *gcplogging.Logger
}

func newCloudLoggingExporter(ctx context.Context, config *config) (loggingExporter, error) {
	c, err := gcplogging.NewClient(ctx, fmt.Sprintf("projects/%v", config.ProjectID), cOptsDisableLogTrace...)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudLoggingExporter: %v", err)
	}
	defer logger.Infof("Successfully created cloudLoggingExporter")
	if len(config.Labels) != 0 {
		logger.Infof("Adding labels: %+v", config.Labels)
	}
	return &cloudLoggingExporter{
		projectID: config.ProjectID,
		client:    c,
		logger:    c.Logger("microservices.googleapis.com/observability/grpc", gcplogging.CommonLabels(config.Labels), gcplogging.BufferedByteLimit(1024*1024*50), gcplogging.DelayThreshold(time.Second*10)),
	}, nil
}

func (cle *cloudLoggingExporter) EmitGcpLoggingEntry(entry gcplogging.Entry) {
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
