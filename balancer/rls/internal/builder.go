// +build go1.10

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

// Package rls implements the RLS LB policy.
package rls

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpcsync"
)

const rlsBalancerName = "rls"

func init() {
	balancer.Register(&rlsBB{})
}

// rlsBB helps build RLS load balancers and parse the service config to be
// passed to the RLS load balancer.
type rlsBB struct{}

// Name returns the name of the RLS LB policy and helps implement the
// balancer.Balancer interface.
func (*rlsBB) Name() string {
	return rlsBalancerName
}

func (*rlsBB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	lb := &rlsBalancer{
		done:       grpcsync.NewEvent(),
		cc:         cc,
		opts:       opts,
		lbCfg:      &lbConfig{},
		ccUpdateCh: make(chan *balancer.ClientConnState),
	}
	go lb.run()
	return lb
}
