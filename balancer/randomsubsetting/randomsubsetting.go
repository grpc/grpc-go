/*
 *
 * Copyright 2025 gRPC authors.
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

// Package randomsubsetting implements the random_subsetting LB policy specified
// here: https://github.com/grpc/proposal/blob/master/A68-random-subsetting.md
//
// To install the LB policy, import this package as:
//
//	import _ "google.golang.org/grpc/balancer/randomsubsetting"
package randomsubsetting

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the random subsetting load balancer.
const Name = "random_subsetting"

var (
	logger   = grpclog.Component(Name)
	HashSeed = uint64(time.Now().UnixNano())
)

func prefixLogger(p *subsettingBalancer) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[random-subsetting-lb %p] ", p))
}

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &subsettingBalancer{
		Balancer: gracefulswitch.NewBalancer(cc, bOpts),
		hashf:    xxhash.NewWithSeed(HashSeed),
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
	return b
}

type lbConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	SubsetSize  uint32                         `json:"subsetSize,omitempty"`
	ChildPolicy *iserviceconfig.BalancerConfig `json:"childPolicy,omitempty"`
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbCfg := &lbConfig{}

	// Ensure that the specified child policy is registered and validates its
	// config, if present.
	if err := json.Unmarshal(s, lbCfg); err != nil {
		return nil, fmt.Errorf("randomsubsetting: unmarshaling configuration: %s, failed: %v", string(s), err)
	}
	if lbCfg.SubsetSize == 0 {
		return nil, fmt.Errorf("randomsubsetting: SubsetSize must be greater than 0")
	}
	if lbCfg.ChildPolicy == nil {
		return nil, fmt.Errorf("randomsubsetting: ChildPolicy must be specified")
	}

	return lbCfg, nil
}

func (bb) Name() string {
	return Name
}

type subsettingBalancer struct {
	*gracefulswitch.Balancer

	logger *internalgrpclog.PrefixLogger
	cfg    *lbConfig
	hashf  *xxhash.Digest
}

func (b *subsettingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Errorf("Received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	if b.cfg == nil || b.cfg.ChildPolicy.Name != lbCfg.ChildPolicy.Name {

		if err := b.Balancer.SwitchTo(balancer.Get(lbCfg.ChildPolicy.Name)); err != nil {
			return fmt.Errorf("randomsubsetting: error switching to child of type %q: %v", lbCfg.ChildPolicy.Name, err)
		}
	}
	b.cfg = lbCfg

	return b.Balancer.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  b.prepareChildResolverState(s),
		BalancerConfig: b.cfg.ChildPolicy.Config,
	})
}

type endpointWithHash struct {
	hash uint64
	ep   resolver.Endpoint
}

// implements the subsetting algorithm,
// as described in A68: https://github.com/grpc/proposal/blob/master/A68-random-subsetting.md
func (b *subsettingBalancer) prepareChildResolverState(s balancer.ClientConnState) resolver.State {
	subsetSize := b.cfg.SubsetSize
	endpoints := s.ResolverState.Endpoints
	if len(endpoints) <= int(subsetSize) || subsetSize < 2 {
		return s.ResolverState
	}

	endpointSet := make([]endpointWithHash, len(endpoints))
	for i, endpoint := range endpoints {
		b.hashf.Write([]byte(endpoint.Addresses[0].String()))
		endpointSet[i] = endpointWithHash{
			hash: b.hashf.Sum64(),
			ep:   endpoint,
		}
	}

	slices.SortFunc(endpointSet, func(a, b endpointWithHash) int {
		if a.hash == b.hash {
			return 0
		}
		if a.hash < b.hash {
			return -1
		}
		return 1
	})

	if b.logger.V(2) {
		b.logger.Infof("Resulting subset: %v", endpointSet[:subsetSize])
	}

	// Convert back to resolver.Endpoints
	endpointSubset := make([]resolver.Endpoint, subsetSize)
	for i, endpoint := range endpointSet[:subsetSize] {
		endpointSubset[i] = endpoint.ep
	}

	return resolver.State{
		Endpoints:     endpointSubset,
		ServiceConfig: s.ResolverState.ServiceConfig,
		Attributes:    s.ResolverState.Attributes,
	}
}
