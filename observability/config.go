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
	"os"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	configpb "google.golang.org/grpc/observability/internal/config"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	envKeyObservabilityConfig = "GRPC_CONFIG_OBSERVABILITY"
)

// gcpDefaultCredentials is the JSON loading struct used to get project id.
type gcpDefaultCredentials struct {
	QuotaProjectID string `json:"quota_project_id"`
}

// fetchDefaultProjectID fetches the default GCP project id from environment.
func fetchDefaultProjectID(ctx context.Context) string {
	// Step 1: Check ENV var
	if s := os.Getenv("GOOGLE_CLOUD_PROJECT"); s != "" {
		return s
	}
	// Step 2: Check default credential
	credentials, err := google.FindDefaultCredentials(ctx, gcplogging.WriteScope)
	if err != nil {
		logger.Info("Failed to locate Google Default Credential: %v", err)
		return ""
	}
	logger.Infof("Found Google Default Credential")
	// Step 2.1: Check if the ProjectID is in the plain view
	if credentials.ProjectID == "" {
		if len(credentials.JSON) > 0 {
			// Step 2.2: Check if the JSON form of the credentials has it
			var d gcpDefaultCredentials
			if err := json.Unmarshal(credentials.JSON, &d); err != nil {
				logger.Infof("Failed to parse default credentials JSON")
				return ""
			} else if d.QuotaProjectID != "" {
				return d.QuotaProjectID
			}
		}
	}
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
	if config.GetExporterConfig() != nil && config.GetExporterConfig().GetProjectId() != "" {
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
		config.GetExporterConfig().ProjectId = projectID
	}
	logger.Infof("Discovered GCP project ID: %v", projectID)
}
