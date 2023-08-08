/*
 *
 * Copyright 2023 gRPC authors.
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
 */

// Package leastrequest implements a least request load balancer.
package leastrequest

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	balancer.Register(bb{})
}

// Name is the name of the least request balancer.
const Name = "least_request_experimental"

// LBConfig is the balancer config for least_request_experimental balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChoiceCount is the number of random SubConns to sample to try and find
	// the one with the Least Request. If unset, defaults to 2. If set to < 2,
	// will become 2, and if set to > 10, will become 10.
	ChoiceCount uint32 `json:"choiceCount,omitempty"`
}

type bb struct{}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &LBConfig{
		ChoiceCount: 2,
	}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("least-request: unable to unmarshal LBConfig: %v", err)
	}
	// "If `choice_count < 2`, the config will be rejected." - A48
	if lbConfig.ChoiceCount < 2 {
		return nil, fmt.Errorf("least-request: lbConfig.choiceCount: %v, must be >= 2", lbConfig.ChoiceCount)
	}
	// "If a LeastRequestLoadBalancingConfig with a choice_count > 10 is
	// received, the least_request_experimental policy will set choice_count =
	// 10." - A48
	if lbConfig.ChoiceCount > 10 {
		lbConfig.ChoiceCount = 10
	}
	return lbConfig, nil
}

func (bb) Name() string {
	return Name
}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	return nil
}
