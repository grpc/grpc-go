/*
 *
 * Copyright 2026 gRPC authors.
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

// Package autosharding implements the autosharding load balancing policy.
package autosharding

import (
	"encoding/json"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the autosharding balancer.
const Name = "autosharding_experimental"

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Name() string {
	return Name
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(s)
}

func (bb) Build(balancer.ClientConn, balancer.BuildOptions) balancer.Balancer {
	return &autoshardingBalancer{}
}

// endpointState represents the state associated with an endpoint in the LB policy.
//
//lint:ignore U1000 Struct fields planned for future implementation
type endpointState struct {
	index      int                         // Index of the endpoint within the NR update
	endpoint   resolver.Endpoint           // The actual endpoint returned by the NR
	childState endpointsharding.ChildState // State as reported by the child policy
}

type autoshardingBalancer struct {
	balancer.Balancer
}
