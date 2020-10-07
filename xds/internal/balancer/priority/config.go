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

package priority

import (
	"encoding/json"
	"fmt"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

type child struct {
	Config *internalserviceconfig.BalancerConfig
}

type lbConfig struct {
	serviceconfig.LoadBalancingConfig

	Children   map[string]*child
	Priorities []string
}

func parseConfig(c json.RawMessage) (*lbConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, err
	}

	prioritiesSet := make(map[string]bool)
	for _, name := range cfg.Priorities {
		if _, ok := cfg.Children[name]; !ok {
			return nil, fmt.Errorf("child name %v is not found in children %+v", name, cfg.Children)
		}
		prioritiesSet[name] = true
	}
	for name := range cfg.Children {
		if _, ok := prioritiesSet[name]; !ok {
			return nil, fmt.Errorf("child with name %v is not used in priorities %+v", name, cfg.Children)
		}
	}
	return &cfg, nil
}
