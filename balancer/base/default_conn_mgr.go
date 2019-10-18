/*
 *
 * Copyright 2019 gRPC authors.
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

package base

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

// DefaultConnectionManager returns a default connection manager
// that will try to create one sub-connection for every resolved
// address it receives from a resolver.
func DefaultConnectionManager(scMgr SubConnManager, config Config) ConnectionManager {
	return &defaultConnMgr{
		scMgr:    scMgr,
		config:   config,
		subConns: make(map[resolver.Address]*SubConn),
	}
}

type defaultConnMgr struct {
	scMgr  SubConnManager
	config Config

	subConns map[resolver.Address]*SubConn
}

func (cm *defaultConnMgr) UpdateResolverState(state resolver.State) error {
	// TODO: handle s.ResolverState.Err (log if not nil) once implemented
	// TODO: handle s.ResolverState.ServiceConfig?
	if grpclog.V(2) {
		grpclog.Infoln("base.defaultConnMgr: got new resolver state: ", state)
	}
	// addrsSet is the set converted from addrs, it's used for quick lookup of an address.
	addrsSet := make(map[resolver.Address]struct{})
	for _, a := range state.Addresses {
		addrsSet[a] = struct{}{}
		if _, ok := cm.subConns[a]; !ok {
			// a is a new address (not existing in b.subConns).
			sc, err := cm.scMgr.NewSubConn(a, balancer.NewSubConnOptions{HealthCheckEnabled: cm.config.HealthCheck})
			if err != nil {
				grpclog.Warningf("base.baseBalancer: failed to create new SubConn: %v", err)
				continue
			}
			cm.subConns[a] = sc
			sc.Connect()
		}
	}
	for a, sc := range cm.subConns {
		// a was removed by resolver.
		if _, ok := addrsSet[a]; !ok {
			cm.scMgr.RemoveSubConn(sc)
			delete(cm.subConns, a)
			// Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
			// The entry will be deleted in HandleSubConnStateChange.
		}
	}
	return nil
}

// Close is a nop because default manager doesn't have internal state to clean up,
// and it doesn't need to call RemoveSubConn for the SubConns.
func (cm *defaultConnMgr) Close() {
}
