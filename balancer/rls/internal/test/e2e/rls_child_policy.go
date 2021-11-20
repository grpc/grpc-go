/*
 *
 * Copyright 2021 gRPC authors.
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

package e2e

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// RLSChildPolicyTargetNameField is a top-level field name to add to the child
// policy's config, whose value is set to the target name for the child policy.
const RLSChildPolicyTargetNameField = "Backend"

// RegisterRLSChildPolicy registers a balancer builder with the given name, to
// be used as a child policy for the RLS LB policy.
//
// The child policy uses a pickfirst balancer under the hood to send all traffic
// to the single backend specified by the `RLSChildPolicyTargetNameField` field
// in its configuration which looks like: {"Backend": "Backend-address"}.
func RegisterRLSChildPolicy(name string) {
	balancer.Register(bb{name: name})
}

type bb struct {
	name string
}

func (bb bb) Name() string { return bb.name }

func (bb bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	pf := balancer.Get(grpc.PickFirstBalancerName)
	return &bal{Balancer: pf.Build(cc, opts)}
}

func (bb bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	cfg := &lbConfig{}
	if err := json.Unmarshal(c, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

type bal struct {
	balancer.Balancer
}

type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	Backend string
	Random  string // A random field to test child policy config changes.
}

func (b *bal) UpdateClientConnState(c balancer.ClientConnState) error {
	cfg, ok := c.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("received balancer config of type %T, want %T", c.BalancerConfig, &lbConfig{})
	}
	return b.Balancer.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{Addresses: []resolver.Address{{Addr: cfg.Backend}}},
	})
}
