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

package grpc

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

// PickFirstBalancerName is the name of the pick_first balancer.
const PickFirstBalancerName = "pick_first"

func newPickfirstBuilder() balancer.Builder {
	return &pickfirstBuilder{}
}

type pickfirstBuilder struct{}

func (*pickfirstBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	return &pickfirstBalancer{cc: cc}
}

func (*pickfirstBuilder) Name() string {
	return PickFirstBalancerName
}

type pickfirstBalancer struct {
	cc balancer.ClientConn
	sc balancer.SubConn
}

func (b *pickfirstBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	if err != nil {
		if grpclog.V(2) {
			grpclog.Infof("pickfirstBalancer: HandleResolvedAddrs called with error %v", err)
		}
		return
	}
	if b.sc == nil {
		b.sc, err = b.cc.NewSubConn(addrs, balancer.NewSubConnOptions{})
		if err != nil {
			//TODO(yuxuanli): why not change the cc state to Idle?
			if grpclog.V(2) {
				grpclog.Errorf("pickfirstBalancer: failed to NewSubConn: %v", err)
			}
			return
		}
		b.cc.UpdateState(balancer.State{State: connectivity.Idle, Picker: &picker{result: balancer.PickResult{SubConn: b.sc}}})
		b.sc.Connect()
	} else {
		b.sc.UpdateAddresses(addrs)
		b.sc.Connect()
	}
}

func (b *pickfirstBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	if grpclog.V(2) {
		grpclog.Infof("pickfirstBalancer: HandleSubConnStateChange: %p, %v", sc, s)
	}
	if b.sc != sc {
		if grpclog.V(2) {
			grpclog.Infof("pickfirstBalancer: ignored state change because sc is not recognized")
		}
		return
	}
	if s == connectivity.Shutdown {
		b.sc = nil
		return
	}

	switch s {
	case connectivity.Ready, connectivity.Idle:
		b.cc.UpdateState(balancer.State{State: s, Picker: &picker{result: balancer.PickResult{SubConn: sc}}})
	case connectivity.Connecting:
		b.cc.UpdateState(balancer.State{State: s, Picker: &picker{err: balancer.ErrNoSubConnAvailable}})
	case connectivity.TransientFailure:
		b.cc.UpdateState(balancer.State{State: s, Picker: &picker{err: balancer.ErrTransientFailure}})
	}
}

func (b *pickfirstBalancer) Close() {
}

type picker struct {
	result balancer.PickResult
	err    error
}

func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return p.result, p.err
}

func init() {
	balancer.Register(newPickfirstBuilder())
}
