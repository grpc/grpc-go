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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
		cc:    cc,
		hashf: xxhash.NewWithSeed(uint64(time.Now().UnixNano())),
	}
	// Create a logger with a prefix specific to this balancer instance.
	b.logger = prefixLogger(b)

	b.logger.Infof("Created")
	b.child = gracefulswitch.NewBalancer(cc, bOpts)
	return b
}

// LBConfig is the config for the outlier detection balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	SubsetSize uint64 `json:"subset_size,omitempty"`

	ChildPolicy *iserviceconfig.BalancerConfig `json:"child_policy,omitempty"`
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbCfg := &LBConfig{
		// Default top layer values.
		SubsetSize: 10,
	}

	if err := json.Unmarshal(s, lbCfg); err != nil { // Validates child config if present as well.
		return nil, fmt.Errorf("subsetting: unable to unmarshal LBconfig: %s, error: %v", string(s), err)
	}

	// if someonw needs subsetSize == 1, he should use pick_first instead
	if lbCfg.SubsetSize < 2 {
		return nil, errors.New("subsetting: subsetSize must be >= 2")
	}

	return lbCfg, nil
}

func (bb) Name() string {
	return Name
}

type subsettingBalancer struct {
	cc     balancer.ClientConn
	logger *internalgrpclog.PrefixLogger
	cfg    *LBConfig
	hashf  *xxhash.Digest
	child  *gracefulswitch.Balancer
}

func (b *subsettingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*LBConfig)
	if !ok {
		b.logger.Errorf("received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	// Reject whole config if child policy doesn't exist, don't persist it for
	// later.
	bb := balancer.Get(lbCfg.ChildPolicy.Name)
	if bb == nil {
		return fmt.Errorf("subsetting: child balancer %q not registered", lbCfg.ChildPolicy.Name)
	}

	if b.cfg == nil || b.cfg.ChildPolicy.Name != lbCfg.ChildPolicy.Name {
		err := b.child.SwitchTo(bb)
		if err != nil {
			return fmt.Errorf("subsetting: error switching to child of type %q: %v", lbCfg.ChildPolicy.Name, err)
		}
	}
	b.cfg = lbCfg

	err := b.child.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  b.prepareChildResolverState(s.ResolverState),
		BalancerConfig: b.cfg.ChildPolicy.Config,
	})

	return err
}

type AddressWithHash struct {
	hash uint64
	addr resolver.Address
}

// implements the subsetting algorithm, as described in A68: https://github.com/grpc/proposal/pull/423
func (b *subsettingBalancer) prepareChildResolverState(s resolver.State) resolver.State {
	addresses := s.Addresses
	backendCount := len(addresses)
	if backendCount <= int(b.cfg.SubsetSize) {
		return s
	}

	addressesSet := make([]AddressWithHash, backendCount)
	// calculate hash for each endpoint
	for i, endpoint := range addresses {

		b.hashf.Write([]byte(s.Addresses[0].String()))
		addressesSet[i] = AddressWithHash{
			hash: b.hashf.Sum64(),
			addr: endpoint,
		}
	}
	// sort addresses by hash
	sort.Slice(addressesSet, func(i, j int) bool {
		return addressesSet[i].hash < addressesSet[j].hash
	})

	b.logger.Infof("resulting subset: %v", addressesSet[:b.cfg.SubsetSize])

	// Convert back to resolver.addresses
	addressesSubset := make([]resolver.Address, b.cfg.SubsetSize)
	for _, eh := range addressesSet[:b.cfg.SubsetSize] {
		addressesSubset = append(addressesSubset, eh.addr)
	}

	return resolver.State{
		Addresses:     addressesSubset,
		ServiceConfig: s.ServiceConfig,
		Attributes:    s.Attributes,
	}
}

func (b *subsettingBalancer) ResolverError(err error) {
	b.child.ResolverError(err)
}

func (b *subsettingBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.child.UpdateSubConnState(sc, state)
}

func (b *subsettingBalancer) Close() {
	b.child.Close()
}

func (b *subsettingBalancer) ExitIdle() {
	b.child.ExitIdle()
}
