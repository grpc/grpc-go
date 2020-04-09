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
	ldsURL = "type.googleapis.com/envoy.api.v2.Listener"
	rdsURL = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	cdsURL = "type.googleapis.com/envoy.api.v2.Cluster"
	edsURL = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
)

// watchState is an enum to represent the state of a watch call.
type watchState int

const (
	watchEnqueued watchState = iota
	watchCancelled
	watchStarted
)

// watchInfo holds all the information about a watch call.
type watchInfo struct {
	typeURL string
	target  []string
	state   watchState

	ldsCallback ldsCallbackFunc
	rdsCallback rdsCallbackFunc
	cdsCallback cdsCallbackFunc
	edsCallback edsCallbackFunc
	expiryTimer *time.Timer
}

// cancel marks the state as cancelled, and also stops the expiry timer.
func (wi *watchInfo) cancel() {
	wi.state = watchCancelled
	if wi.expiryTimer != nil {
		wi.expiryTimer.Stop()
	}
}

// stopTimer stops the expiry timer without cancelling the watch.
func (wi *watchInfo) stopTimer() {
	if wi.expiryTimer != nil {
		wi.expiryTimer.Stop()
	}
}

type ackInfo struct {
	typeURL string
	version string // NACK if version is an empty string.
	nonce   string
	// ACK/NACK are tagged with the stream it's for. When the stream is down,
	// all the ACK/NACK for this stream will be dropped, and the version/nonce
	// won't be updated.
	stream adsStream
}

type ldsUpdate struct {
	routeName string
}
type ldsCallbackFunc func(ldsUpdate, error)

type rdsUpdate struct {
	clusterName string
}
type rdsCallbackFunc func(rdsUpdate, error)

// CDSUpdate contains information from a received CDS response, which is of
// interest to the registered CDS watcher.
type CDSUpdate struct {
	// ServiceName is the service name corresponding to the clusterName which
	// is being watched for through CDS.
	ServiceName string
	// EnableLRS indicates whether or not load should be reported through LRS.
	EnableLRS bool
}
type cdsCallbackFunc func(CDSUpdate, error)

type edsCallbackFunc func(*EDSUpdate, error)
