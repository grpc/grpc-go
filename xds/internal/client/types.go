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
	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
)

type adsStream adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesClient

type resourceType int

const (
	ldsResource resourceType = iota
	rdsResource
	cdsResource
	edsResource
)

const (
	listenerURL = "type.googleapis.com/envoy.api.v2.Listener"
	routeURL    = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	clusterURL  = "type.googleapis.com/envoy.api.v2.Cluster"
	endpointURL = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
)

const (
	// Using raw constants here instead of enums because it seems to hard to
	// get that to work with atomic operations which expect int* operands.
	watchEnqueued  = 0
	watchCancelled = 1
	watchStarted   = 2
)

type watchInfo struct {
	wType    resourceType
	target   []string
	state    int32
	callback interface{}
}

type ldsUpdate struct {
	routeName string
}

type ldsCallback func(ldsUpdate, error)

type rdsUpdate struct {
	cluster string
}

type rdsCallback func(rdsUpdate, error)
