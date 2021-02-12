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

package clusterimpl

import (
	"encoding/json"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

type dropCategory struct {
	Category           string
	RequestsPerMillion uint32
}

// lbConfig is the balancer config for weighted_target.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig

	Cluster                    string
	EDSServiceName             string
	LRSLoadReportingServerName *string
	MaxConcurrentRequests      *uint32
	DropCategories             []dropCategory
	ChildPolicy                *internalserviceconfig.BalancerConfig
}

func parseConfig(c json.RawMessage) (*lbConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func equalDropCategories(a, b []dropCategory) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
