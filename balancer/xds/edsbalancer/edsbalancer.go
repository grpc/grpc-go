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

// Package edsbalancer ...
package edsbalancer

// TODO: this file is used as a place holder. It should be deleted after edsbalancer implementation
// is merged.

import (
	"encoding/json"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
)

type DummyEdsBalancer struct{}

func (d *DummyEdsBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	panic("implement me")
}

func (d *DummyEdsBalancer) HandleResolvedAddrs([]resolver.Address, error) {
	panic("implement me")
}

func (d *DummyEdsBalancer) Close() {
	panic("implement me")
}

func (d *DummyEdsBalancer) HandleEDSResponse(edsResp *v2.ClusterLoadAssignment) {
	panic("implement me")
}

func (d *DummyEdsBalancer) HandleChildPolicy(name string, config json.RawMessage) {
	panic("implement me")
}

// NewXDSBalancer creates an edsBalancer
func NewXDSBalancer(cc balancer.ClientConn) *DummyEdsBalancer {
	return &DummyEdsBalancer{}
}
