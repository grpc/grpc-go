/*
 *
 * Copyright 2019 gRPC authors.
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
	"time"

	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
)

type adsStream adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

const (
	listenerURL = "type.googleapis.com/envoy.api.v2.Listener"
	routeURL    = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	clusterURL  = "type.googleapis.com/envoy.api.v2.Cluster"
	endpointURL = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
)

type resourceType int

const (
	ldsResource resourceType = iota
	rdsResource
	cdsResource
	edsResource
)

type watchState int

const (
	watchEnqueued watchState = iota
	watchCancelled
	watchStarted
)

type watchInfo struct {
	wType       resourceType
	target      []string
	state       watchState
	callback    interface{}
	expiryTimer *time.Timer
}

func (wi *watchInfo) cancel() {
	wi.state = watchCancelled
	if wi.expiryTimer != nil {
		wi.expiryTimer.Stop()
	}
}

type ldsUpdate struct {
	routeName string
}

type ldsCallback func(ldsUpdate, error)

type rdsUpdate struct {
	clusterName string
}

type rdsCallback func(rdsUpdate, error)
