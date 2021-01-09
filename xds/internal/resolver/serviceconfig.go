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
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/balancer/clustermanager"
	"google.golang.org/grpc/xds/internal/env"
)

const (
	cdsName               = "cds_experimental"
	xdsClusterManagerName = "xds_cluster_manager_experimental"
)

type serviceConfig struct {
	LoadBalancingConfig balancerConfig `json:"loadBalancingConfig"`
}

type balancerConfig []map[string]interface{}

func newBalancerConfig(name string, config interface{}) balancerConfig {
	return []map[string]interface{}{{name: config}}
}

type cdsBalancerConfig struct {
	Cluster string `json:"cluster"`
}

type xdsChildConfig struct {
	ChildPolicy balancerConfig `json:"childPolicy"`
}

type xdsClusterManagerConfig struct {
	Children map[string]xdsChildConfig `json:"children"`
}

// pruneActiveClusters deletes entries in r.activeClusters with zero
// references.
func (r *xdsResolver) pruneActiveClusters() {
	for cluster, ci := range r.activeClusters {
		if atomic.LoadInt32(&ci.refCount) == 0 {
			delete(r.activeClusters, cluster)
		}
	}
}

// serviceConfigJSON produces a service config in JSON format representing all
// the clusters referenced in activeClusters.  This includes clusters with zero
// references, so they must be pruned first.
func serviceConfigJSON(activeClusters map[string]*clusterInfo) (string, error) {
	// Generate children (all entries in activeClusters).
	children := make(map[string]xdsChildConfig)
	for cluster := range activeClusters {
		children[cluster] = xdsChildConfig{
			ChildPolicy: newBalancerConfig(cdsName, cdsBalancerConfig{Cluster: cluster}),
		}
	}

	sc := serviceConfig{
		LoadBalancingConfig: newBalancerConfig(
			xdsClusterManagerName, xdsClusterManagerConfig{Children: children},
		),
	}

	bs, err := json.Marshal(sc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal json: %v", err)
	}
	return string(bs), nil
}

type route struct {
	m                 *compositeMatcher // converted from route matchers
	clusters          wrr.WRR
	maxStreamDuration time.Duration
}

func (r route) String() string {
	return fmt.Sprintf("%s -> { clusters: %v, maxStreamDuration: %v }", r.m.String(), r.clusters, r.maxStreamDuration)
}

type configSelector struct {
	r        *xdsResolver
	routes   []route
	clusters map[string]*clusterInfo
}

var errNoMatchedRouteFound = status.Errorf(codes.Unavailable, "no matched route was found")

func (cs *configSelector) SelectConfig(rpcInfo iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
	if cs == nil {
		return nil, status.Errorf(codes.Unavailable, "no valid clusters")
	}
	var rt *route
	// Loop through routes in order and select first match.
	for _, r := range cs.routes {
		if r.m.match(rpcInfo) {
			rt = &r
			break
		}
	}
	if rt == nil || rt.clusters == nil {
		return nil, errNoMatchedRouteFound
	}
	cluster, ok := rt.clusters.Next().(string)
	if !ok {
		return nil, status.Errorf(codes.Internal, "error retrieving cluster for match: %v (%T)", cluster, cluster)
	}
	// Add a ref to the selected cluster, as this RPC needs this cluster until
	// it is committed.
	ref := &cs.clusters[cluster].refCount
	atomic.AddInt32(ref, 1)

	config := &iresolver.RPCConfig{
		// Communicate to the LB policy the chosen cluster.
		Context: clustermanager.SetPickedCluster(rpcInfo.Context, cluster),
		OnCommitted: func() {
			// When the RPC is committed, the cluster is no longer required.
			// Decrease its ref.
			if v := atomic.AddInt32(ref, -1); v == 0 {
				// This entry will be removed from activeClusters when
				// producing the service config for the empty update.
				select {
				case cs.r.updateCh <- suWithError{emptyUpdate: true}:
				default:
				}
			}
		},
	}

	if env.TimeoutSupport && rt.maxStreamDuration != 0 {
		config.MethodConfig.Timeout = &rt.maxStreamDuration
	}

	return config, nil
}

// stop decrements refs of all clusters referenced by this config selector.
func (cs *configSelector) stop() {
	// The resolver's old configSelector may be nil.  Handle that here.
	if cs == nil {
		return
	}
	// If any refs drop to zero, we'll need a service config update to delete
	// the cluster.
	needUpdate := false
	// Loops over cs.clusters, but these are pointers to entries in
	// activeClusters.
	for _, ci := range cs.clusters {
		if v := atomic.AddInt32(&ci.refCount, -1); v == 0 {
			needUpdate = true
		}
	}
	// We stop the old config selector immediately after sending a new config
	// selector; we need another update to delete clusters from the config (if
	// we don't have another update pending already).
	if needUpdate {
		select {
		case cs.r.updateCh <- suWithError{emptyUpdate: true}:
		default:
		}
	}
}

// A global for testing.
var newWRR = wrr.NewRandom

// newConfigSelector creates the config selector for su; may add entries to
// r.activeClusters for previously-unseen clusters.
func (r *xdsResolver) newConfigSelector(su serviceUpdate) (*configSelector, error) {
	cs := &configSelector{
		r:        r,
		routes:   make([]route, len(su.routes)),
		clusters: make(map[string]*clusterInfo),
	}

	for i, rt := range su.routes {
		clusters := newWRR()
		for cluster, weight := range rt.Action {
			clusters.Add(cluster, int64(weight))

			// Initialize entries in cs.clusters map, creating entries in
			// r.activeClusters as necessary.  Set to zero as they will be
			// incremented by incRefs.
			ci := r.activeClusters[cluster]
			if ci == nil {
				ci = &clusterInfo{refCount: 0}
				r.activeClusters[cluster] = ci
			}
			cs.clusters[cluster] = ci
		}
		cs.routes[i].clusters = clusters

		var err error
		cs.routes[i].m, err = routeToMatcher(rt)
		if err != nil {
			return nil, err
		}
		if rt.MaxStreamDuration == nil {
			cs.routes[i].maxStreamDuration = su.ldsConfig.maxStreamDuration
		} else {
			cs.routes[i].maxStreamDuration = *rt.MaxStreamDuration
		}
	}

	// Account for this config selector's clusters.  Do this after no further
	// errors may occur.  Note: cs.clusters are pointers to entries in
	// activeClusters.
	for _, ci := range cs.clusters {
		atomic.AddInt32(&ci.refCount, 1)
	}

	return cs, nil
}

type clusterInfo struct {
	// number of references to this cluster; accessed atomically
	refCount int32
}
