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
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package observability

import (
	"context"
	"fmt"

	"google.golang.org/grpc/grpclog"
)

var logger = grpclog.Component("observability")

// Start is the opt-in API for gRPC Observability plugin. This function should
// be invoked in the main function, and before creating any gRPC clients or
// servers, otherwise, they might not be instrumented. At high-level, this
// module does the following:
//
//   - it loads observability config from environment;
//   - it registers default exporters if not disabled by the config;
//   - it sets up telemetry collectors (binary logging sink or StatsHandlers).
//
// Note: this method should only be invoked once.
// Note: handle the error
func Start(ctx context.Context) error {
	config, err := parseObservabilityConfig()
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("no ObservabilityConfig found")
	}

	// Set the project ID if it isn't configured manually.
	if err := ensureProjectIDInObservabilityConfig(ctx, config); err != nil {
		return err
	}

	// Enabling tracing and metrics via OpenCensus
	if err := startOpenCensus(config); err != nil {
		return fmt.Errorf("failed to instrument OpenCensus: %v", err)
	}

	// Logging is controlled by the config at methods level.
	return startLogging(ctx, config)
}

// End is the clean-up API for gRPC Observability plugin. It is expected to be
// invoked in the main function of the application. The suggested usage is
// "defer observability.End()". This function also flushes data to upstream, and
// cleanup resources.
//
// Note: this method should only be invoked once.
func End() {
	stopLogging()
	stopOpenCensus()
}
