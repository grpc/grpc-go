/*
 *
 * Copyright 2022 gRPC authors.
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
// as defined in
// https://github.com/grpc/proposal/blob/master/A52-xds-custom-lb-policies.md.
package wrrlocality

import (
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/weightedtarget"
	"google.golang.org/grpc/serviceconfig"
)

const Name = "xds_wrr_locality_experimental"

// takes child endpoint config and wraps it with locality weights is all this balancer is


// parse config is going to have a problem as well with JSON unmarshaling

// the grpclb registry is called in the client

type bb struct{}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	// comment here about 1:1 assumption as to why we can hardcode this child to xds_wrr_locality_experimental

	builder := balancer.Get(weightedtarget.Name)
	if builder == nil {
		// shouldn't happen, defensive programming
		return nil
	}

	wtb := builder.Build(cc, bOpts) // here is what determines whether client conn is pass through or not
	if wtb == nil { // can this even happen?
		// shouldn't happen, defensive programming
		return nil
	}
	return &wrrLocalityExperimental{
		child: wtb/*hardcoded to weighted target - this will handle graceful switch for you in balancer group*/,
	}
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	// right now with our JSON parsing (maybe add as a TODO to get rid of double validations)
	// the verification of child will happen in this function call

	// makes sures structure with child etc.
	var lbCfg *LBConfig
	if err := json.Unmarshal(s, &lbCfg); err != nil { // Validates child config if present as well.
		return nil, fmt.Errorf("xds: unable to unmarshal LBconfig: %s, error: %v", string(s), err)
	}
	// do we want to require child, also feels weird to have this defined her eand also up there with wrrLocalityExperimental perhaps require it here, since prepared by client anyway?
	return lbCfg, nil
}

type wrrLocalityExperimental struct {

	// what state does this need? this literally just takes child endpoint
	// picking config and wraps with locality weights

	// at what level is this gracefully switching. This child's config, or the
	// wrr locality...is the wrr locality level have a child that is already gracefully switched

	// maybe if endpoint type changes you prepare a whole new weighted target config
	// and then gracefully switch that child itself.

	child balancer.Balancer



}



// balancer.Balancer?


// balancer.ClientConn, does this need to intercept anything?
func (b *wrrLocalityExperimental) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*LBConfig)

	if !ok {
		b.logger.Errorf("received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	// it's already validated child right - I don't think I need this check

	// compare old endpoint policy name to old one, if it's different
	// and switch to bb...then prepare new config and send downward

	s.ResolverState.Attributes // this is where locality weights will be

	var localityWeights map[string]uint32
	weightedTargets := make(map[string]weightedtarget.Target)
	// use this to prepare the same weighted target config, similar logic to the
	// config builder

	// need to get the endpoints corresponding to the locality string
	for locality, weight := range localityWeights {
		weightedTargets[locality] = weightedtarget.Target{Weight: weight, ChildPolicy: lbCfg.ChildPolicy}
		// wait the only thing the endpoints do
		// is populate addresses, we just need to prepare weighted target
	}

	// the child type will always be weighted target

	// the config is the thing that changes. Does weighted target handle this for you?
	// Weighted target uses a balancer group. thus, this handles graceful switch for you

	// if this comes 1:1 with the child can you not just hardcode since you know
	// child type won't change, this is only given an endpoint picking policy
	// too.


	// this is the lb config
	// &weightedtarget.LBConfig{Targets: weightedTargets}

	b.child.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: s.ResolverState, // wait do you even need resolver state? you never clear out the attributes, right? To make it simpler for child?
		BalancerConfig: &weightedtarget.LBConfig{Targets: weightedTargets},
	})
}

func (b *wrrLocalityExperimental) ResolverError(error) {
	// should this send ErrPicker back? that's why Java had graceful switch.

	// oh wait now this hardcodes creation of cluster impl child, so can just

	// delegate to that and then you don't even need ERR PICKER, this happens in
	// graceful switch lb



}

// the rest of balancer.Balancer I think you need - or can we just pass through and embed, esp since it's 1:1 relation
func (b *wrrLocalityExperimental) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {

}

func (b *wrrLocalityExperimental) Close() {}

// I think balancer.ClientConn should be pass through
