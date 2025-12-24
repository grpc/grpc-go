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
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package randomsubsetting

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"

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
const Name = "random_subsetting_experimental"

var (
	logger     = grpclog.Component(Name)
	randUint64 = rand.Uint64
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
		Balancer:   gracefulswitch.NewBalancer(cc, bOpts),
		hashSeed:   randUint64(),
		hashDigest: xxhash.New(),
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
		return nil, fmt.Errorf("randomsubsetting: json.Unmarshal failed for configuration: %s with error: %v", string(s), err)
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

	logger     *internalgrpclog.PrefixLogger
	cfg        *lbConfig
	hashSeed   uint64
	hashDigest *xxhash.Digest
}

func (b *subsettingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Warningf("Received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	// Build config for the gracefulswitch balancer. It is safe to ignore
	// JSON marshaling errors here, since the config was already validated
	// as part of ParseConfig().
	cfg := []map[string]any{{lbCfg.ChildPolicy.Name: lbCfg.ChildPolicy.Config}}
	cfgJSON, _ := json.Marshal(cfg)
	parsedCfg, err := gracefulswitch.ParseConfig(cfgJSON)
	if err != nil {
		return fmt.Errorf("randomsubsetting: error switching to child of type %q: %v", lbCfg.ChildPolicy.Name, err)
	}
	b.cfg = lbCfg
	endpoints := resolver.State{
		Endpoints:     b.calculateSubset(s.ResolverState.Endpoints),
		ServiceConfig: s.ResolverState.ServiceConfig,
		Attributes:    s.ResolverState.Attributes,
	}

	return b.Balancer.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  endpoints,
		BalancerConfig: parsedCfg,
	})
}

// calculateSubset implements the subsetting algorithm, as described in A68:
// https://github.com/grpc/proposal/blob/master/A68-random-subsetting.md#subsetting-algorithm
func (b *subsettingBalancer) calculateSubset(endpoints []resolver.Endpoint) []resolver.Endpoint {
	// A helper struct to hold an endpoint and its hash.
	type endpointWithHash struct {
		hash uint64
		ep   resolver.Endpoint
	}

	subsetSize := b.cfg.SubsetSize
	if len(endpoints) <= int(subsetSize) {
		return endpoints
	}

	hashedEndpoints := make([]endpointWithHash, len(endpoints))
	for i, endpoint := range endpoints {
		// For every endpoint in the list, compute a hash with previously
		// generated seed - A68.
		//
		// The xxhash package's Sum64() function does not allow setting a seed.
		// This means that we need to reset the digest with the seed for every
		// endpoint. Without this, an endpoint will not retain the same hash
		// across resolver updates.
		//
		// Note that we only hash the first address of the endpoint, as per A68.
		b.hashDigest.ResetWithSeed(b.hashSeed)
		b.hashDigest.WriteString(endpoint.Addresses[0].String())
		hashedEndpoints[i] = endpointWithHash{
			hash: b.hashDigest.Sum64(),
			ep:   endpoint,
		}
	}

	slices.SortFunc(hashedEndpoints, func(a, b endpointWithHash) int {
		// Note: This uses the standard library cmp package, not the
		// github.com/google/go-cmp/cmp package. The latter is intended for
		// testing purposes only.
		return cmp.Compare(a.hash, b.hash)
	})

	// Convert back to resolver.Endpoints
	endpointSubset := make([]resolver.Endpoint, subsetSize)
	for i, endpoint := range hashedEndpoints[:subsetSize] {
		endpointSubset[i] = endpoint.ep
	}

	return endpointSubset
}
