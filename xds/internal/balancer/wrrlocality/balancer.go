/*
 *
 * Copyright 2023 gRPC authors.
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

// Package wrrlocality provides an implementation of the wrr locality LB policy,
// as defined in [A52 - xDS Custom LB Policies].
//
// [A52 - xDS Custom LB Policies]: https://github.com/grpc/proposal/blob/master/A52-xds-custom-lb-policies.md
package wrrlocality

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/weightedtarget"
	"google.golang.org/grpc/internal/grpclog"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal"
)

// Name is the name of wrr_locality balancer.
const Name = "xds_wrr_locality_experimental"

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Name() string {
	return Name
}

// LBConfig is the config for the wrr locality balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig
	// ChildPolicy is the config for the child policy.
	ChildPolicy *internalserviceconfig.BalancerConfig `json:"childPolicy,omitempty"`
}

// To plumb in a different child in tests.
var weightedTargetName = weightedtarget.Name

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	builder := balancer.Get(weightedTargetName)
	if builder == nil {
		// Shouldn't happen, registered through imported weighted target,
		// defensive programming.
		return nil
	}

	// Doesn't need to intercept any balancer.ClientConn operations; pass
	// through by just giving cc to child balancer.
	wtb := builder.Build(cc, bOpts)
	if wtb == nil {
		// shouldn't happen, defensive programming.
		return nil
	}
	wrrL := &wrrLocalityBalancer{
		child: wtb,
	}

	wrrL.logger = prefixLogger(wrrL)
	wrrL.logger.Infof("Created")
	return wrrL
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var lbCfg *LBConfig
	if err := json.Unmarshal(s, &lbCfg); err != nil {
		return nil, fmt.Errorf("xds: invalid LBConfig for wrrlocality: %s, error: %v", string(s), err)
	}
	if lbCfg == nil || lbCfg.ChildPolicy == nil {
		return nil, errors.New("xds: invalid LBConfig for wrrlocality: child policy field must be set")
	}
	return lbCfg, nil
}

type attributeKey struct{}

// Equal allows the values to be compared by Attributes.Equal.
func (a AddrInfo) Equal(o interface{}) bool {
	oa, ok := o.(AddrInfo)
	return ok && oa.LocalityWeight == a.LocalityWeight
}

// AddrInfo is the locality weight of the locality an address is a part of.
type AddrInfo struct {
	LocalityWeight uint32
}

// SetAddrInfo returns a copy of addr in which the BalancerAttributes field is
// updated with AddrInfo.
func SetAddrInfo(addr resolver.Address, addrInfo AddrInfo) resolver.Address {
	addr.BalancerAttributes = addr.BalancerAttributes.WithValue(attributeKey{}, addrInfo)
	return addr
}

func (a AddrInfo) String() string {
	return fmt.Sprintf("Locality Weight: %d", a.LocalityWeight)
}

// getAddrInfo returns the AddrInfo stored in the BalancerAttributes field of
// addr. Returns an AddrInfo with LocalityWeight of 1 if no AddrInfo found.
func getAddrInfo(addr resolver.Address) AddrInfo {
	v := addr.BalancerAttributes.Value(attributeKey{})
	var ai AddrInfo
	var ok bool
	if ai, ok = v.(AddrInfo); !ok {
		// Shouldn't happen, but to avoid undefiend behavior of 0 locality
		// weight.
		return AddrInfo{
			LocalityWeight: 1,
		}
	}
	return ai
}

// wrrLocalityBalancer wraps a weighted target balancer, and builds
// configuration for the weighted target once it receives configuration
// specifying the weighted target child balancer and locality weight
// information.
type wrrLocalityBalancer struct {
	// child will be a weighted target balancer, and this balancer will build
	// configuration for this child. Thus, build it at wrrLocalityBalancer build
	// time, and configure it once wrrLocalityBalancer received configurations.
	// Other balancer operations you pass through.
	child balancer.Balancer

	logger *grpclog.PrefixLogger
}

func (b *wrrLocalityBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*LBConfig)
	if !ok {
		b.logger.Errorf("received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	weightedTargets := make(map[string]weightedtarget.Target)
	for _, addr := range s.ResolverState.Addresses {
		// These gets of attributes could potentially return zero values. This
		// shouldn't happen though, and thus don't error out, and just build a
		// weighted target with undefined behavior. (For the attribute in this
		// package, I defaulted the getter to return a value with defined
		// behavior rather than zero value).
		locality := internal.GetLocalityID(addr)
		localityString, err := locality.ToString()
		if err != nil {
			// Should never happen.
			logger.Infof("failed to marshal LocalityID: %v, skipping this locality in weighted target")
		}
		ai := getAddrInfo(addr)
		weightedTargets[localityString] = weightedtarget.Target{Weight: ai.LocalityWeight, ChildPolicy: lbCfg.ChildPolicy}
	}
	wtCfg := &weightedtarget.LBConfig{Targets: weightedTargets}
	return b.child.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  s.ResolverState,
		BalancerConfig: wtCfg,
	})
}

func (b *wrrLocalityBalancer) ResolverError(err error) {
	b.child.ResolverError(err)
}

func (b *wrrLocalityBalancer) UpdateSubConnState(sc balancer.SubConn, scState balancer.SubConnState) {
	b.child.UpdateSubConnState(sc, scState)
}

func (b *wrrLocalityBalancer) Close() {
	b.child.Close()
}
