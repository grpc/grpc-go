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
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
)

// ccBalancerWrapper sits between the ClientConn and the Balancer.
//
// ccBalancerWrapper implements methods corresponding to the ones on the
// balancer.Balancer interface. The ClientConn is free to call these methods
// concurrently and the ccBalancerWrapper ensures that calls from the ClientConn
// to the Balancer happen synchronously and in order.
//
// ccBalancerWrapper also implements the balancer.ClientConn interface and is
// passed to the Balancer implementations. It invokes unexported methods on the
// ClientConn to handle these calls from the Balancer.
//
// It uses the gracefulswitch.Balancer internally to ensure that balancer
// switches happen in a graceful manner.
type ccBalancerWrapper struct {
	cc *ClientConn

	// Outgoing (gRPC --> balancer) calls are guaranteed to execute in a
	// mutually exclusive manner as they are scheduled on the
	// CallbackSerializer. Fields accessed *only* in serializer callbacks, can
	// therefore be accessed without a mutex.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel context.CancelFunc
	balancer         *gracefulswitch.Balancer
	curBalancerName  string
}

// newCCBalancerWrapper creates a new balancer wrapper. The underlying balancer
// is not created until the switchTo() method is invoked.
func newCCBalancerWrapper(cc *ClientConn, bopts balancer.BuildOptions) *ccBalancerWrapper {
	ctx, cancel := context.WithCancel(context.Background())
	ccb := &ccBalancerWrapper{
		cc:               cc,
		serializer:       grpcsync.NewCallbackSerializer(ctx),
		serializerCancel: cancel,
	}
	ccb.balancer = gracefulswitch.NewBalancer(ccb, bopts)
	return ccb
}

// updateClientConnState is invoked by grpc to push a ClientConnState update to
// the underlying balancer.
func (ccb *ccBalancerWrapper) updateClientConnState(ccs *balancer.ClientConnState) error {
	errCh := make(chan error, 1)
	ccb.serializer.Schedule(func(_ context.Context) {
		// If the addresses specified in the update contain addresses of type
		// "grpclb" and the selected LB policy is not "grpclb", these addresses
		// will be filtered out and ccs will be modified with the updated
		// address list.
		if ccb.curBalancerName != grpclbName {
			var addrs []resolver.Address
			for _, addr := range ccs.ResolverState.Addresses {
				if addr.Type == resolver.GRPCLB {
					continue
				}
				addrs = append(addrs, addr)
			}
			ccs.ResolverState.Addresses = addrs
		}
		errCh <- ccb.balancer.UpdateClientConnState(*ccs)
	})

	// If the balancer wrapper is closed when waiting for this state update to
	// be handled, the callback serializer will be closed as well, and we can
	// rely on its Done channel to ensure that we don't block here forever.
	select {
	case err := <-errCh:
		return err
	case <-ccb.serializer.Done:
		return nil
	}
}

// updateSubConnState is invoked by grpc to push a subConn state update to the
// underlying balancer.
func (ccb *ccBalancerWrapper) updateSubConnState(sc balancer.SubConn, s connectivity.State, err error) {
	// When updating addresses for a SubConn, if the address in use is not in
	// the new addresses, the old ac will be tearDown() and a new ac will be
	// created. tearDown() generates a state change with Shutdown state, we
	// don't want the balancer to receive this state change. So before
	// tearDown() on the old ac, ac.acbw (acWrapper) will be set to nil, and
	// this function will be called with (nil, Shutdown). We don't need to call
	// balancer method in this case.
	//
	// TODO: Suppress the above mentioned state change to Shutdown, so we don't
	// have to handle it here.
	if sc == nil {
		return
	}
	ccb.serializer.Schedule(func(_ context.Context) {
		ccb.balancer.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: s, ConnectionError: err})
	})
}

func (ccb *ccBalancerWrapper) exitIdle() {
	ccb.serializer.Schedule(func(_ context.Context) {
		ccb.balancer.ExitIdle()
	})
}

func (ccb *ccBalancerWrapper) resolverError(err error) {
	ccb.serializer.Schedule(func(_ context.Context) {
		ccb.balancer.ResolverError(err)
	})
}

// switchTo is invoked by grpc to instruct the balancer wrapper to switch to the
// LB policy identified by name.
//
// ClientConn calls newCCBalancerWrapper() at creation time. Upon receipt of the
// first good update from the name resolver, it determines the LB policy to use
// and invokes the switchTo() method. Upon receipt of every subsequent update
// from the name resolver, it invokes this method.
//
// the ccBalancerWrapper keeps track of the current LB policy name, and skips
// the graceful balancer switching process if the name does not change.
func (ccb *ccBalancerWrapper) switchTo(name string) {
	ccb.serializer.Schedule(func(_ context.Context) {
		// TODO: Other languages use case-insensitive balancer registries. We should
		// switch as well. See: https://github.com/grpc/grpc-go/issues/5288.
		if strings.EqualFold(ccb.curBalancerName, name) {
			return
		}

		// Use the default LB policy, pick_first, if no LB policy with name is
		// found in the registry.
		builder := balancer.Get(name)
		if builder == nil {
			channelz.Warningf(logger, ccb.cc.channelzID, "Channel switches to new LB policy %q, since the specified LB policy %q was not registered", PickFirstBalancerName, name)
			builder = newPickfirstBuilder()
		} else {
			channelz.Infof(logger, ccb.cc.channelzID, "Channel switches to new LB policy %q", name)
		}

		if err := ccb.balancer.SwitchTo(builder); err != nil {
			channelz.Errorf(logger, ccb.cc.channelzID, "Channel failed to build new LB policy %q: %v", name, err)
			return
		}
		ccb.curBalancerName = builder.Name()
	})
}

func (ccb *ccBalancerWrapper) close() {
	// Close the serializer to ensure that no more calls from gRPC are sent to
	// the balancer. We don't have to worry about suppressing calls from a
	// closed balancer because these are handled by the ClientConn (balancer
	// wrapper is only ever closed when the ClientConn is closed).
	ccb.serializerCancel()
	<-ccb.serializer.Done
	ccb.balancer.Close()
}

func (ccb *ccBalancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if len(addrs) <= 0 {
		return nil, fmt.Errorf("grpc: cannot create SubConn with empty address list")
	}
	ac, err := ccb.cc.newAddrConn(addrs, opts)
	if err != nil {
		channelz.Warningf(logger, ccb.cc.channelzID, "acBalancerWrapper: NewSubConn: failed to newAddrConn: %v", err)
		return nil, err
	}
	acbw := &acBalancerWrapper{ac: ac, producers: make(map[balancer.ProducerBuilder]*refCountedProducer)}
	acbw.ac.mu.Lock()
	ac.acbw = acbw
	acbw.ac.mu.Unlock()
	return acbw, nil
}

func (ccb *ccBalancerWrapper) RemoveSubConn(sc balancer.SubConn) {
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	ccb.cc.removeAddrConn(acbw.getAddrConn(), errConnDrain)
}

func (ccb *ccBalancerWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	acbw.UpdateAddresses(addrs)
}

func (ccb *ccBalancerWrapper) UpdateState(s balancer.State) {
	// Update picker before updating state.  Even though the ordering here does
	// not matter, it can lead to multiple calls of Pick in the common start-up
	// case where we wait for ready and then perform an RPC.  If the picker is
	// updated later, we could call the "connecting" picker when the state is
	// updated, and then call the "ready" picker after the picker gets updated.
	ccb.cc.blockingpicker.updatePicker(s.Picker)
	ccb.cc.csMgr.updateState(s.ConnectivityState)
}

func (ccb *ccBalancerWrapper) ResolveNow(o resolver.ResolveNowOptions) {
	ccb.cc.resolveNow(o)
}

func (ccb *ccBalancerWrapper) Target() string {
	return ccb.cc.target
}

// acBalancerWrapper is a wrapper on top of ac for balancers.
// It implements balancer.SubConn interface.
type acBalancerWrapper struct {
	mu        sync.Mutex
	ac        *addrConn
	producers map[balancer.ProducerBuilder]*refCountedProducer
}

func (acbw *acBalancerWrapper) UpdateAddresses(addrs []resolver.Address) {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	if len(addrs) <= 0 {
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)
		return
	}
	if !acbw.ac.tryUpdateAddrs(addrs) {
		cc := acbw.ac.cc
		opts := acbw.ac.scopts
		acbw.ac.mu.Lock()
		// Set old ac.acbw to nil so the Shutdown state update will be ignored
		// by balancer.
		//
		// TODO(bar) the state transition could be wrong when tearDown() old ac
		// and creating new ac, fix the transition.
		acbw.ac.acbw = nil
		acbw.ac.mu.Unlock()
		acState := acbw.ac.getState()
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)

		if acState == connectivity.Shutdown {
			return
		}

		newAC, err := cc.newAddrConn(addrs, opts)
		if err != nil {
			channelz.Warningf(logger, acbw.ac.channelzID, "acBalancerWrapper: UpdateAddresses: failed to newAddrConn: %v", err)
			return
		}
		acbw.ac = newAC
		newAC.mu.Lock()
		newAC.acbw = acbw
		newAC.mu.Unlock()
		if acState != connectivity.Idle {
			go newAC.connect()
		}
	}
}

func (acbw *acBalancerWrapper) Connect() {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	go acbw.ac.connect()
}

func (acbw *acBalancerWrapper) getAddrConn() *addrConn {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	return acbw.ac
}

// NewStream begins a streaming RPC on the addrConn.  If the addrConn is not
// ready, blocks until it is or ctx expires.  Returns an error when the context
// expires or the addrConn is shut down.
func (acbw *acBalancerWrapper) NewStream(ctx context.Context, desc *StreamDesc, method string, opts ...CallOption) (ClientStream, error) {
	transport, err := acbw.ac.getTransport(ctx)
	if err != nil {
		return nil, err
	}
	return newNonRetryClientStream(ctx, desc, method, transport, acbw.ac, opts...)
}

// Invoke performs a unary RPC.  If the addrConn is not ready, returns
// errSubConnNotReady.
func (acbw *acBalancerWrapper) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...CallOption) error {
	cs, err := acbw.NewStream(ctx, unaryStreamDesc, method, opts...)
	if err != nil {
		return err
	}
	if err := cs.SendMsg(args); err != nil {
		return err
	}
	return cs.RecvMsg(reply)
}

type refCountedProducer struct {
	producer balancer.Producer
	refs     int    // number of current refs to the producer
	close    func() // underlying producer's close function
}

func (acbw *acBalancerWrapper) GetOrBuildProducer(pb balancer.ProducerBuilder) (balancer.Producer, func()) {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()

	// Look up existing producer from this builder.
	pData := acbw.producers[pb]
	if pData == nil {
		// Not found; create a new one and add it to the producers map.
		p, close := pb.Build(acbw)
		pData = &refCountedProducer{producer: p, close: close}
		acbw.producers[pb] = pData
	}
	// Account for this new reference.
	pData.refs++

	// Return a cleanup function wrapped in a OnceFunc to remove this reference
	// and delete the refCountedProducer from the map if the total reference
	// count goes to zero.
	unref := func() {
		acbw.mu.Lock()
		pData.refs--
		if pData.refs == 0 {
			defer pData.close() // Run outside the acbw mutex
			delete(acbw.producers, pb)
		}
		acbw.mu.Unlock()
	}
	return pData.producer, grpcsync.OnceFunc(unref)
}
