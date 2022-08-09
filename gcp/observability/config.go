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
	"io/ioutil"
	"os"
	"regexp"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
)

const (
	envObservabilityConfig     = "GRPC_CONFIG_OBSERVABILITY"
	envObservabilityConfigJSON = "GRPC_CONFIG_OBSERVABILITY_JSON"
	envProjectID               = "GOOGLE_CLOUD_PROJECT"
	logFilterPatternRegexpStr  = `^([\w./]+)/((?:\w+)|[*])$`
)

var logFilterPatternRegexp = regexp.MustCompile(logFilterPatternRegexpStr)

// LogFilter represents a method logging configuration.
type LogFilter struct {
	// Pattern is a string which can select a group of method names. By
	// default, the Pattern is an empty string, matching no methods.
	//
	// Only "*" Wildcard is accepted for Pattern. A Pattern is in the form
	// of <service>/<method> or just a character "*" .
	//
	// If the Pattern is "*", it specifies the defaults for all the
	// services; If the Pattern is <service>/*, it specifies the defaults
	// for all methods in the specified service <service>; If the Pattern is
	// */<method>, this is not supported.
	//
	// Examples:
	//  - "Foo/Bar" selects only the method "Bar" from service "Foo"
	//  - "Foo/*" selects all methods from service "Foo"
	//  - "*" selects all methods from all services.
	Pattern string `json:"pattern,omitempty"`
	// HeaderBytes is the number of bytes of each header to log. If the size of
	// the header is greater than the defined limit, content past the limit will
	// be truncated. The default value is 0.
	HeaderBytes int32 `json:"header_bytes,omitempty"`
	// MessageBytes is the number of bytes of each message to log. If the size
	// of the message is greater than the defined limit, content pass the limit
	// will be truncated. The default value is 0.
	MessageBytes int32 `json:"message_bytes,omitempty"`
}

// ObvConfig is configuration for observability behaviors. By default, no
// configuration is required for tracing/metrics/logging to function. This
// config captures the most common knobs for gRPC users. It's always possible to
// override with explicit config in code.
type ObvConfig struct {
	// EnableCloudTrace represents whether the tracing data upload to
	// CloudTrace should be enabled or not.
	EnableCloudTrace bool `json:"enable_cloud_trace,omitempty"`
	// EnableCloudMonitoring represents whether the metrics data upload to
	// CloudMonitoring should be enabled or not.
	EnableCloudMonitoring bool `json:"enable_cloud_monitoring,omitempty"`
	// EnableCloudLogging represents Whether the logging data upload to
	// CloudLogging should be enabled or not.
	EnableCloudLogging bool `json:"enable_cloud_logging,omitempty"`
	// DestinationProjectID is the destination GCP project identifier for the
	// uploading log entries. If empty, the gRPC Observability plugin will
	// attempt to fetch the project_id from the GCP environment variables, or
	// from the default credentials.
	DestinationProjectID string `json:"destination_project_id,omitempty"`
	// LogFilters is a list of method config. The order matters here - the first
	// Pattern which matches the current method will apply the associated config
	// options in the LogFilter. Any other LogFilter that also matches that
	// comes later will be ignored. So a LogFilter of "*/*" should appear last
	// in this list.
	LogFilters []LogFilter `json:"log_filters,omitempty"`
	// GlobalTraceSamplingRate is the global setting that controls the
	// probability of a RPC being traced. For example, 0.05 means there is a 5%
	// chance for a RPC to be traced, 1.0 means trace every call, 0 means don’t
	// start new traces.
	GlobalTraceSamplingRate float64 `json:"global_trace_sampling_rate,omitempty"`
	// CustomTags a list of custom tags that will be attached to every log
	// entry.
	CustomTags map[string]string `json:"custom_tags,omitempty"`
}

// fetchDefaultProjectID fetches the default GCP project id from environment.
func fetchDefaultProjectID(ctx context.Context) string {
	// Step 1: Check ENV var
	if s := os.Getenv(envProjectID); s != "" {
		logger.Infof("Found project ID from env %v: %v", envProjectID, s)
		return s
	}
	// Step 2: Check default credential
	credentials, err := google.FindDefaultCredentials(ctx, gcplogging.WriteScope)
	if err != nil {
		logger.Infof("Failed to locate Google Default Credential: %v", err)
		return ""
	}
	if credentials.ProjectID == "" {
		logger.Infof("Failed to find project ID in default credential: %v", err)
		return ""
	}
	logger.Infof("Found project ID from Google Default Credential: %v", credentials.ProjectID)
	return credentials.ProjectID
}

func validateFilters(config *ObvConfig) error {
	for _, filter := range config.LogFilters {
		if filter.Pattern == "*" {
			continue
		}
		match := logFilterPatternRegexp.FindStringSubmatch(filter.Pattern)
		if match == nil {
			return fmt.Errorf("invalid log filter Pattern: %v", filter.Pattern)
		}
	}
	return nil
}

// unmarshalAndVerifyConfig unmarshals a json string representing an
// observability config into its internal go format, and also verifies the
// configuration's fields for validity.
func unmarshalAndVerifyConfig(rawJSON json.RawMessage) (*ObvConfig, error) {
	var config ObvConfig
	if err := json.Unmarshal(rawJSON, &config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if err := validateFilters(&config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if config.GlobalTraceSamplingRate > 1 || config.GlobalTraceSamplingRate < 0 {
		return nil, fmt.Errorf("error parsing observability config: invalid global trace sampling rate %v", config.GlobalTraceSamplingRate)
	}
	logger.Infof("Parsed ObservabilityConfig: %+v", &config)
	return &config, nil
}

func parseObservabilityConfig() (*ObvConfig, error) {
	if fileSystemPath := os.Getenv(envObservabilityConfigJSON); fileSystemPath != "" {
		content, err := ioutil.ReadFile(fileSystemPath) // TODO: Switch to os.ReadFile once dropped support for go 1.15
		if err != nil {
			return nil, fmt.Errorf("error reading observability configuration file %q: %v", fileSystemPath, err)
		}
		return unmarshalAndVerifyConfig(content)
	} else if content := os.Getenv(envObservabilityConfig); content != "" {
		return unmarshalAndVerifyConfig([]byte(content))
	}
	// If the ENV var doesn't exist, do nothing
	return nil, nil
}

func ensureProjectIDInObservabilityConfig(ctx context.Context, config *ObvConfig) error {
	if config.DestinationProjectID == "" {
		// Try to fetch the GCP project id
		projectID := fetchDefaultProjectID(ctx)
		if projectID == "" {
			return fmt.Errorf("empty destination project ID")
		}
		config.DestinationProjectID = projectID
	}
	return nil
}
