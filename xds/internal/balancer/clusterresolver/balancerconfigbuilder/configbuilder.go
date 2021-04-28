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

// Package balancerconfigbuilder contains utility functions to build balancer
// config. The built config will generate a tree of balancers with priority,
// cluster_impl, weighted_target, lrs, and roundrobin.
//
// This is in a subpackage of cluster_resolver so that it can be used by the EDS
// balancer. Eventually we will delete the EDS balancer, and replace it with
// cluster_resolver, then we can move the functions to package cluster_resolver,
// and unexport them.
//
// TODO: move and unexport. Read above.
package balancerconfigbuilder

import (
	"encoding/json"
	"fmt"
	"sort"

	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/internal/hierarchy"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/balancer/weightedtarget"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const million = 1000000

// PriorityConfig is config for one priority. For example, if there an EDS and a
// DNS, the priority list will be [priorityConfig{EDS}, PriorityConfig{DNS}].
//
// Each PriorityConfig corresponds to one discovery mechanism from the LBConfig.
type PriorityConfig struct {
	Mechanism DiscoveryMechanism
	// EDSResp if type is EDS.
	EDSResp xdsclient.EndpointsUpdate
	// Addresses if type is DNS.
	Addresses []string
}

// BuildPriorityConfigMarshalled build balancer config for the passed in priorities.
//
// The built tree of balancers (see test for the output struct).
//
//              ┌────────┐
//              │priority│
//              └┬──────┬┘
//               │      │
//   ┌───────────▼┐    ┌▼───────────┐
//   │cluster_impl│    │cluster_impl│
//   └─────┬──────┘    └─────┬──────┘
//         │                 │
// ┌───────▼───────┐ ┌───────▼───────┐
// │weighted_target│ │weighted_target│
// └───┬───────┬───┘ └───┬───────┬───┘
//     │       │         │       │
//   ┌─▼─┐   ┌─▼─┐     ┌─▼─┐   ┌─▼─┐
//   │LRS│   │LRS│     │LRS│   │LRS│
//   └─┬─┘   └─┬─┘     └─┬─┘   └─┬─┘
//     │       │         │       │
//   ┌─▼─┐   ┌─▼─┐     ┌─▼─┐   ┌─▼─┐
//   │RR │   │RR │     │RR │   │RR │
//   └───┘   └───┘     └───┘   └───┘
//
// If endpointPickingPolicy is nil, roundrobin will be used.
//
// TODO: support setting locality picking policy.
func BuildPriorityConfigMarshalled(priorities []PriorityConfig, endpointPickingPolicy *internalserviceconfig.BalancerConfig) ([]byte, []resolver.Address) {
	pc, addrs := buildPriorityConfig(priorities, endpointPickingPolicy)
	ret, err := json.Marshal(pc)
	if err != nil {
		logger.Warningf("failed to marshal built priority config struct into json: %v", err)
		return nil, nil
	}
	return ret, addrs
}

func buildPriorityConfig(priorities []PriorityConfig, endpointPickingPolicy *internalserviceconfig.BalancerConfig) (*priority.LBConfig, []resolver.Address) {
	var (
		retConfig = &priority.LBConfig{Children: make(map[string]*priority.Child)}
		retAddrs  []resolver.Address
	)
	for i, p := range priorities {
		switch p.Mechanism.Type {
		case DiscoveryMechanismTypeEDS:
			names, configs, addrs := buildClusterImplConfigForEDS(i, p.EDSResp, p.Mechanism, endpointPickingPolicy)
			retConfig.Priorities = append(retConfig.Priorities, names...)
			for n, c := range configs {
				retConfig.Children[n] = &priority.Child{
					Config: &internalserviceconfig.BalancerConfig{
						Name:   clusterimpl.Name,
						Config: c,
					},
					// Ignore all re-resolution from EDS children.
					IgnoreReresolutionRequests: true,
				}
			}
			retAddrs = append(retAddrs, addrs...)
		case DiscoveryMechanismTypeLogicalDNS:
			name, config, addrs := buildClusterImplConfigForDNS(i, p.Addresses)
			retConfig.Priorities = append(retConfig.Priorities, name)
			retConfig.Children[name] = &priority.Child{
				Config: &internalserviceconfig.BalancerConfig{
					Name:   clusterimpl.Name,
					Config: config,
				},
				IgnoreReresolutionRequests: false,
			}
			retAddrs = append(retAddrs, addrs...)
		}
	}
	return retConfig, retAddrs
}

func buildClusterImplConfigForDNS(parentPriority int, addrStrs []string) (string, *clusterimpl.LBConfig, []resolver.Address) {
	// Endpoint picking policy for DNS is hardcoded to pick_first.
	const childPolicy = "pick_first"
	var retAddrs []resolver.Address
	pName := fmt.Sprintf("priority-%v", parentPriority)
	for _, addrStr := range addrStrs {
		retAddrs = append(retAddrs,
			hierarchy.Set(resolver.Address{Addr: addrStr}, []string{pName}),
		)
	}
	return pName, &clusterimpl.LBConfig{
		ChildPolicy: &internalserviceconfig.BalancerConfig{
			Name: childPolicy,
		},
	}, retAddrs
}

// buildClusterImplConfigForEDS returns a list of cluster_impl configs, one for
// each priority, sorted by priority.
func buildClusterImplConfigForEDS(parentPriority int, edsResp xdsclient.EndpointsUpdate, mechanism DiscoveryMechanism, endpointPickingPolicy *internalserviceconfig.BalancerConfig) ([]string, map[string]*clusterimpl.LBConfig, []resolver.Address) {
	var (
		retNames   []string
		retAddrs   []resolver.Address
		retConfigs = make(map[string]*clusterimpl.LBConfig)
	)

	if endpointPickingPolicy == nil {
		endpointPickingPolicy = &internalserviceconfig.BalancerConfig{
			Name: roundrobin.Name,
		}
	}

	cluster := mechanism.Cluster
	edsService := mechanism.EDSServiceName
	lrsServer := mechanism.LoadReportingServerName
	maxRequest := mechanism.MaxConcurrentRequests

	drops := make([]clusterimpl.DropConfig, 0, len(edsResp.Drops))
	for _, d := range edsResp.Drops {
		drops = append(drops, clusterimpl.DropConfig{
			Category:           d.Category,
			RequestsPerMillion: d.Numerator * million / d.Denominator,
		})
	}

	priorityChildNames, priorities := groupLocalitiesByPriority(edsResp.Localities)
	for _, priorityName := range priorityChildNames {
		priorityLocalities := priorities[priorityName]
		// Prepend parent priority to the priority names, to avoid duplicates.
		pName := fmt.Sprintf("priority-%v-%v", parentPriority, priorityName)
		retNames = append(retNames, pName)
		wtConfig, addrs := localitiesToWeightedTarget(priorityLocalities, pName, endpointPickingPolicy, lrsServer, cluster, edsService)
		retConfigs[pName] = &clusterimpl.LBConfig{
			Cluster:        cluster,
			EDSServiceName: edsService,
			ChildPolicy: &internalserviceconfig.BalancerConfig{
				Name:   weightedtarget.Name,
				Config: wtConfig,
			},
			LoadReportingServerName: lrsServer,
			MaxConcurrentRequests:   maxRequest,
			DropCategories:          drops,
		}
		retAddrs = append(retAddrs, addrs...)
	}

	return retNames, retConfigs, retAddrs
}

// groupLocalitiesByPriority returns the localities grouped by locality.
//
// It also returns a list of strings where each string represents a priority,
// and the list is sorted from higher priority to lower priority.
func groupLocalitiesByPriority(localities []xdsclient.Locality) ([]string, map[string][]xdsclient.Locality) {
	var priorityIntSlice []int
	priorities := make(map[string][]xdsclient.Locality)
	for _, locality := range localities {
		if locality.Weight == 0 {
			continue
		}
		priorityName := fmt.Sprintf("%v", locality.Priority)
		priorities[priorityName] = append(priorities[priorityName], locality)
		priorityIntSlice = append(priorityIntSlice, int(locality.Priority))
	}
	// Sort the priorities based on the int value, deduplicate, and then turn
	// the sorted list into a string list. This will be child names, in priority
	// order.
	sort.Ints(priorityIntSlice)
	priorityIntSliceDeduped := dedupSortedIntSlice(priorityIntSlice)
	priorityNameSlice := make([]string, 0, len(priorityIntSliceDeduped))
	for _, p := range priorityIntSliceDeduped {
		priorityNameSlice = append(priorityNameSlice, fmt.Sprintf("%v", p))
	}
	return priorityNameSlice, priorities
}

func dedupSortedIntSlice(a []int) []int {
	if len(a) == 0 {
		return a
	}
	i, j := 0, 1
	for ; j < len(a); j++ {
		if a[i] == a[j] {
			continue
		}
		i++
		if i != j {
			a[i] = a[j]
		}
	}
	return a[:i+1]
}

// localitiesToWeightedTarget takes a list of localities (with the same
// priority), and generates a weighted target config, and list of addresses.
//
// The addresses have path hierarchy set to [p, locality-name], so priority and
// weighted target know which child policy they are for.
func localitiesToWeightedTarget(localities []xdsclient.Locality, priorityName string, childPolicy *internalserviceconfig.BalancerConfig, lrsServer *string, cluster, edsService string) (*weightedtarget.LBConfig, []resolver.Address) {
	weightedTargets := make(map[string]weightedtarget.Target)
	var addrs []resolver.Address
	for _, locality := range localities {
		localityStr, err := locality.ID.ToString()
		if err != nil {
			localityStr = fmt.Sprintf("%+v", locality.ID)
		}

		var child *internalserviceconfig.BalancerConfig
		if lrsServer != nil {
			localityID := locality.ID
			child = &internalserviceconfig.BalancerConfig{
				Name: lrs.Name,
				Config: &lrs.LBConfig{
					ClusterName:             cluster,
					EDSServiceName:          edsService,
					ChildPolicy:             childPolicy,
					LoadReportingServerName: *lrsServer,
					Locality:                &localityID,
				},
			}
		} else {
			// If lrsServer is not set, skip LRS policy.
			child = childPolicy
		}
		weightedTargets[localityStr] = weightedtarget.Target{
			Weight:      locality.Weight,
			ChildPolicy: child,
		}

		for _, endpoint := range locality.Endpoints {
			// Filter out all "unhealthy" endpoints (unknown and
			// healthy are both considered to be healthy:
			// https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/core/health_check.proto#envoy-api-enum-core-healthstatus).
			if endpoint.HealthStatus != xdsclient.EndpointHealthStatusHealthy &&
				endpoint.HealthStatus != xdsclient.EndpointHealthStatusUnknown {
				continue
			}

			addr := resolver.Address{
				Addr: endpoint.Address,
			}
			if childPolicy.Name == weightedroundrobin.Name && endpoint.Weight != 0 {
				ai := weightedroundrobin.AddrInfo{Weight: endpoint.Weight}
				addr = weightedroundrobin.SetAddrInfo(addr, ai)
			}
			addr = hierarchy.Set(addr, []string{priorityName, localityStr})
			addrs = append(addrs, addr)
		}
	}
	return &weightedtarget.LBConfig{
		Targets: weightedTargets,
	}, addrs
}
