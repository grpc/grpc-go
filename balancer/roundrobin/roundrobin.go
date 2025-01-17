/*
 *
 * Copyright 2017 gRPC authors.
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

// Package roundrobin defines a roundrobin balancer. Roundrobin balancer is
// installed as one of the default balancers in gRPC, users don't need to
// explicitly install this balancer.
package roundrobin

import (
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
)

// Name is the name of round_robin balancer.
const Name = "round_robin"

var (
	logger = grpclog.Component("roundrobin")
)

func init() {
	balancer.Register(builder{})
}

type builder struct{}

func (bb builder) Name() string {
	return Name
}

func (bb builder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	bal := &rrBalancer{
		cc:       cc,
		Balancer: endpointsharding.NewBalancer(cc, opts),
	}
	bal.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[%p] ", bal))
	bal.logger.Infof("Created")
	return bal
}

type rrBalancer struct {
	balancer.Balancer
	cc     balancer.ClientConn
	logger *internalgrpclog.PrefixLogger
}

func (b *rrBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	// Enable the health listener in pickfirst children for client side health
	// checks and outlier detection, if configured.
	ccs.ResolverState = pickfirstleaf.EnableHealthListener(ccs.ResolverState)
	ccs.BalancerConfig = endpointsharding.PickFirstConfig
	return b.Balancer.UpdateClientConnState(ccs)
}
