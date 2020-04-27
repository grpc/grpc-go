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

package weightedtarget

import (
	"encoding/json"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

type target struct {
	// Weight is the weight of the child policy.
	Weight uint32
	// ChildPolicy is the child policy and it's config.
	ChildPolicy *internalserviceconfig.BalancerConfig
}

// lbConfig is the balancer config for weighted_target. The proto representation
// is:
//
// message WeightedTargetConfig {
//   message Target {
//     uint32 weight = 1;
//     repeated LoadBalancingConfig child_policy = 2;
//   }
//   map<string, Target> targets = 1;
// }
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	Targets map[string]target
}

func parseConfig(c json.RawMessage) (*lbConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
