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

package client

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"google.golang.org/grpc/xds/internal"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

// OverloadDropConfig contains the config to drop overloads.
type OverloadDropConfig struct {
	Category    string
	Numerator   uint32
	Denominator uint32
}

// EndpointHealthStatus represents the health status of an endpoint.
type EndpointHealthStatus int32

const (
	// EndpointHealthStatusUnknown represents HealthStatus UNKNOWN.
	EndpointHealthStatusUnknown EndpointHealthStatus = iota
	// EndpointHealthStatusHealthy represents HealthStatus HEALTHY.
	EndpointHealthStatusHealthy
	// EndpointHealthStatusUnhealthy represents HealthStatus UNHEALTHY.
	EndpointHealthStatusUnhealthy
	// EndpointHealthStatusDraining represents HealthStatus DRAINING.
	EndpointHealthStatusDraining
	// EndpointHealthStatusTimeout represents HealthStatus TIMEOUT.
	EndpointHealthStatusTimeout
	// EndpointHealthStatusDegraded represents HealthStatus DEGRADED.
	EndpointHealthStatusDegraded
)

// Endpoint contains information of an endpoint.
type Endpoint struct {
	Address      string
	HealthStatus EndpointHealthStatus
	Weight       uint32
}

// Locality contains information of a locality.
type Locality struct {
	Endpoints []Endpoint
	ID        internal.LocalityID
	Priority  uint32
	Weight    uint32
}

// EndpointsUpdate contains an EDS update.
type EndpointsUpdate struct {
	Drops      []OverloadDropConfig
	Localities []Locality
}

// WatchEndpoints uses EDS to discover endpoints in the provided clusterName.
//
// WatchEndpoints can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) WatchEndpoints(clusterName string, cb func(EndpointsUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		typeURL:     edsURL,
		target:      clusterName,
		edsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		c.scheduleCallback(wi, EndpointsUpdate{}, fmt.Errorf("xds: EDS target %s not found, watcher timeout", clusterName))
	})
	return c.watch(wi)
}

// BuildEndpointsUpdate builds EndpointsUpdate.
func BuildEndpointsUpdate(dropPercents []uint32) *EndpointsUpdate {
	ret := &EndpointsUpdate{}

	for i, d := range dropPercents {
		drop := OverloadDropConfig{
			Category:    fmt.Sprintf("test-drop-%d", i),
			Numerator:   d,
			Denominator: 100,
		}
		ret.Drops = append(ret.Drops, drop)
	}
	return ret
}

// AddLocality adds a locality to the EndpointsUpdate.
func (eu *EndpointsUpdate) AddLocality(subzone string, weight uint32, priority uint32, addrsWithPort []string, healths []corepb.HealthStatus) {
	priorities := make(map[uint32]struct{})

	var lbEndPoints []Endpoint
	for i, a := range addrsWithPort {
		_, portStr, err := net.SplitHostPort(a)
		if err != nil {
			panic("failed to split " + a)
		}
		_, err = strconv.Atoi(portStr)
		if err != nil {
			panic("failed to atoi " + portStr)
		}

		ep := Endpoint{
			Address: a,
			Weight:  weight,
		}
		if healths != nil {
			if i < len(healths) {
				ep.HealthStatus = EndpointHealthStatus(healths[i])
			}
		}
		lbEndPoints = append(lbEndPoints, ep)

		var lid internal.LocalityID
		if subzone != "" {
			lid = internal.LocalityID{
				Region:  "",
				Zone:    "",
				SubZone: subzone,
			}
		}

		priorities[priority] = struct{}{}
		eu.Localities = append(eu.Localities, Locality{
			ID:        lid,
			Endpoints: lbEndPoints,
			Weight:    weight,
			Priority:  priority,
		})
	}
}
