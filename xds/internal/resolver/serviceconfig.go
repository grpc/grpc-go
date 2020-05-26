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

package resolver

import (
	"encoding/json"
	"fmt"

	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	cdsName            = "cds_experimental"
	weightedTargetName = "weighted_target_experimental"
)

type serviceConfig struct {
	LoadBalancingConfig balancerConfig `json:"loadBalancingConfig"`
}

type balancerConfig []map[string]interface{}

func newBalancerConfig(name string, config interface{}) balancerConfig {
	return []map[string]interface{}{{name: config}}
}

type weightedCDSBalancerConfig struct {
	Targets map[string]cdsWithWeight `json:"targets"`
}

type cdsWithWeight struct {
	Weight      uint32         `json:"weight"`
	ChildPolicy balancerConfig `json:"childPolicy"`
}

type cdsBalancerConfig struct {
	Cluster string `json:"cluster"`
}

func serviceUpdateToJSON(su xdsclient.ServiceUpdate) (string, error) {
	// Even if WeightedCluster has only one entry, we still use weighted_target
	// as top level balancer, to avoid switching top policy between CDS and
	// weighted_target, causing TCP connection to be recreated.
	targets := make(map[string]cdsWithWeight)
	for name, weight := range su.WeightedCluster {
		targets[name] = cdsWithWeight{
			Weight:      weight,
			ChildPolicy: newBalancerConfig(cdsName, cdsBalancerConfig{Cluster: name}),
		}
	}

	sc := serviceConfig{
		LoadBalancingConfig: newBalancerConfig(
			weightedTargetName, weightedCDSBalancerConfig{
				Targets: targets,
			},
		),
	}
	bs, err := json.Marshal(sc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal json: %v", err)
	}
	return string(bs), nil
}
