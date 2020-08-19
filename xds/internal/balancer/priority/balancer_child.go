/*
 *
 * Copyright 2020 gRPC authors.
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

package priority

import (
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

type childBalancer struct {
	name   string
	parent *priorityBalancer
	bb     balancer.Builder

	config serviceconfig.LoadBalancingConfig
	rState resolver.State

	started bool
	state   balancer.State
}

// newChildBalancer creates a child balancer place holder, but doesn't build the
// child balance.
func newChildBalancer(name string, parent *priorityBalancer, bb balancer.Builder) *childBalancer {
	return &childBalancer{
		name:    name,
		parent:  parent,
		bb:      bb,
		started: false,
		// Start child balancer with connecting state.
		state: balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		},
	}
}

// updateConfig sets childBalancer's config and state, but doesn't send update to
// the child balancer.
func (cb *childBalancer) updateConfig(config serviceconfig.LoadBalancingConfig, rState resolver.State) {
	cb.config = config
	cb.rState = rState

}

// start build the child balancer if it's not already started.
func (cb *childBalancer) start() {
	if !cb.started {
		cb.started = true
		cb.parent.bg.Add(cb.name, cb.bb)
	}
}

// sendUpdate sends the addresses and config to the child balancer.
func (cb *childBalancer) sendUpdate() {
	fmt.Println(" +++ sending addresses to ", cb.name)
	// TODO: handle aggregate returned error.
	cb.parent.bg.UpdateClientConnState(cb.name, balancer.ClientConnState{
		ResolverState:  cb.rState,
		BalancerConfig: cb.config,
	})
}

func (cb *childBalancer) stop() {
	if !cb.started {
		return
	}
	cb.parent.bg.Remove(cb.name)
	cb.started = false
	cb.state = balancer.State{
		ConnectivityState: connectivity.Connecting,
		Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
	}
}
