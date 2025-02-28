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
	"encoding/json"
	"fmt"
	"sort"

	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/hierarchy"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/clusterimpl"
	"google.golang.org/grpc/xds/internal/balancer/outlierdetection"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

const million = 1000000

// priorityConfig is config for one priority. For example, if there's an EDS and a
// DNS, the priority list will be [priorityConfig{EDS}, priorityConfig{DNS}].
//
// Each priorityConfig corresponds to one discovery mechanism from the LBConfig
// generated by the CDS balancer. The CDS balancer resolves the cluster name to
// an ordered list of discovery mechanisms (if the top cluster is an aggregated
// cluster), one for each underlying cluster.
type priorityConfig struct {
	mechanism DiscoveryMechanism
	// edsResp is set only if type is EDS.
	edsResp xdsresource.EndpointsUpdate
	// endpoints is set only if type is DNS.
	endpoints []resolver.Endpoint
	// Each discovery mechanism has a name generator so that the child policies
	// can reuse names between updates (EDS updates for example).
	childNameGen *nameGenerator
}

// buildPriorityConfigJSON builds balancer config for the passed in
// priorities.
//
// The built tree of balancers (see test for the output struct).
//
//	          ┌────────┐
//	          │priority│
//	          └┬──────┬┘
//	           │      │
//	┌──────────▼─┐  ┌─▼──────────┐
//	│cluster_impl│  │cluster_impl│
//	└──────┬─────┘  └─────┬──────┘
//	       │              │
//	┌──────▼─────┐  ┌─────▼──────┐
//	│xDSLBPolicy │  │xDSLBPolicy │ (Locality and Endpoint picking layer)
//	└────────────┘  └────────────┘
func buildPriorityConfigJSON(priorities []priorityConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) ([]byte, []resolver.Endpoint, error) {
	pc, endpoints, err := buildPriorityConfig(priorities, xdsLBPolicy)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build priority config: %v", err)
	}
	ret, err := json.Marshal(pc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal built priority config struct into json: %v", err)
	}
	return ret, endpoints, nil
}

func buildPriorityConfig(priorities []priorityConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) (*priority.LBConfig, []resolver.Endpoint, error) {
	var (
		retConfig    = &priority.LBConfig{Children: make(map[string]*priority.Child)}
		retEndpoints []resolver.Endpoint
	)
	for _, p := range priorities {
		switch p.mechanism.Type {
		case DiscoveryMechanismTypeEDS:
			names, configs, endpoints, err := buildClusterImplConfigForEDS(p.childNameGen, p.edsResp, p.mechanism, xdsLBPolicy)
			if err != nil {
				return nil, nil, err
			}
			retConfig.Priorities = append(retConfig.Priorities, names...)
			retEndpoints = append(retEndpoints, endpoints...)
			odCfgs := convertClusterImplMapToOutlierDetection(configs, p.mechanism.outlierDetection)
			for n, c := range odCfgs {
				retConfig.Children[n] = &priority.Child{
					Config: &internalserviceconfig.BalancerConfig{Name: outlierdetection.Name, Config: c},
					// Ignore all re-resolution from EDS children.
					IgnoreReresolutionRequests: true,
				}
			}
			continue
		case DiscoveryMechanismTypeLogicalDNS:
			name, config, endpoints := buildClusterImplConfigForDNS(p.childNameGen, p.endpoints, p.mechanism)
			retConfig.Priorities = append(retConfig.Priorities, name)
			retEndpoints = append(retEndpoints, endpoints...)
			odCfg := makeClusterImplOutlierDetectionChild(config, p.mechanism.outlierDetection)
			retConfig.Children[name] = &priority.Child{
				Config: &internalserviceconfig.BalancerConfig{Name: outlierdetection.Name, Config: odCfg},
				// Not ignore re-resolution from DNS children, they will trigger
				// DNS to re-resolve.
				IgnoreReresolutionRequests: false,
			}
			continue
		}
	}
	return retConfig, retEndpoints, nil
}

func convertClusterImplMapToOutlierDetection(ciCfgs map[string]*clusterimpl.LBConfig, odCfg outlierdetection.LBConfig) map[string]*outlierdetection.LBConfig {
	odCfgs := make(map[string]*outlierdetection.LBConfig, len(ciCfgs))
	for n, c := range ciCfgs {
		odCfgs[n] = makeClusterImplOutlierDetectionChild(c, odCfg)
	}
	return odCfgs
}

func makeClusterImplOutlierDetectionChild(ciCfg *clusterimpl.LBConfig, odCfg outlierdetection.LBConfig) *outlierdetection.LBConfig {
	odCfgRet := odCfg
	odCfgRet.ChildPolicy = &internalserviceconfig.BalancerConfig{Name: clusterimpl.Name, Config: ciCfg}
	return &odCfgRet
}

func buildClusterImplConfigForDNS(g *nameGenerator, endpoints []resolver.Endpoint, mechanism DiscoveryMechanism) (string, *clusterimpl.LBConfig, []resolver.Endpoint) {
	// Endpoint picking policy for DNS is hardcoded to pick_first.
	const childPolicy = "pick_first"
	retEndpoints := make([]resolver.Endpoint, len(endpoints))
	pName := fmt.Sprintf("priority-%v", g.prefix)
	for i, e := range endpoints {
		retEndpoints[i] = hierarchy.SetInEndpoint(e, []string{pName})
		// Copy the nested address field as slice fields are shared by the
		// iteration variable and the original slice.
		retEndpoints[i].Addresses = append([]resolver.Address{}, e.Addresses...)
	}
	return pName, &clusterimpl.LBConfig{
		Cluster:         mechanism.Cluster,
		TelemetryLabels: mechanism.TelemetryLabels,
		ChildPolicy:     &internalserviceconfig.BalancerConfig{Name: childPolicy},
	}, retEndpoints
}

// buildClusterImplConfigForEDS returns a list of cluster_impl configs, one for
// each priority, sorted by priority, and the addresses for each priority (with
// hierarchy attributes set).
//
// For example, if there are two priorities, the returned values will be
// - ["p0", "p1"]
// - map{"p0":p0_config, "p1":p1_config}
// - [p0_address_0, p0_address_1, p1_address_0, p1_address_1]
//   - p0 addresses' hierarchy attributes are set to p0
func buildClusterImplConfigForEDS(g *nameGenerator, edsResp xdsresource.EndpointsUpdate, mechanism DiscoveryMechanism, xdsLBPolicy *internalserviceconfig.BalancerConfig) ([]string, map[string]*clusterimpl.LBConfig, []resolver.Endpoint, error) {
	drops := make([]clusterimpl.DropConfig, 0, len(edsResp.Drops))
	for _, d := range edsResp.Drops {
		drops = append(drops, clusterimpl.DropConfig{
			Category:           d.Category,
			RequestsPerMillion: d.Numerator * million / d.Denominator,
		})
	}

	// Localities of length 0 is triggered by an NACK or resource-not-found
	// error before update, or an empty localities list in an update. In either
	// case want to create a priority, and send down empty address list, causing
	// TF for that priority. "If any discovery mechanism instance experiences an
	// error retrieving data, and it has not previously reported any results, it
	// should report a result that is a single priority with no endpoints." -
	// A37
	priorities := [][]xdsresource.Locality{{}}
	if len(edsResp.Localities) != 0 {
		priorities = groupLocalitiesByPriority(edsResp.Localities)
	}
	retNames := g.generate(priorities)
	retConfigs := make(map[string]*clusterimpl.LBConfig, len(retNames))
	var retEndpoints []resolver.Endpoint
	for i, pName := range retNames {
		priorityLocalities := priorities[i]
		cfg, endpoints, err := priorityLocalitiesToClusterImpl(priorityLocalities, pName, mechanism, drops, xdsLBPolicy)
		if err != nil {
			return nil, nil, nil, err
		}
		retConfigs[pName] = cfg
		retEndpoints = append(retEndpoints, endpoints...)
	}
	return retNames, retConfigs, retEndpoints, nil
}

// groupLocalitiesByPriority returns the localities grouped by priority.
//
// The returned list is sorted from higher priority to lower. Each item in the
// list is a group of localities.
//
// For example, for L0-p0, L1-p0, L2-p1, results will be
// - [[L0, L1], [L2]]
func groupLocalitiesByPriority(localities []xdsresource.Locality) [][]xdsresource.Locality {
	var priorityIntSlice []int
	priorities := make(map[int][]xdsresource.Locality)
	for _, locality := range localities {
		priority := int(locality.Priority)
		priorities[priority] = append(priorities[priority], locality)
		priorityIntSlice = append(priorityIntSlice, priority)
	}
	// Sort the priorities based on the int value, deduplicate, and then turn
	// the sorted list into a string list. This will be child names, in priority
	// order.
	sort.Ints(priorityIntSlice)
	priorityIntSliceDeduped := dedupSortedIntSlice(priorityIntSlice)
	ret := make([][]xdsresource.Locality, 0, len(priorityIntSliceDeduped))
	for _, p := range priorityIntSliceDeduped {
		ret = append(ret, priorities[p])
	}
	return ret
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

// priorityLocalitiesToClusterImpl takes a list of localities (with the same
// priority), and generates a cluster impl policy config, and a list of
// addresses with their path hierarchy set to [priority-name, locality-name], so
// priority and the xDS LB Policy know which child policy each address is for.
func priorityLocalitiesToClusterImpl(localities []xdsresource.Locality, priorityName string, mechanism DiscoveryMechanism, drops []clusterimpl.DropConfig, xdsLBPolicy *internalserviceconfig.BalancerConfig) (*clusterimpl.LBConfig, []resolver.Endpoint, error) {
	var retEndpoints []resolver.Endpoint
	for _, locality := range localities {
		var lw uint32 = 1
		if locality.Weight != 0 {
			lw = locality.Weight
		}
		localityStr, err := locality.ID.ToString()
		if err != nil {
			localityStr = fmt.Sprintf("%+v", locality.ID)
		}
		for _, endpoint := range locality.Endpoints {
			// Filter out all "unhealthy" endpoints (unknown and healthy are
			// both considered to be healthy:
			// https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/core/health_check.proto#envoy-api-enum-core-healthstatus).
			if endpoint.HealthStatus != xdsresource.EndpointHealthStatusHealthy && endpoint.HealthStatus != xdsresource.EndpointHealthStatusUnknown {
				continue
			}
			resolverEndpoint := resolver.Endpoint{}
			for _, as := range endpoint.Addresses {
				resolverEndpoint.Addresses = append(resolverEndpoint.Addresses, resolver.Address{Addr: as})
			}
			resolverEndpoint = hierarchy.SetInEndpoint(resolverEndpoint, []string{priorityName, localityStr})
			resolverEndpoint = internal.SetLocalityIDInEndpoint(resolverEndpoint, locality.ID)
			// "To provide the xds_wrr_locality load balancer information about
			// locality weights received from EDS, the cluster resolver will
			// populate a new locality weight attribute for each address The
			// attribute will have the weight (as an integer) of the locality
			// the address is part of." - A52
			resolverEndpoint = wrrlocality.SetAddrInfoInEndpoint(resolverEndpoint, wrrlocality.AddrInfo{LocalityWeight: lw})
			var ew uint32 = 1
			if endpoint.Weight != 0 {
				ew = endpoint.Weight
			}
			resolverEndpoint = weight.Set(resolverEndpoint, weight.EndpointInfo{Weight: lw * ew})
			retEndpoints = append(retEndpoints, resolverEndpoint)
		}
	}
	return &clusterimpl.LBConfig{
		Cluster:               mechanism.Cluster,
		EDSServiceName:        mechanism.EDSServiceName,
		LoadReportingServer:   mechanism.LoadReportingServer,
		MaxConcurrentRequests: mechanism.MaxConcurrentRequests,
		TelemetryLabels:       mechanism.TelemetryLabels,
		DropCategories:        drops,
		ChildPolicy:           xdsLBPolicy,
	}, retEndpoints, nil
}
