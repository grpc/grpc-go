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

// TODO(bar) move ClientConn methods to clientConn file.

func (cc *ClientConn) updatePicker(p balancer.Picker) {
	// TODO(bar) add a goroutine and sync it.
	// TODO(bar) implement blocking behavior and unblock the previous pick.
	cc.pmu.Lock()
	cc.picker = p
	cc.pmu.Unlock()
}

// ccBalancerWrapper is a wrapper on top of cc for balancers.
// It implements balancer.ClientConnection interface.
type ccBalancerWrapper struct {
	cc *ClientConn
}

func (ccb *ccBalancerWrapper) NewSubConnection(addrs []resolver.Address, opts balancer.NewSubConnectionOptions) (balancer.SubConnection, error) {
	grpclog.Infof("ccBalancerWrapper: new subconn: %v", addrs)
	ac, err := ccb.cc.newAddrConn(addrs[0])
	if err != nil {
		return nil, err
	}
	acbw := &acBalancerWrapper{ac: ac}
	ac.acbw = acbw
	return acbw, nil
}

func (ccb *ccBalancerWrapper) RemoveSubConnection(sc balancer.SubConnection) {
	grpclog.Infof("ccBalancerWrapper: removing subconn")
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	ccb.cc.removeSubConnection(acbw.ac, errConnClosing)
}

func (ccb *ccBalancerWrapper) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
	// TODO(bar) update cc connectivity state.
	ccb.cc.updatePicker(p)
}

func (ccb *ccBalancerWrapper) Target() string {
	return ccb.cc.target
}

// acBalancerWrapper is a wrapper on top of ac for balancers.
// It implements balancer.SubConnection interface.
type acBalancerWrapper struct {
	ac *addrConn
}

func (acbw *acBalancerWrapper) UpdateAddresses([]resolver.Address) {
	// TODO(bar) update the addresses or tearDown and create a new ac.
}

func (acbw *acBalancerWrapper) Connect() {
	acbw.ac.connect(false)
}
