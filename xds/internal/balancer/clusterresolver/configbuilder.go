/*
 *
 * Copyright 2021 gRPC authors.
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

package clusterresolver

import (
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver/balancerconfig"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const million = 1000000

func buildPriorityConfigJSON(edsResp xdsclient.EndpointsUpdate, c *EDSConfig) ([]byte, []resolver.Address, error) {
	var childConfig *internalserviceconfig.BalancerConfig
	if c.ChildPolicy != nil {
		childConfig = &internalserviceconfig.BalancerConfig{Name: c.ChildPolicy.Name}
	}
	return balancerconfig.BuildPriorityConfigJSON(
		[]balancerconfig.PriorityConfig{
			{
				Mechanism: balancerconfig.DiscoveryMechanism{
					Cluster:                 c.ClusterName,
					LoadReportingServerName: c.LrsLoadReportingServerName,
					MaxConcurrentRequests:   c.MaxConcurrentRequests,
					Type:                    balancerconfig.DiscoveryMechanismTypeEDS,
					EDSServiceName:          c.EDSServiceName,
				},
				EDSResp: edsResp,
			},
		}, childConfig,
	)
}
