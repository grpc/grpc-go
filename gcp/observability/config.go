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
	"os"
	"regexp"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	configpb "google.golang.org/grpc/gcp/observability/internal/config"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	envObservabilityConfig    = "GRPC_CONFIG_OBSERVABILITY"
	envProjectID              = "GOOGLE_CLOUD_PROJECT"
	logFilterPatternRegexpStr = `^([\w./]+)/((?:\w+)|[*])$`
)

var logFilterPatternRegexp = regexp.MustCompile(logFilterPatternRegexpStr)

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

func validateFilters(config *configpb.ObservabilityConfig) error {
	for _, filter := range config.GetLogFilters() {
		if filter.Pattern == "*" {
			continue
		}
		match := logFilterPatternRegexp.FindStringSubmatch(filter.Pattern)
		if match == nil {
			return fmt.Errorf("invalid log filter pattern: %v", filter.Pattern)
		}
	}
	return nil
}

func parseObservabilityConfig() (*configpb.ObservabilityConfig, error) {
	// Parse the config from ENV var
	if content := os.Getenv(envObservabilityConfig); content != "" {
		var config configpb.ObservabilityConfig
		if err := protojson.Unmarshal([]byte(content), &config); err != nil {
			return nil, fmt.Errorf("error parsing observability config from env %v: %v", envObservabilityConfig, err)
		}
		if err := validateFilters(&config); err != nil {
			return nil, fmt.Errorf("error parsing observability config: %v", err)
		}
		logger.Infof("Parsed ObservabilityConfig: %+v", &config)
		return &config, nil
	}
	// If the ENV var doesn't exist, do nothing
	return nil, nil
}

func ensureProjectIDInObservabilityConfig(ctx context.Context, config *configpb.ObservabilityConfig) error {
	if config.GetDestinationProjectId() == "" {
		// Try to fetch the GCP project id
		projectID := fetchDefaultProjectID(ctx)
		if projectID == "" {
			return fmt.Errorf("empty destination project ID")
		}
		config.DestinationProjectId = projectID
	}
	return nil
}
