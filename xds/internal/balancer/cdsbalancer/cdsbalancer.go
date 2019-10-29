/*
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
 */

// Package cdsbalancer implements a balancer to handle CDS responses.
package cdsbalancer

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const cdsLBPolicyName = "experimental_cds"

func init() {
	balancer.Register(cdsBB{})
}

// cdsBB (short for cdsBalancerBuilder) implements the balancer.Builder
// interface to help build a cdsBalancer.
// It also implements the balancer.ConfigParser interface to help parse the
// JSON service config, to be passed to the cdsBalancer.
type cdsBB struct{}

func (cdsBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return &cdsBalancer{cc: cc}
}

func (cdsBB) Name() string {
	return cdsLBPolicyName
}

func (cdsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg xdsinternal.LBConfigCDS
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("unable to unmarshal balancer config: %s", string(c))
	}
	return &cfg, nil
}

type cdsBalancer struct {
	cc     balancer.ClientConn
	client *xdsclient.Client
}

// TODO: Implement these methods.
func (b *cdsBalancer) UpdateClientConnState(balancer.ClientConnState) error       { return nil }
func (b *cdsBalancer) ResolverError(error)                                        {}
func (b *cdsBalancer) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {}
func (b *cdsBalancer) Close()                                                     {}

func (b *cdsBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	grpclog.Error("UpdateSubConnState should be called instead of HandleSubConnStateChange")
}

func (b *cdsBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	grpclog.Error("UpdateClientConnState should be called instead of HandleResolvedAddrs")
}
