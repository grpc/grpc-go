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

	coptlogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	configpb "google.golang.org/grpc/observability/config"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	EnvKeyObservabilityConfig = "GRPC_OBSERVABILITY_CONFIG"
)

// gcpDefaultCredentials is the JSON loading struct used to get project id.
type gcpDefaultCredentials struct {
	QuotaProjectId string `json:"quota_project_id"`
}

// fetchDefaultProjectId fetches the default GCP project id from environment.
func fetchDefaultProjectId() string {
	// Step 1: Check ENV var
	if s := os.Getenv("GCLOUD_PROJECT_ID"); s != "" {
		return s
	}
	// Step 2: Check default credential
	ctx := context.Background()
	if credentials, err := google.FindDefaultCredentials(ctx, coptlogging.WriteScope); err == nil {
		logger.Infof("found Google Default Credential")
		// Step 2.1: Check if the ProjectID is in the plain view
		if credentials.ProjectID != "" {
			return credentials.ProjectID
		} else if len(credentials.JSON) > 0 {
			// Step 2.2: Check if the JSON form of the credentials has it
			var d gcpDefaultCredentials
			if err := json.Unmarshal(credentials.JSON, &d); err != nil {
				logger.Infof("failed to parse default credentials JSON")
			} else if d.QuotaProjectId != "" {
				return d.QuotaProjectId
			}
		}
	} else {
		logger.Info("failed to locate Google Default Credential: %v", err)
	}
	// No default project ID found
	return ""
}

// parseObservabilityConfig parses and processes the config for observability,
// currently, we only support loading config from static ENV var. But we might
// support dynamic configuration with control plane in future.
func parseObservabilityConfig() *configpb.ObservabilityConfig {
	// Parse the config from ENV var
	var config configpb.ObservabilityConfig
	content := os.Getenv(EnvKeyObservabilityConfig)
	if content != "" {
		if err := protojson.Unmarshal([]byte(content), &config); err != nil {
			logger.Warningf("failed to load observability config from env GRPC_OBSERVABILITY_CONFIG: %s", err)
		}
	}
	// Fill in GCP project id if not present
	if config.Exporter == nil {
		config.Exporter = &configpb.ExporterConfig{
			ProjectId: fetchDefaultProjectId(),
		}
	} else {
		// If any default exporter is required, fill the default project id
		if !config.Exporter.DisableDefaultLoggingExporter || !config.Exporter.DisableDefaultTracingExporter || !config.Exporter.DisableDefaultMetricsExporter {
			if config.Exporter.ProjectId == "" {
				config.Exporter.ProjectId = fetchDefaultProjectId()
			}
		}
	}
	configJSON, _ := protojson.Marshal(&config)
	logger.Infof("Using ObservabilityConfig: %v", string(configJSON))
	return &config
}
