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

package xdsclient

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type clusterRequestsCounter struct {
	mu       sync.Mutex
	clusters map[string]*ClusterRequestsCounter
}

var src = &clusterRequestsCounter{
	clusters: make(map[string]*ClusterRequestsCounter),
}

// ClusterRequestsCounter is used to track the total inflight requests for a
// service with the provided name.
type ClusterRequestsCounter struct {
	ClusterName string
	numRequests uint32
}

// GetClusterRequestsCounter returns the ClusterRequestsCounter with the
// provided serviceName. If one does not exist, it creates it.
func GetClusterRequestsCounter(clusterName string) *ClusterRequestsCounter {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.clusters[clusterName]
	if !ok {
		c = &ClusterRequestsCounter{ClusterName: clusterName}
		src.clusters[clusterName] = c
	}
	return c
}

// StartRequest starts a request for a cluster, incrementing its number of
// requests by 1. Returns an error if the max number of requests is exceeded.
func (c *ClusterRequestsCounter) StartRequest(max uint32) error {
	// Note that during race, the limits could be exceeded. This is allowed:
	// "Since the implementation is eventually consistent, races between threads
	// may allow limits to be potentially exceeded."
	// https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/circuit_breaking#arch-overview-circuit-break.
	if atomic.LoadUint32(&c.numRequests) >= max {
		return fmt.Errorf("max requests %v exceeded on service %v", max, c.ClusterName)
	}
	atomic.AddUint32(&c.numRequests, 1)
	return nil
}

// EndRequest ends a request for a service, decrementing its number of requests
// by 1.
func (c *ClusterRequestsCounter) EndRequest() {
	atomic.AddUint32(&c.numRequests, ^uint32(0))
}

// ClearCounterForTesting clears the counter for the service. Should be only
// used in tests.
func ClearCounterForTesting(clusterName string) {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.clusters[clusterName]
	if !ok {
		return
	}
	c.numRequests = 0
}

// ClearAllCountersForTesting clears all the counters. Should be only used in
// tests.
func ClearAllCountersForTesting() {
	src.mu.Lock()
	defer src.mu.Unlock()
	src.clusters = make(map[string]*ClusterRequestsCounter)
}
