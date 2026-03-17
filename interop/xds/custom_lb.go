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

// Package xds contains various xds interop helpers for usage in interop tests.
package xds

import (
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	balancer.Register(rpcBehaviorBB{})
}

const name = "test.RpcBehaviorLoadBalancer"

type rpcBehaviorBB struct{}

func (rpcBehaviorBB) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &rpcBehaviorLB{
		ClientConn: cc,
	}
	// round_robin child to complete balancer tree with a usable leaf policy and
	// have RPCs actually work.
	builder := balancer.Get(roundrobin.Name)
	if builder == nil {
		// Shouldn't happen, defensive programming. Registered from import of
		// roundrobin package.
		return nil
	}
	rr := builder.Build(b, bOpts)
	if rr == nil {
		// Shouldn't happen, defensive programming.
		return nil
	}
	b.Balancer = rr
	return b
}

func (rpcBehaviorBB) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbCfg := &lbConfig{}
	if err := json.Unmarshal(s, lbCfg); err != nil {
		return nil, fmt.Errorf("rpc-behavior-lb: unable to marshal lbConfig: %s, error: %v", string(s), err)
	}
	return lbCfg, nil

}

func (rpcBehaviorBB) Name() string {
	return name
}

type lbConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`
	RPCBehavior                       string `json:"rpcBehavior,omitempty"`
}

// rpcBehaviorLB is a load balancer that wraps a round robin balancer and
// appends the rpc-behavior metadata field to any metadata in pick results based
// on what is specified in configuration.
type rpcBehaviorLB struct {
	// embed a ClientConn to wrap only UpdateState() operation
	balancer.ClientConn
	// embed a Balancer to wrap only UpdateClientConnState() operation
	balancer.Balancer

	mu  sync.Mutex
	cfg *lbConfig
}

func (b *rpcBehaviorLB) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("test.RpcBehaviorLoadBalancer:received config with unexpected type %T: %s", s.BalancerConfig, pretty.ToJSON(s.BalancerConfig))
	}
	b.mu.Lock()
	b.cfg = lbCfg
	b.mu.Unlock()
	return b.Balancer.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: s.ResolverState,
	})
}

func (b *rpcBehaviorLB) UpdateState(state balancer.State) {
	b.mu.Lock()
	rpcBehavior := b.cfg.RPCBehavior
	b.mu.Unlock()

	b.ClientConn.UpdateState(balancer.State{
		ConnectivityState: state.ConnectivityState,
		Picker:            newRPCBehaviorPicker(state.Picker, rpcBehavior),
	})
}

// rpcBehaviorPicker wraps a picker and adds the rpc-behavior metadata field
// into the child pick result's metadata.
type rpcBehaviorPicker struct {
	childPicker balancer.Picker
	rpcBehavior string
}

// Pick appends the rpc-behavior metadata entry to the pick result of the child.
func (p *rpcBehaviorPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	pr, err := p.childPicker.Pick(info)
	if err != nil {
		return balancer.PickResult{}, err
	}
	pr.Metadata = metadata.Join(pr.Metadata, metadata.Pairs("rpc-behavior", p.rpcBehavior))
	return pr, nil
}

func newRPCBehaviorPicker(childPicker balancer.Picker, rpcBehavior string) *rpcBehaviorPicker {
	return &rpcBehaviorPicker{
		childPicker: childPicker,
		rpcBehavior: rpcBehavior,
	}
}
