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

// Package randomsubsetting defines a random subsetting balancer.
//
// To install random subsetting balancer, import this package as:
//
//	import _ "google.golang.org/grpc/balancer/randomsubsetting"
package randomsubsetting

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	// Name is the name of the random subsetting load balancer.
	Name = "random_subsetting"
)

var (
	logger = grpclog.Component(Name)
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
		hashf:    xxhash.NewWithSeed(uint64(time.Now().UnixNano())),
	}
	// Create a logger with a prefix specific to this balancer instance.
	b.logger = prefixLogger(b)

	b.logger.Infof("Created")
	return b
}

// LBConfig is the config for the random subsetting balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	SubsetSize  uint64                         `json:"subsetSize,omitempty"`
	ChildPolicy *iserviceconfig.BalancerConfig `json:"childPolicy,omitempty"`
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbCfg := &LBConfig{
		SubsetSize:  2, // default value
		ChildPolicy: &iserviceconfig.BalancerConfig{Name: "round_robin"},
	}

	if err := json.Unmarshal(s, lbCfg); err != nil { // Validates child config if present as well.
		return nil, fmt.Errorf("randomsubsetting: unable to unmarshal LBConfig: %s, error: %v", string(s), err)
	}

	// if someone needs SubsetSize == 1, he should use pick_first instead
	if lbCfg.SubsetSize < 2 {
		return nil, fmt.Errorf("randomsubsetting: SubsetSize must be >= 2")
	}

	if lbCfg.ChildPolicy == nil {
		return nil, fmt.Errorf("randomsubsetting: child policy field must be set")
	}

	// Reject whole config if child policy doesn't exist, don't persist it for
	// later.
	bb := balancer.Get(lbCfg.ChildPolicy.Name)
	if bb == nil {
		return nil, fmt.Errorf("randomsubsetting: child balancer %q not registered", lbCfg.ChildPolicy.Name)
	}

	return lbCfg, nil
}

func (bb) Name() string {
	return Name
}

type subsettingBalancer struct {
	*gracefulswitch.Balancer

	logger *internalgrpclog.PrefixLogger
	cfg    *LBConfig
	hashf  *xxhash.Digest
}

func (b *subsettingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*LBConfig)
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
	endPoints := s.ResolverState.Endpoints
	backendCount := len(endPoints)
	if backendCount <= int(subsetSize) || subsetSize < 2 {
		return s.ResolverState
	}

	// calculate hash for each endpoint
	endpointSet := make([]endpointWithHash, backendCount)
	for i, endpoint := range endPoints {
		b.hashf.Write([]byte(endpoint.Addresses[0].String()))
		endpointSet[i] = endpointWithHash{
			hash: b.hashf.Sum64(),
			ep:   endpoint,
		}
	}

	// sort endpoint by hash
	slices.SortFunc(endpointSet, func(a, b endpointWithHash) int {
		return cmp.Compare(a.hash, b.hash)
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
