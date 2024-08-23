/*
 *
 * Copyright 2024 gRPC authors.
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

// Package pickfirstleaf contains the pick_first load balancing policy which
// will be the universal leaf policy after dualstack changes are implemented.
package pickfirstleaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	if envconfig.NewPickFirstEnabled {
		internal.ShuffleAddressListForTesting = func(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }
		// Register as the default pick_first balancer.
		Name = "pick_first"
	}
	balancer.Register(pickfirstBuilder{})
}

var (
	logger            = grpclog.Component("pick-first-leaf-lb")
	errBalancerClosed = fmt.Errorf("pickfirst: LB policy is closed")
	// Name is the name of the pick_first_leaf balancer.
	// Can be changed in init() if this balancer is to be registered as the default
	// pickfirst.
	Name = "pick_first_leaf"
)

const logPrefix = "[pick-first-leaf-lb %p] "

type pickfirstBuilder struct{}

func (pickfirstBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	ctx, cancel := context.WithCancel(context.Background())
	b := &pickfirstBalancer{
		cc:               cc,
		addressList:      addressList{},
		subConns:         resolver.NewAddressMap(),
		serializer:       grpcsync.NewCallbackSerializer(ctx),
		serializerCancel: cancel,
		state:            connectivity.Connecting,
	}
	b.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(logPrefix, b))
	return b
}

func (b pickfirstBuilder) Name() string {
	return Name
}

func (pickfirstBuilder) ParseConfig(js json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg pfConfig
	if err := json.Unmarshal(js, &cfg); err != nil {
		return nil, fmt.Errorf("pickfirst: unable to unmarshal LB policy config: %s, error: %v", string(js), err)
	}
	return cfg, nil
}

type pfConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// If set to true, instructs the LB policy to shuffle the order of the list
	// of endpoints received from the name resolver before attempting to
	// connect to them.
	ShuffleAddressList bool `json:"shuffleAddressList"`
}

// scData keeps track of the current state of the subConn.
type scData struct {
	subConn balancer.SubConn
	state   connectivity.State
	addr    resolver.Address
	lastErr error
}

func newSCData(b *pickfirstBalancer, addr resolver.Address) (*scData, error) {
	sd := &scData{
		state: connectivity.Idle,
		addr:  addr,
	}
	sc, err := b.cc.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{
		StateListener: func(state balancer.SubConnState) {
			// Store the state and delegate.
			b.serializer.TrySchedule(func(_ context.Context) {
				sd.state = state.ConnectivityState
				b.updateSubConnState(sd, state)
			})
		},
	})
	if err != nil {
		return nil, err
	}
	sd.subConn = sc
	return sd, nil
}

type pickfirstBalancer struct {
	// The following fields are initialized at build time and read-only after
	// that and therefore do not need to be guarded by a mutex.
	logger *internalgrpclog.PrefixLogger
	cc     balancer.ClientConn

	// The serializer and its cancel func are initialized at build time, and the
	// rest of the fields here are only accessed from serializer callbacks (or
	// from balancer.Balancer methods, which themselves are guaranteed to be
	// mutually exclusive) and hence do not need to be guarded by a mutex.
	// The serializer is used to ensure synchronization of updates triggered
	// from the idle picker and the already serialized resolver,
	// subconn state updates.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel func()
	state            connectivity.State
	subConns         *resolver.AddressMap // scData for active subonns mapped by address.
	addressList      addressList
	firstPass        bool
}

func (b *pickfirstBalancer) ResolverError(err error) {
	b.serializer.TrySchedule(func(_ context.Context) {
		b.resolverError(err)
	})
}

func (b *pickfirstBalancer) resolverError(err error) {
	if b.logger.V(2) {
		b.logger.Infof("Received error from the name resolver: %v", err)
	}
	if b.state == connectivity.Shutdown {
		return
	}
	// The picker will not change since the balancer does not currently
	// report an error.
	if b.state != connectivity.TransientFailure {
		return
	}

	b.closeSubConns()
	b.addressList.updateEndpointList(nil)
	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            &picker{err: fmt.Errorf("name resolver error: %v", err)},
	})
}

func (b *pickfirstBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	errCh := make(chan error, 1)
	b.serializer.ScheduleOr(func(_ context.Context) {
		err := b.updateClientConnState(state)
		errCh <- err
	}, func() {
		errCh <- errBalancerClosed
	})
	return <-errCh
}

// updateClientConnState handles clientConn state changes.
// Only executed in the context of a serializer callback.
func (b *pickfirstBalancer) updateClientConnState(state balancer.ClientConnState) error {
	if b.state == connectivity.Shutdown {
		return errBalancerClosed
	}
	if len(state.ResolverState.Addresses) == 0 && len(state.ResolverState.Endpoints) == 0 {
		// Cleanup state pertaining to the previous resolver state.
		// Treat an empty address list like an error by calling b.ResolverError.
		b.state = connectivity.TransientFailure
		b.resolverError(errors.New("produced zero addresses"))
		return balancer.ErrBadResolverState
	}
	cfg, ok := state.BalancerConfig.(pfConfig)
	if state.BalancerConfig != nil && !ok {
		return fmt.Errorf("pickfirst: received illegal BalancerConfig (type %T): %v: %w", state.BalancerConfig, state.BalancerConfig, balancer.ErrBadResolverState)
	}

	if b.logger.V(2) {
		b.logger.Infof("Received new config %s, resolver state %s", pretty.ToJSON(cfg), pretty.ToJSON(state.ResolverState))
	}

	newEndpoints := state.ResolverState.Endpoints
	if len(newEndpoints) == 0 {
		newEndpoints = make([]resolver.Endpoint, len(state.ResolverState.Addresses))
		// Convert addresses to endpoints.
		for i, a := range state.ResolverState.Addresses {
			newEndpoints[i].Attributes = a.BalancerAttributes
			newEndpoints[i].Addresses = []resolver.Address{a}
			// We can't remove balancer attributes here since xds packages use
			// them to store locality metadata.
		}
	}

	// Since we have a new set of addresses, we are again at first pass.
	b.firstPass = true
	// If multiple endpoints have the same address, they would use the same
	// subconn because an AddressMap is used to store subconns.
	// This would result in attempting to connect to the same subconn multiple
	// times in the same pass. We don't want this, so we ensure each address is
	// present in only one endpoint before moving further.
	newEndpoints = deDupAddresses(newEndpoints)

	// Perform the optional shuffling described in gRFC A62. The shuffling will
	// change the order of endpoints but not touch the order of the addresses
	// within each endpoint. - A61
	if cfg.ShuffleAddressList {
		newEndpoints = append([]resolver.Endpoint{}, newEndpoints...)
		internal.ShuffleAddressListForTesting.(func(int, func(int, int)))(len(newEndpoints), func(i, j int) { newEndpoints[i], newEndpoints[j] = newEndpoints[j], newEndpoints[i] })
	}

	// If the previous ready subconn exists in new address list,
	// keep this connection and don't create new subconns.
	prevAddr := b.addressList.currentAddress()
	prevAddrsCount := b.addressList.size()
	b.addressList.updateEndpointList(newEndpoints)
	if b.state == connectivity.Ready && b.addressList.seekTo(prevAddr) {
		return nil
	}

	b.reconcileSubConns(newEndpoints)
	// If its the first resolver update or the balancer was already READY
	// (but the new address list does not contain the ready subconn) or
	// CONNECTING, enter CONNECTING.
	// We may be in TRANSIENT_FAILURE due to a previous empty address list,
	// we should still enter CONNECTING because the sticky TF behaviour mentioned
	// in A62 applies only when the TRANSIENT_FAILURE is reported due to connectivity
	// failures.
	if b.state == connectivity.Ready || b.state == connectivity.Connecting || prevAddrsCount == 0 {
		// Start connection attempt at first address.
		b.state = connectivity.Connecting
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            &picker{err: balancer.ErrNoSubConnAvailable},
		})
		b.requestConnection()
	} else if b.state == connectivity.TransientFailure {
		// If we're in TRANSIENT_FAILURE, we stay in TRANSIENT_FAILURE until
		// we're READY. See A62.
		b.requestConnection()
	}
	return nil
}

// UpdateSubConnState is unused as a StateListener is always registered when
// creating SubConns.
func (b *pickfirstBalancer) UpdateSubConnState(subConn balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", subConn, state)
}

func (b *pickfirstBalancer) Close() {
	b.serializer.TrySchedule(func(_ context.Context) {
		b.closeSubConns()
		b.state = connectivity.Shutdown
	})
	b.serializerCancel()
	<-b.serializer.Done()
}

// ExitIdle moves the balancer out of idle state. It can be called concurrently
// by the idlePicker and clientConn so access to variables should be synchronized.
func (b *pickfirstBalancer) ExitIdle() {
	b.serializer.TrySchedule(func(_ context.Context) {
		if b.state == connectivity.Idle {
			b.requestConnection()
		}
	})
}

func (b *pickfirstBalancer) closeSubConns() {
	for _, sd := range b.subConns.Values() {
		sd.(*scData).subConn.Shutdown()
	}
	b.subConns = resolver.NewAddressMap()
}

// deDupAddresses ensures that each address belongs to only one endpoint.
func deDupAddresses(endpoints []resolver.Endpoint) []resolver.Endpoint {
	seenAddrs := resolver.NewAddressMap()
	newEndpoints := []resolver.Endpoint{}

	for _, ep := range endpoints {
		addrs := []resolver.Address{}
		for _, addr := range ep.Addresses {
			if _, ok := seenAddrs.Get(addr); ok {
				continue
			}
			addrs = append(addrs, addr)
		}
		if len(addrs) == 0 {
			continue
		}
		newEndpoints = append(newEndpoints, resolver.Endpoint{
			Addresses:  addrs,
			Attributes: ep.Attributes,
		})
	}
	return newEndpoints
}

func (b *pickfirstBalancer) reconcileSubConns(newEndpoints []resolver.Endpoint) {
	// Remove old subConns that were not in new address list.
	oldAddrs := resolver.NewAddressMap()
	for _, k := range b.subConns.Keys() {
		oldAddrs.Set(k, true)
	}

	// Flatten the new endpoint addresses.
	newAddrs := resolver.NewAddressMap()
	for _, endpoint := range newEndpoints {
		for _, addr := range endpoint.Addresses {
			newAddrs.Set(addr, true)
		}
	}

	// Shut them down and remove them.
	for _, oldAddr := range oldAddrs.Keys() {
		if _, ok := newAddrs.Get(oldAddr); ok {
			continue
		}
		val, _ := b.subConns.Get(oldAddr)
		val.(*scData).subConn.Shutdown()
		b.subConns.Delete(oldAddr)
	}
}

// shutdownRemaining shuts down remaining subConns. Called when a subConn
// becomes ready, which means that all other subConn must be shutdown.
func (b *pickfirstBalancer) shutdownRemaining(selected *scData) {
	for _, v := range b.subConns.Values() {
		sd := v.(*scData)
		if sd.subConn != selected.subConn {
			sd.subConn.Shutdown()
		}
	}
	for _, k := range b.subConns.Keys() {
		b.subConns.Delete(k)
	}
	b.subConns.Set(selected.addr, selected)
}

// requestConnection starts connecting on the subchannel corresponding to the
// current address. If no subchannel exists, one is created. If the current
// subchannel is in TransientFailure, a connection to the next address is
// attempted.
func (b *pickfirstBalancer) requestConnection() {
	if !b.addressList.isValid() || b.state == connectivity.Shutdown {
		return
	}
	curAddr := b.addressList.currentAddress()
	sd, ok := b.subConns.Get(curAddr)
	if !ok {
		var err error
		// We want to assign the new scData to sd from the outer scope, hence
		// we can't use := below.
		sd, err = newSCData(b, curAddr)
		if err != nil {
			// This should never happen, unless the clientConn is being shut
			// down.
			b.logger.Warningf("Failed to create a subConn for address %v: %v", curAddr.String(), err)
			// The LB policy remains in TRANSIENT_FAILURE until a new resolver
			// update is received.
			b.state = connectivity.TransientFailure
			b.addressList.reset()
			b.cc.UpdateState(balancer.State{
				ConnectivityState: connectivity.TransientFailure,
				Picker:            &picker{err: fmt.Errorf("failed to create a new subConn: %v", err)},
			})
			return
		}
		b.subConns.Set(curAddr, sd)
	}

	scd := sd.(*scData)
	switch scd.state {
	case connectivity.Idle:
		scd.subConn.Connect()
	case connectivity.TransientFailure:
		if !b.addressList.increment() {
			b.endFirstPass(scd.lastErr)
			return
		}
		b.requestConnection()
	case connectivity.Ready:
		// Should never happen.
		b.logger.Errorf("Requesting a connection even though we have a READY subconn")
	}
}

// updateSubConnState handles subConn state updates.
// Only executed in the context of a serializer callback.
func (b *pickfirstBalancer) updateSubConnState(sd *scData, state balancer.SubConnState) {
	// Previously relevant subconns can still callback with state updates.
	// To prevent pickers from returning these obsolete subconns, this logic
	// is included to check if the current list of active subconns includes this
	// subconn.
	if activeSD, found := b.subConns.Get(sd.addr); !found || activeSD != sd {
		return
	}
	if state.ConnectivityState == connectivity.Shutdown {
		return
	}

	if state.ConnectivityState == connectivity.Ready {
		b.shutdownRemaining(sd)
		if !b.addressList.seekTo(sd.addr) {
			// This should not fail as we should have only one subconn after
			// entering READY. The subconn should be present in the addressList.
			b.logger.Errorf("Address %q not found address list in  %v", sd.addr, b.addressList.addresses)
			return
		}
		b.state = connectivity.Ready
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.Ready,
			Picker:            &picker{result: balancer.PickResult{SubConn: sd.subConn}},
		})
		return
	}

	// If we are transitioning from READY to IDLE, reset index and re-connect when
	// prompted.
	if b.state == connectivity.Ready && state.ConnectivityState == connectivity.Idle {
		// Once a transport fails, the balancer enters IDLE and starts from
		// the first address when the picker is used.
		b.state = connectivity.Idle
		b.addressList.reset()
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.Idle,
			Picker:            &idlePicker{exitIdle: b.ExitIdle},
		})
		return
	}

	if b.firstPass {
		switch state.ConnectivityState {
		case connectivity.Connecting:
			// The balancer can be in either IDLE, CONNECTING or TRANSIENT_FAILURE.
			// If it's in TRANSIENT_FAILURE, stay in TRANSIENT_FAILURE until
			// it's READY. See A62.
			// If the balancer is already in CONNECTING, no update is needed.
			if b.state == connectivity.Idle {
				b.cc.UpdateState(balancer.State{
					ConnectivityState: connectivity.Connecting,
					Picker:            &picker{err: balancer.ErrNoSubConnAvailable},
				})
			}
		case connectivity.TransientFailure:
			sd.lastErr = state.ConnectionError
			// Since we're re-using common subconns while handling resolver updates,
			// we could receive an out of turn TRANSIENT_FAILURE from a pass
			// over the previous address list. We ignore such updates.

			if curAddr := b.addressList.currentAddress(); !equalAddressIgnoringBalAttributes(&curAddr, &sd.addr) {
				return
			}
			if b.addressList.increment() {
				b.requestConnection()
				return
			}
			// End of the first pass.
			b.endFirstPass(state.ConnectionError)
		case connectivity.Idle:
			// A subconn can transition from CONNECTING directly to IDLE when
			// a transport is successfully created, but the connection fails before
			// the subconn can send the notification for READY. We treat this
			// as a successful connection and transition to IDLE.
			if curAddr := b.addressList.currentAddress(); !equalAddressIgnoringBalAttributes(&sd.addr, &curAddr) {
				return
			}
			b.state = connectivity.Idle
			b.addressList.reset()
			b.cc.UpdateState(balancer.State{
				ConnectivityState: connectivity.Idle,
				Picker:            &idlePicker{exitIdle: b.ExitIdle},
			})
		}
		return
	}

	// We have finished the first pass, keep re-connecting failing subconns.
	switch state.ConnectivityState {
	case connectivity.TransientFailure:
		sd.lastErr = state.ConnectionError
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            &picker{err: state.ConnectionError},
		})
		// We don't need to request re-resolution since the subconn already does
		// that before reporting TRANSIENT_FAILURE.
		// TODO: #7534 - Move re-resolution requests from subconn into pick_first.
	case connectivity.Idle:
		sd.subConn.Connect()
	}
}

func (b *pickfirstBalancer) endFirstPass(lastErr error) {
	b.firstPass = false
	b.state = connectivity.TransientFailure

	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            &picker{err: lastErr},
	})
	// Start re-connecting all the subconns that are already in IDLE.
	for _, v := range b.subConns.Values() {
		sd := v.(*scData)
		if sd.state == connectivity.Idle {
			sd.subConn.Connect()
		}
	}
}

type picker struct {
	result balancer.PickResult
	err    error
}

func (p *picker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	return p.result, p.err
}

// idlePicker is used when the SubConn is IDLE and kicks the SubConn into
// CONNECTING when Pick is called.
type idlePicker struct {
	exitIdle func()
}

func (i *idlePicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	i.exitIdle()
	return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
}

// addressList manages sequentially iterating over addresses present in a list of
// endpoints. It provides a 1 dimensional view of the addresses present in the
// endpoints.
// This type is not safe for concurrent access.
type addressList struct {
	addresses []resolver.Address
	idx       int
}

func (al *addressList) isValid() bool {
	return al.idx < len(al.addresses)
}

func (al *addressList) size() int {
	return len(al.addresses)
}

// increment moves to the next index in the address list.
// This method returns false if it went off the list, true otherwise.
func (al *addressList) increment() bool {
	if !al.isValid() {
		return false
	}
	al.idx++
	return al.idx < len(al.addresses)
}

// currentAddress returns the current address pointed to in the addressList.
// If the list is in an invalid state, it returns an empty address instead.
func (al *addressList) currentAddress() resolver.Address {
	if !al.isValid() {
		return resolver.Address{}
	}
	return al.addresses[al.idx]
}

func (al *addressList) reset() {
	al.idx = 0
}

func (al *addressList) updateEndpointList(endpoints []resolver.Endpoint) {
	// Flatten the addresses.
	addrs := []resolver.Address{}
	for _, e := range endpoints {
		addrs = append(addrs, e.Addresses...)
	}
	al.addresses = addrs
	al.reset()
}

// seekTo returns false if the needle was not found and the current index was left unchanged.
func (al *addressList) seekTo(needle resolver.Address) bool {
	for ai, addr := range al.addresses {
		if !equalAddressIgnoringBalAttributes(&addr, &needle) {
			continue
		}
		al.idx = ai
		return true
	}
	return false
}

// equalAddressIgnoringBalAttributes returns true is a and b are considered equal.
// This is different from the Equal method on the resolver.Address type which
// considers all fields to determine equality. Here, we only consider fields
// that are meaningful to the subconn.
func equalAddressIgnoringBalAttributes(a, b *resolver.Address) bool {
	return a.Addr == b.Addr && a.ServerName == b.ServerName &&
		a.Attributes.Equal(b.Attributes) &&
		a.Metadata == b.Metadata
}
