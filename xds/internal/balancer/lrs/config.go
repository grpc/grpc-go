/*
 *
 * Copyright 2020 gRPC authors.
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

package lrs

import (
	"encoding/json"
	"fmt"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal"
)

// LBConfig is the balancer config for lrs balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	ClusterName             string                                `json:"clusterName,omitempty"`
	EDSServiceName          string                                `json:"edsServiceName,omitempty"`
	LoadReportingServerName string                                `json:"lrsLoadReportingServerName,omitempty"`
	Locality                *internal.LocalityID                  `json:"locality,omitempty"`
	ChildPolicy             *internalserviceconfig.BalancerConfig `json:"childPolicy,omitempty"`
}

func parseConfig(c json.RawMessage) (*LBConfig, error) {
	var cfg LBConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, err
	}
	if cfg.ClusterName == "" {
		return nil, fmt.Errorf("required ClusterName is not set in %+v", cfg)
	}
	if cfg.Locality == nil {
		return nil, fmt.Errorf("required Locality is not set in %+v", cfg)
	}
	return &cfg, nil
}
