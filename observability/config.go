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
	"os"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	configpb "google.golang.org/grpc/observability/internal/config"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	envKeyObservabilityConfig = "GRPC_CONFIG_OBSERVABILITY"
	envProjectID              = "GOOGLE_CLOUD_PROJECT"
)

// fetchDefaultProjectID fetches the default GCP project id from environment.
func fetchDefaultProjectID(ctx context.Context) string {
	// Step 1: Check ENV var
	if s := os.Getenv(envProjectID); s != "" {
		logger.Infof("Found project ID from env GOOGLE_CLOUD_PROJECT: %v", s)
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

func parseObservabilityConfig() *configpb.ObservabilityConfig {
	// Parse the config from ENV var
	if content := os.Getenv(envKeyObservabilityConfig); content != "" {
		var config configpb.ObservabilityConfig
		if err := protojson.Unmarshal([]byte(content), &config); err != nil {
			logger.Warningf("Error parsing observability config from env GRPC_CONFIG_OBSERVABILITY: %v", err)
			return nil
		}
		logger.Infof("Parsed ObservabilityConfig: %+v", &config)
		return &config
	}
	// If the ENV var doesn't exist, do nothing
	return nil
}

func maybeUpdateProjectIDInObservabilityConfig(ctx context.Context, config *configpb.ObservabilityConfig) {
	if config == nil {
		return
	}
	if config.GetExporterConfig().GetProjectId() != "" {
		// User already specified project ID, do nothing
		return
	}
	// Try to fetch the GCP project id
	projectID := fetchDefaultProjectID(ctx)
	if projectID == "" {
		// No GCP project id found in well-known sources
		return
	}
	if config.GetExporterConfig() == nil {
		config.ExporterConfig = &configpb.ObservabilityConfig_ExporterConfig{
			ProjectId: projectID,
		}
	} else if config.GetExporterConfig().GetProjectId() != "" {
		config.ExporterConfig.ProjectId = projectID
	}
}
