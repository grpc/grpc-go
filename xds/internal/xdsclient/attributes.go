/*
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

package xdsclient

import (
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

type clientKeyType string

const clientKey = clientKeyType("grpc.xds.internal.client.Client")

// Interface contains a subset of the *Client methods that are needed by the
// balancers.
type Interface interface {
	WatchCluster(string, func(ClusterUpdate, error)) func()
	WatchEndpoints(clusterName string, edsCb func(EndpointsUpdate, error)) (cancel func())
	BootstrapConfig() *bootstrap.Config
	ReportLoad(server string) (*load.Store, func())
	Close()
}

// FromResolverState returns the Client from state, or nil if not present.
func FromResolverState(state resolver.State) Interface {
	cs, _ := state.Attributes.Value(clientKey).(Interface)
	return cs
}

// SetClient sets c in state and returns the new state.
func SetClient(state resolver.State, c Interface) resolver.State {
	state.Attributes = state.Attributes.WithValues(clientKey, c)
	return state
}
