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

// Package observability implements the tracing, metrics, and logging data
// collection, and provides controlling knobs via a config file.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
package observability

import (
	"context"

	"google.golang.org/grpc/binarylog"
	"google.golang.org/grpc/grpclog"
	configpb "google.golang.org/grpc/observability/internal/config"
)

var (
	logger = grpclog.Component("observability")
	config *configpb.ObservabilityConfig
)

func init() {
	internalInit()
}

// internalInit exists to allow unit tests to re-parse ENV var.
func internalInit() {
	config = parseObservabilityConfig()

	// Logging is controlled by the config at methods level. Users might bring
	// their in-house exporter. If logging is disabled, binary logging also
	// won't start. The overhead should be minimum.
	startLogging(config)
}

// Start is the opt-in API for gRPC Observability plugin. This function should
// be invoked in the main function, and before creating any gRPC clients or
// servers, otherwise, they might not be instrumented. At high-level, this
// module does the following:
//
//   - it loads observability config from environment;
//   - it registers default exporters if not disabled by the config;
//   - it sets up binary logging sink against the logging exporter.
//
// Note: currently, the binarylog module only supports one sink, so using the
// "observability" module will conflict with existing binarylog usage.
// Note: handle the error
func Start(ctx context.Context) error {
	// If no project ID is found, that's ok
	maybeUpdateProjectIDInObservabilityConfig(ctx, config)

	// If the default logging exporter is not disabled, register one.
	if config != nil && config.ExporterConfig.ProjectId != "" && !config.ExporterConfig.DisableDefaultLoggingExporter {
		if err := createDefaultLoggingExporter(ctx, config.ExporterConfig.ProjectId); err != nil {
			return err
		}
		defaultCloudLoggingSink.SetExporter(loggingExporter)
	}
	return nil
}

// End is the clean-up API for gRPC Observability plugin. It is expected to be
// invoked in the main function of the application. The suggested usage is
// "defer observability.End()". This function also flushes data to upstream, and
// cleanup resources.
func End() {
	closeLoggingExporter()
	binarylog.SetSink(nil)
}
