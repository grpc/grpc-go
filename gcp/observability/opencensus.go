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
	"os"
	"strconv"
	"time"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/stats/opencensus"
)

var (
	// It's a variable instead of const to speed up testing
	defaultMetricsReportingInterval = time.Second * 30
	defaultViews                    = []*view.View{
		opencensus.ClientStartedRPCsView,
		opencensus.ClientCompletedRPCsView,
		opencensus.ClientRoundtripLatencyView,
		opencensus.ClientSentCompressedMessageBytesPerRPCView,
		opencensus.ClientReceivedCompressedMessageBytesPerRPCView,
		opencensus.ClientAPILatencyView,
		opencensus.ServerStartedRPCsView,
		opencensus.ServerCompletedRPCsView,
		opencensus.ServerSentCompressedMessageBytesPerRPCView,
		opencensus.ServerReceivedCompressedMessageBytesPerRPCView,
		opencensus.ServerLatencyView,
	}
)

func labelsToMonitoringLabels(labels map[string]string) *stackdriver.Labels {
	sdLabels := &stackdriver.Labels{}
	for k, v := range labels {
		sdLabels.Set(k, v, "")
	}
	return sdLabels
}

func labelsToTraceAttributes(labels map[string]string) map[string]any {
	ta := make(map[string]any, len(labels))
	for k, v := range labels {
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

var newExporter = newStackdriverExporter

func newStackdriverExporter(config *config) (tracingMetricsExporter, error) {
	// Create the Stackdriver exporter, which is shared between tracing and stats
	mr := monitoredresource.Autodetect()
	logger.Infof("Detected MonitoredResource:: %+v", mr)
	var err error
	// Custom labels completely overwrite any labels generated in the OpenCensus
	// library, including their label that uniquely identifies the process.
	// Thus, generate a unique process identifier here to uniquely identify
	// process for metrics exporting to function correctly.
	metricsLabels := make(map[string]string, len(config.Labels)+1)
	for k, v := range config.Labels {
		metricsLabels[k] = v
	}
	metricsLabels["opencensus_task"] = generateUniqueProcessIdentifier()
	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:               config.ProjectID,
		MonitoredResource:       mr,
		DefaultMonitoringLabels: labelsToMonitoringLabels(metricsLabels),
		DefaultTraceAttributes:  labelsToTraceAttributes(config.Labels),
		MonitoringClientOptions: cOptsDisableLogTrace,
		TraceClientOptions:      cOptsDisableLogTrace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Stackdriver exporter: %v", err)
	}
	return exporter, nil
}

// generateUniqueProcessIdentifier returns a unique process identifier for the
// process this code is running in. This is the same way the OpenCensus library
// generates the unique process identifier, in the format of
// "go-<pid>@<hostname>".
func generateUniqueProcessIdentifier() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	return "go-" + strconv.Itoa(os.Getpid()) + "@" + hostname
}

// This method accepts config and exporter; the exporter argument is exposed to
// assist unit testing of the OpenCensus behavior.
func startOpenCensus(config *config) error {
	// If both tracing and metrics are disabled, there's no point inject default
	// StatsHandler.
	if config == nil || (config.CloudTrace == nil && config.CloudMonitoring == nil) {
		return nil
	}

	var err error
	exporter, err = newExporter(config)
	if err != nil {
		return err
	}

	var to opencensus.TraceOptions
	if config.CloudTrace != nil {
		to.TS = trace.ProbabilitySampler(config.CloudTrace.SamplingRate)
		trace.RegisterExporter(exporter.(trace.Exporter))
		logger.Infof("Start collecting and exporting trace spans with global_trace_sampling_rate=%.2f", config.CloudTrace.SamplingRate)
	}

	if config.CloudMonitoring != nil {
		if err := view.Register(defaultViews...); err != nil {
			return fmt.Errorf("failed to register observability views: %v", err)
		}
		view.SetReportingPeriod(defaultMetricsReportingInterval)
		view.RegisterExporter(exporter.(view.Exporter))
		logger.Infof("Start collecting and exporting metrics")
	}

	internal.AddGlobalServerOptions.(func(opt ...grpc.ServerOption))(opencensus.ServerOption(to))
	internal.AddGlobalDialOptions.(func(opt ...grpc.DialOption))(opencensus.DialOption(to))
	logger.Infof("Enabled OpenCensus StatsHandlers for clients and servers")

	return nil
}

// stopOpenCensus flushes the exporter's and cleans up globals across all
// packages if exporter was created.
func stopOpenCensus() {
	if exporter != nil {
		internal.ClearGlobalDialOptions()
		internal.ClearGlobalServerOptions()
		// This Unregister call guarantees the data recorded gets sent to
		// exporter, synchronising the view package and exporter. Doesn't matter
		// if views not registered, will be a noop if not registered.
		view.Unregister(defaultViews...)
		// Call these unconditionally, doesn't matter if not registered, will be
		// a noop if not registered.
		trace.UnregisterExporter(exporter)
		view.UnregisterExporter(exporter)

		// This Flush call makes sure recorded telemetry get sent to backend.
		exporter.Flush()
		exporter.Close()
	}
}
