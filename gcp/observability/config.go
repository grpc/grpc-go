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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	"google.golang.org/grpc/internal/envconfig"
)

const (
	envProjectID          = "GOOGLE_CLOUD_PROJECT"
	methodStringRegexpStr = `^([\w./]+)/((?:\w+)|[*])$`
)

var methodStringRegexp = regexp.MustCompile(methodStringRegexpStr)

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

func validateLogEventMethod(methods []string, exclude bool) error {
	for _, method := range methods {
		if method == "*" {
			if exclude {
				return errors.New("cannot have exclude and a '*' wildcard")
			}
			continue
		}
		match := methodStringRegexp.FindStringSubmatch(method)
		if match == nil {
			return fmt.Errorf("invalid method string: %v", method)
		}
	}
	return nil
}

func validateLoggingEvents(config *config) error {
	if config.CloudLogging == nil {
		return nil
	}
	for _, clientRPCEvent := range config.CloudLogging.ClientRPCEvents {
		if err := validateLogEventMethod(clientRPCEvent.Methods, clientRPCEvent.Exclude); err != nil {
			return fmt.Errorf("error in clientRPCEvent method: %v", err)
		}
	}
	for _, serverRPCEvent := range config.CloudLogging.ServerRPCEvents {
		if err := validateLogEventMethod(serverRPCEvent.Methods, serverRPCEvent.Exclude); err != nil {
			return fmt.Errorf("error in serverRPCEvent method: %v", err)
		}
	}
	return nil
}

// unmarshalAndVerifyConfig unmarshals a json string representing an
// observability config into its internal go format, and also verifies the
// configuration's fields for validity.
func unmarshalAndVerifyConfig(rawJSON json.RawMessage) (*config, error) {
	var config config
	if err := json.Unmarshal(rawJSON, &config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if err := validateLoggingEvents(&config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if config.CloudTrace != nil && (config.CloudTrace.SamplingRate > 1 || config.CloudTrace.SamplingRate < 0) {
		return nil, fmt.Errorf("error parsing observability config: invalid cloud trace sampling rate %v", config.CloudTrace.SamplingRate)
	}
	logger.Infof("Parsed ObservabilityConfig: %+v", &config)
	return &config, nil
}

func parseObservabilityConfig() (*config, error) {
	if f := envconfig.ObservabilityConfigFile; f != "" {
		if envconfig.ObservabilityConfig != "" {
			logger.Warning("Ignoring GRPC_GCP_OBSERVABILITY_CONFIG and using GRPC_GCP_OBSERVABILITY_CONFIG_FILE contents.")
		}
		content, err := ioutil.ReadFile(f) // TODO: Switch to os.ReadFile once dropped support for go 1.15
		if err != nil {
			return nil, fmt.Errorf("error reading observability configuration file %q: %v", f, err)
		}
		return unmarshalAndVerifyConfig(content)
	} else if envconfig.ObservabilityConfig != "" {
		return unmarshalAndVerifyConfig([]byte(envconfig.ObservabilityConfig))
	}
	// If the ENV var doesn't exist, do nothing
	return nil, nil
}

func ensureProjectIDInObservabilityConfig(ctx context.Context, config *config) error {
	if config.ProjectID == "" {
		// Try to fetch the GCP project id
		projectID := fetchDefaultProjectID(ctx)
		if projectID == "" {
			return fmt.Errorf("empty destination project ID")
		}
		config.ProjectID = projectID
	}
	return nil
}

type clientRPCEvents struct {
	// Methods is a list of strings which can select a group of methods. By
	// default, the list is empty, matching no methods.
	//
	// The value of the method is in the form of <service>/<method>.
	//
	// "*" is accepted as a wildcard for:
	//    1. The method name. If the value is <service>/*, it matches all
	//    methods in the specified service.
	//    2. The whole value of the field which matches any <service>/<method>.
	//    It’s not supported when Exclude is true.
	//    3. The * wildcard cannot be used on the service name independently,
	//    */<method> is not supported.
	//
	// The service name, when specified, must be the fully qualified service
	// name, including the package name.
	//
	// Examples:
	//    1."goo.Foo/Bar" selects only the method "Bar" from service "goo.Foo",
	//    here “goo” is the package name.
	//    2."goo.Foo/*" selects all methods from service "goo.Foo"
	//    3. "*" selects all methods from all services.
	Methods []string `json:"methods,omitempty"`
	// Exclude represents whether the methods denoted by Methods should be
	// excluded from logging. The default value is false, meaning the methods
	// denoted by Methods are included in the logging. If Exclude is true, the
	// wildcard `*` cannot be used as value of an entry in Methods.
	Exclude bool `json:"exclude,omitempty"`
	// MaxMetadataBytes is the maximum number of bytes of each header to log. If
	// the size of the metadata is greater than the defined limit, content past
	// the limit will be truncated. The default value is 0.
	MaxMetadataBytes int `json:"max_metadata_bytes"`
	// MaxMessageBytes is the maximum number of bytes of each message to log. If
	// the size of the message is greater than the defined limit, content past
	// the limit will be truncated. The default value is 0.
	MaxMessageBytes int `json:"max_message_bytes"`
}

type serverRPCEvents struct {
	// Methods is a list of strings which can select a group of methods. By
	// default, the list is empty, matching no methods.
	//
	// The value of the method is in the form of <service>/<method>.
	//
	// "*" is accepted as a wildcard for:
	//    1. The method name. If the value is <service>/*, it matches all
	//    methods in the specified service.
	//    2. The whole value of the field which matches any <service>/<method>.
	//    It’s not supported when Exclude is true.
	//    3. The * wildcard cannot be used on the service name independently,
	//    */<method> is not supported.
	//
	// The service name, when specified, must be the fully qualified service
	// name, including the package name.
	//
	// Examples:
	//    1."goo.Foo/Bar" selects only the method "Bar" from service "goo.Foo",
	//    here “goo” is the package name.
	//    2."goo.Foo/*" selects all methods from service "goo.Foo"
	//    3. "*" selects all methods from all services.
	Methods []string `json:"methods,omitempty"`
	// Exclude represents whether the methods denoted by Methods should be
	// excluded from logging. The default value is false, meaning the methods
	// denoted by Methods are included in the logging. If Exclude is true, the
	// wildcard `*` cannot be used as value of an entry in Methods.
	Exclude bool `json:"exclude,omitempty"`
	// MaxMetadataBytes is the maximum number of bytes of each header to log. If
	// the size of the metadata is greater than the defined limit, content past
	// the limit will be truncated. The default value is 0.
	MaxMetadataBytes int `json:"max_metadata_bytes"`
	// MaxMessageBytes is the maximum number of bytes of each message to log. If
	// the size of the message is greater than the defined limit, content past
	// the limit will be truncated. The default value is 0.
	MaxMessageBytes int `json:"max_message_bytes"`
}

type cloudLogging struct {
	// ClientRPCEvents represents the configuration for outgoing RPC's from the
	// binary. The client_rpc_events configs are evaluated in text order, the
	// first one matched is used. If an RPC doesn't match an entry, it will
	// continue on to the next entry in the list.
	ClientRPCEvents []clientRPCEvents `json:"client_rpc_events,omitempty"`

	// ServerRPCEvents represents the configuration for incoming RPC's to the
	// binary. The server_rpc_events configs are evaluated in text order, the
	// first one matched is used. If an RPC doesn't match an entry, it will
	// continue on to the next entry in the list.
	ServerRPCEvents []serverRPCEvents `json:"server_rpc_events,omitempty"`
}

type cloudMonitoring struct{}

type cloudTrace struct {
	// SamplingRate is the global setting that controls the probability of a RPC
	// being traced. For example, 0.05 means there is a 5% chance for a RPC to
	// be traced, 1.0 means trace every call, 0 means don’t start new traces. By
	// default, the sampling_rate is 0.
	SamplingRate float64 `json:"sampling_rate,omitempty"`
}

type config struct {
	// ProjectID is the destination GCP project identifier for uploading log
	// entries. If empty, the gRPC Observability plugin will attempt to fetch
	// the project_id from the GCP environment variables, or from the default
	// credentials. If not found, the observability init functions will return
	// an error.
	ProjectID string `json:"project_id,omitempty"`
	// CloudLogging defines the logging options. If not present, logging is disabled.
	CloudLogging *cloudLogging `json:"cloud_logging,omitempty"`
	// CloudMonitoring determines whether or not metrics are enabled based on
	// whether it is present or not. If present, monitoring will be enabled, if
	// not present, monitoring is disabled.
	CloudMonitoring *cloudMonitoring `json:"cloud_monitoring,omitempty"`
	// CloudTrace defines the tracing options. When present, tracing is enabled
	// with default configurations. When absent, the tracing is disabled.
	CloudTrace *cloudTrace `json:"cloud_trace,omitempty"`
	// Labels are applied to cloud logging, monitoring, and trace.
	Labels map[string]string `json:"labels,omitempty"`
}
