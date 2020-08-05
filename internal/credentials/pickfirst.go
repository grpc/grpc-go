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

package credentials

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/connectivity"
)

// PickFirstBalancerName is the name of the pick_first
const PickFirstBalancerName = "pick_first"

func newPickfirstBuilder() Builder {
	return &pickfirstBuilder{}
}

type pickfirstBuilder struct{}

func (*pickfirstBuilder) Build(cc ClientConn, opt BuildOptions) Balancer {
	return &pickfirstBalancer{cc: cc}
}

func (*pickfirstBuilder) Name() string {
	return PickFirstBalancerName
}

type pickfirstBalancer struct {
	state connectivity.State
	cc    ClientConn
	sc    SubConn
}

func (b *pickfirstBalancer) ResolverError(err error) {
	switch b.state {
	case connectivity.TransientFailure, connectivity.Idle, connectivity.Connecting:
		// Set a failing picker if we don't have a good picker.
		b.cc.UpdateState(State{ConnectivityState: connectivity.TransientFailure,
			Picker: &picker{err: fmt.Errorf("name resolver error: %v", err)},
		})
	}
	if logger.V(2) {
		logger.Infof("pickfirstBalancer: ResolverError called with error %v", err)
	}
}

func (b *pickfirstBalancer) UpdateClientConnState(cs ClientConnState) error {
	if len(cs.ResolverState.Addresses) == 0 {
		b.ResolverError(errors.New("produced zero addresses"))
		return ErrBadResolverState
	}
	if b.sc == nil {
		var err error
		b.sc, err = b.cc.NewSubConn(cs.ResolverState.Addresses, NewSubConnOptions{})
		if err != nil {
			if logger.V(2) {
				logger.Errorf("pickfirstBalancer: failed to NewSubConn: %v", err)
			}
			b.state = connectivity.TransientFailure
			b.cc.UpdateState(State{ConnectivityState: connectivity.TransientFailure,
				Picker: &picker{err: fmt.Errorf("error creating connection: %v", err)},
			})
			return ErrBadResolverState
		}
		b.state = connectivity.Idle
		b.cc.UpdateState(State{ConnectivityState: connectivity.Idle, Picker: &picker{result: PickResult{SubConn: b.sc}}})
		b.sc.Connect()
	} else {
		b.sc.UpdateAddresses(cs.ResolverState.Addresses)
		b.sc.Connect()
	}
	return nil
}

func (b *pickfirstBalancer) UpdateSubConnState(sc SubConn, s SubConnState) {
	if logger.V(2) {
		logger.Infof("pickfirstBalancer: UpdateSubConnState: %p, %v", sc, s)
	}
	if b.sc != sc {
		if logger.V(2) {
			logger.Infof("pickfirstBalancer: ignored state change because sc is not recognized")
		}
		return
	}
	b.state = s.ConnectivityState
	if s.ConnectivityState == connectivity.Shutdown {
		b.sc = nil
		return
	}

	switch s.ConnectivityState {
	case connectivity.Ready, connectivity.Idle:
		b.cc.UpdateState(State{ConnectivityState: s.ConnectivityState, Picker: &picker{result: PickResult{SubConn: sc}}})
	case connectivity.Connecting:
		b.cc.UpdateState(State{ConnectivityState: s.ConnectivityState, Picker: &picker{err: ErrNoSubConnAvailable}})
	case connectivity.TransientFailure:
		b.cc.UpdateState(State{
			ConnectivityState: s.ConnectivityState,
			Picker:            &picker{err: s.ConnectionError},
		})
	}
}

func (b *pickfirstBalancer) Close() {
}

type picker struct {
	result PickResult
	err    error
}

func (p *picker) Pick(info PickInfo) (PickResult, error) {
	return p.result, p.err
}

func init() {
	Register(newPickfirstBuilder())
}
