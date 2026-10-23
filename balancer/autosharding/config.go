/*
 *
 * Copyright 2026 gRPC authors.
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

package autosharding

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/internal/metadata"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

// LBConfig is the balancer config for the autosharding balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	ChannelFactoryKey        string                  `json:"channelFactoryKey,omitempty"`
	AutoShardingTarget       string                  `json:"autoshardingTarget,omitempty"`
	KeyHeaderName            string                  `json:"keyHeaderName,omitempty"`
	EnableFallback           bool                    `json:"enableFallback,omitempty"`
	InitialAssignmentTimeout iserviceconfig.Duration `json:"initialAssignmentTimeout,omitempty"`
}

func parseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &LBConfig{InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second)}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("autosharding: unable to unmarshal LBConfig: %v", err)
	}
	if lbConfig.ChannelFactoryKey == "" {
		return nil, fmt.Errorf("autosharding: channelFactoryKey field is required")
	}
	if lbConfig.AutoShardingTarget == "" {
		return nil, fmt.Errorf("autosharding: autoshardingTarget field is required")
	}
	if lbConfig.KeyHeaderName == "" {
		return nil, fmt.Errorf("autosharding: keyHeaderName field is required")
	}
	// When parsing the JSON LB configuration, the LB policy must ensure that
	// the key_header_name field contains a valid gRPC header name. If not, the
	// configuration must be rejected - A119.
	if err := metadata.ValidateKey(lbConfig.KeyHeaderName); err != nil {
		return nil, fmt.Errorf("autosharding: invalid keyHeaderName %q: %v", lbConfig.KeyHeaderName, err)
	}
	return lbConfig, nil
}
