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

// Package autosharding implements the autosharding load balancing policy.
package autosharding

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/balancer"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the autosharding balancer.
const Name = "autosharding_experimental"

func init() {
	balancer.Register(bb{})
}

// lbConfig is the balancer config for the autosharding balancer.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	ChannelFactoryKey        string                  `json:"channelFactoryKey,omitempty"`
	AutoShardingTarget       string                  `json:"autoshardingTarget,omitempty"`
	KeyHeaderName            string                  `json:"keyHeaderName,omitempty"`
	EnableFallback           bool                    `json:"enableFallback,omitempty"`
	InitialAssignmentTimeout iserviceconfig.Duration `json:"initialAssignmentTimeout,omitempty"`
}

type bb struct{}

func (bb) Name() string {
	return Name
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &lbConfig{InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second)}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("autosharding: unable to unmarshal LBConfig: %v", err)
	}
	if lbConfig.ChannelFactoryKey == "" {
		return nil, fmt.Errorf("autosharding: channel_factory_key field is required")
	}
	if lbConfig.AutoShardingTarget == "" {
		return nil, fmt.Errorf("autosharding: autosharding_target field is required")
	}
	if lbConfig.KeyHeaderName == "" {
		return nil, fmt.Errorf("autosharding: key_header_name field is required")
	}
	return lbConfig, nil
}

func (bb) Build(balancer.ClientConn, balancer.BuildOptions) balancer.Balancer {
	return &autoshardingBalancer{}
}

type autoshardingBalancer struct {
	balancer.Balancer
}
