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

	jsonFormatClusterOnly = `{"loadBalancingConfig":[{"cds_experimental":{"Cluster": "%s"}}]}`
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

func serviceUpdateToJSON(su xdsclient.ServiceUpdate) string {
	if su.WeightedCluster == nil {
		// This never happens, because xds_client never sends just the cluster
		// name. If there's one cluster, it puts it in a map, with one entry.
		//
		// This mean we will build a weighted_target balancer even if there's
		// just one cluster. Otherwise, we will need to switch the top policy
		// between weighted_target and CDS. In case when the action changes
		// between one cluster and multiple clusters, changing top level policy
		// means recreating TCP connection every time.
		//
		// Keeping this logic for a while, in case we want to go back to
		// changing top level policy, to make cases with one cluster efficient.
		return fmt.Sprintf(jsonFormatClusterOnly, su.Cluster)
	}
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
		panic(err.Error())
	}
	return string(bs)
}
