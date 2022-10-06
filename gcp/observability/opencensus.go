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
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"

	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
)

var (
	// It's a variable instead of const to speed up testing
	defaultMetricsReportingInterval = time.Second * 30
)

func tagsToMonitoringLabels(tags map[string]string) *stackdriver.Labels {
	labels := &stackdriver.Labels{}
	for k, v := range tags {
		labels.Set(k, v, "")
	}
	return labels
}

func tagsToTraceAttributes(tags map[string]string) map[string]interface{} {
	ta := make(map[string]interface{}, len(tags))
	for k, v := range tags {
		ta[k] = v
	}
	return ta
}

type tracingMetricsExporter interface {
	trace.Exporter
	view.Exporter
	Flush()
	Close() error
}

var exporter tracingMetricsExporter

// global to stub out in tests
var newExporter = newStackdriverExporter

func newStackdriverExporter(config *config) (tracingMetricsExporter, error) {
	// Create the Stackdriver exporter, which is shared between tracing and stats
	mr := monitoredresource.Autodetect()
	logger.Infof("Detected MonitoredResource:: %+v", mr)
	var err error
	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:               config.DestinationProjectID,
		MonitoredResource:       mr,
		DefaultMonitoringLabels: tagsToMonitoringLabels(config.CustomTags),
		DefaultTraceAttributes:  tagsToTraceAttributes(config.CustomTags),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Stackdriver exporter: %v", err)
	}
	return exporter, nil
}

// This method accepts config and exporter; the exporter argument is exposed to
// assist unit testing of the OpenCensus behavior.
func startOpenCensus(config *config) error {
	// If both tracing and metrics are disabled, there's no point inject default
	// StatsHandler.
	if config == nil || (!config.EnableCloudTrace && !config.EnableCloudMonitoring) {
		return nil
	}

	var err error
	exporter, err = newExporter(config)
	if err != nil {
		return err
	}

	var so trace.StartOptions
	if config.EnableCloudTrace {
		so.Sampler = trace.ProbabilitySampler(config.GlobalTraceSamplingRate)
		trace.RegisterExporter(exporter.(trace.Exporter))
		logger.Infof("Start collecting and exporting trace spans with global_trace_sampling_rate=%.2f", config.GlobalTraceSamplingRate)
	}

	if config.EnableCloudMonitoring {
		if err := view.Register(ocgrpc.DefaultClientViews...); err != nil {
			return fmt.Errorf("failed to register default client views: %v", err)
		}
		if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
			return fmt.Errorf("failed to register default server views: %v", err)
		}
		view.SetReportingPeriod(defaultMetricsReportingInterval)
		view.RegisterExporter(exporter.(view.Exporter))
		logger.Infof("Start collecting and exporting metrics")
	}

	// Only register default StatsHandlers if other things are setup correctly.
	internal.AddGlobalServerOptions.(func(opt ...grpc.ServerOption))(grpc.StatsHandler(&ocgrpc.ServerHandler{StartOptions: so}))
	internal.AddGlobalDialOptions.(func(opt ...grpc.DialOption))(grpc.WithStatsHandler(&ocgrpc.ClientHandler{StartOptions: so}))
	logger.Infof("Enabled OpenCensus StatsHandlers for clients and servers")

	return nil
}

// stopOpenCensus flushes the exporter's and cleans up globals across all
// packages if exporter was created.
func stopOpenCensus() {
	if exporter != nil {
		internal.ClearGlobalDialOptions()
		internal.ClearGlobalServerOptions()

		// Call these unconditionally, doesn't matter if not registered, will be
		// a noop if not registered.
		trace.UnregisterExporter(exporter)
		view.UnregisterExporter(exporter)

		exporter.Flush()
		exporter.Close()
	}
}
