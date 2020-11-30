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
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
)

const priorityBalancerName = "priority_experimental"

func init() {
	balancer.Register(&priorityBB{})
}

type priorityBB struct{}

func (pbb *priorityBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return newPriorityBalancer(cc)
}

func (pbb *priorityBB) Name() string {
	return priorityBalancerName
}

type priorityBalancer struct {
	logger           *grpclog.PrefixLogger
	cc               balancer.ClientConn
	bg               *balancergroup.BalancerGroup
	ctx              context.Context
	cancel           context.CancelFunc
	childStateUpdate *buffer.Unbounded

	config *lbConfig

	mu            sync.Mutex
	childInUse    string
	priorityInUse int
	// priorities is a list of child names from higher to lower priority.
	priorities      []string
	childToPriority map[string]int
	// children is a map from child name to sub-balancers.
	children map[string]*childBalancer
	// The timer to give a priority 10 seconds to connect. And if the priority
	// doesn't go into Ready/Failure, start the next priority.
	//
	// One timer is enough because there can be at most one priority in init
	// state.
	priorityInitTimer *time.Timer
}

func newPriorityBalancer(cc balancer.ClientConn) *priorityBalancer {
	b := &priorityBalancer{
		cc:               cc,
		childToPriority:  make(map[string]int),
		children:         make(map[string]*childBalancer),
		childStateUpdate: buffer.NewUnbounded(),
	}
	b.ctx, b.cancel = context.WithCancel(context.Background())

	b.logger = prefixLogger(b)
	b.bg = balancergroup.New(cc, b, nil, b.logger)
	b.bg.Start()
	go b.run()
	b.logger.Infof("Created")
	return b
}

func (pb *priorityBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}
	addressesSplit := hierarchy.Group(s.ResolverState.Addresses)

	pb.mu.Lock()
	defer pb.mu.Unlock()
	// Create and remove children, since we know all children from the config
	// are used by some priority.
	for name, newSubConfig := range newConfig.Children {
		bb := balancer.Get(newSubConfig.Config.Name)
		if bb == nil {
			pb.logger.Warningf("balancer name %v from config is not registered", newSubConfig.Config.Name)
			continue
		}

		currentChild, ok := pb.children[name]
		if !ok {
			// This is a new child, add it to the children list. But note that
			// the balancer isn't built, because this child can be a low
			// priority. If necessary, it will be built when handling
			// priorities.
			cb := newChildBalancer(name, pb, bb)
			fmt.Printf(" ********** original err picker in child: %p\n", cb.state.Picker)
			cb.updateConfig(newSubConfig.Config.Config, resolver.State{
				Addresses:     addressesSplit[name],
				ServiceConfig: s.ResolverState.ServiceConfig,
				Attributes:    s.ResolverState.Attributes,
			})
			pb.children[name] = cb
			continue
		}

		// This is not a new child. But the config/addresses could change.

		// The balancing policy name is changed, close the old child. But don't
		// rebuild, rebuild will happen when handling priorities.
		if currentChild.bb.Name() != bb.Name() {
			currentChild.stop()
			currentChild.bb = bb
		}

		// Update config and address, but don't send to child balancer (the
		// child balancer might not exist, if it's a low priority).
		currentChild.updateConfig(newSubConfig.Config.Config, resolver.State{
			Addresses:     addressesSplit[name],
			ServiceConfig: s.ResolverState.ServiceConfig,
			Attributes:    s.ResolverState.Attributes,
		})
	}
	// Remove child from children if it's not in new config.
	for name, oldChild := range pb.children {
		if _, ok := newConfig.Children[name]; !ok {
			oldChild.stop()
		}
	}

	// Update priorities and handle priority changes.
	pb.priorities = make([]string, 0, len(newConfig.Priorities))
	pb.childToPriority = make(map[string]int)
	for pi, pName := range newConfig.Priorities {
		pb.priorities = append(pb.priorities, pName)
		pb.childToPriority[pName] = pi
	}

	// Sync the states of all children to the new updated priorities. This
	// include starting/stopping child balancers when necessary.
	pb.syncPriority()

	pb.config = newConfig
	return nil
}

func (pb *priorityBalancer) ResolverError(err error) {
	pb.bg.ResolverError(err)
}

func (pb *priorityBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	pb.bg.UpdateSubConnState(sc, state)
}

func (pb *priorityBalancer) Close() {
	// FIXME: check other things
	pb.cancel()
	pb.bg.Close()
}

// UpdateState implements balancergroup.BalancerStateAggregator interface. The
// balancer group sends new connectivity state and picker here.
func (pb *priorityBalancer) UpdateState(childName string, state balancer.State) {
	fmt.Printf(" +++ child state update from %q, %+v\n", childName, state)
	pb.childStateUpdate.Put(&balancerStateWithPriority{
		name: childName,
		s:    state,
	})
}

type balancerStateWithPriority struct {
	name string
	s    balancer.State
}

func (pb *priorityBalancer) run() {
	for {
		select {
		case u := <-pb.childStateUpdate.Get():
			pb.childStateUpdate.Load()
			s := u.(*balancerStateWithPriority)
			// Needs to handle state update in a goroutine, because each state
			// update needs to start/close child policy, could result in
			// deadlock.
			pb.handleChildStateUpdate(s.name, s.s)
		case <-pb.ctx.Done():
			return
		}
	}
}
