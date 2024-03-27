/*
 *
 * Copyright 2023 gRPC authors.
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

package customroundrobin

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	balancer.Register(customRoundRobinBuilder{})
}

const customRRName = "custom_round_robin"

type customRRConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChooseSecond represents how often pick iterations choose the second
	// SubConn in the list. Defaults to 3. If 0 never choose the second SubConn.
	ChooseSecond uint32 `json:"chooseSecond,omitempty"`
}

type customRoundRobinBuilder struct{}

func (customRoundRobinBuilder) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &customRRConfig{
		ChooseSecond: 3,
	}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("custom-round-robin: unable to unmarshal customRRConfig: %v", err)
	}
	return lbConfig, nil
}

func (customRoundRobinBuilder) Name() string {
	return customRRName
}

func (customRoundRobinBuilder) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	pfBuilder := balancer.Get(grpc.PickFirstBalancerName)
	if pfBuilder == nil {
		return nil
	}
	return &customRoundRobin{
		cc:               cc,
		bOpts:            bOpts,
		pfs:              resolver.NewEndpointMap(),
		pickFirstBuilder: pfBuilder,
	}
}

var logger = grpclog.Component("example")

type customRoundRobin struct {
	// All state and operations on this balancer are either initialized at build
	// time and read only after, or are only accessed as part of its
	// balancer.Balancer API (UpdateState from children only comes in from
	// balancer.Balancer calls as well, and children are called one at a time),
	// in which calls are guaranteed to come synchronously. Thus, no extra
	// synchronization is required in this balancer.
	cc    balancer.ClientConn
	bOpts balancer.BuildOptions
	// Note that this balancer is a petiole policy which wraps pick first (see
	// gRFC A61). This is the intended way a user written custom lb should be
	// specified, as pick first will contain a lot of useful functionality, such
	// as Sticky Transient Failure, Happy Eyeballs, and Health Checking.
	pickFirstBuilder balancer.Builder
	pfs              *resolver.EndpointMap

	cfg *customRRConfig

	// InhibitPickerUpdates determines whether picker updates from the child
	// forward to cc or not.
	inhibitPickerUpdates bool
}

func (crr *customRoundRobin) UpdateClientConnState(state balancer.ClientConnState) error {
	crrCfg, ok := state.BalancerConfig.(*customRRConfig)
	if !ok {
		return balancer.ErrBadResolverState
	}
	crr.cfg = crrCfg

	endpointSet := resolver.NewEndpointMap()
	crr.inhibitPickerUpdates = true
	for _, endpoint := range state.ResolverState.Endpoints {
		endpointSet.Set(endpoint, nil)
		var pickFirst *balancerWrapper
		if pf, ok := crr.pfs.Get(endpoint); ok {
			pickFirst = pf.(*balancerWrapper)
		} else {
			pickFirst = &balancerWrapper{
				ClientConn: crr.cc,
				crr:        crr,
			}
			pfb := crr.pickFirstBuilder.Build(pickFirst, crr.bOpts)
			pickFirst.Balancer = pfb
			crr.pfs.Set(endpoint, pickFirst)
		}
		// Update child uncondtionally, in case attributes or address ordering
		// changed. Let pick first deal with any potential diffs, too
		// complicated to only update if we know something changed.
		pickFirst.UpdateClientConnState(balancer.ClientConnState{
			ResolverState: resolver.State{
				Endpoints:  []resolver.Endpoint{endpoint},
				Attributes: state.ResolverState.Attributes,
			},
			// no service config, never needed to turn on address list shuffling
			// bool in petiole policies.
		})
		// Ignore error because just care about ready children.
	}
	for _, e := range crr.pfs.Keys() {
		ep, _ := crr.pfs.Get(e)
		pickFirst := ep.(balancer.Balancer)
		// pick first was removed by resolver (unique endpoint logically
		// corresponding to pick first child was removed).
		if _, ok := endpointSet.Get(e); !ok {
			pickFirst.Close()
			crr.pfs.Delete(e)
		}
	}
	crr.inhibitPickerUpdates = false
	crr.regeneratePicker() // one synchronous picker update per UpdateClientConnState operation.
	return nil
}

func (crr *customRoundRobin) ResolverError(err error) {
	crr.inhibitPickerUpdates = true
	for _, pf := range crr.pfs.Values() {
		pickFirst := pf.(*balancerWrapper)
		pickFirst.ResolverError(err)
	}
	crr.inhibitPickerUpdates = false
	crr.regeneratePicker()
}

// This function is deprecated. SubConn state updates now come through listener
// callbacks. This balancer does not deal with SubConns directly and has no need
// to intercept listener callbacks.
func (crr *customRoundRobin) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	logger.Errorf("custom_round_robin: UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

func (crr *customRoundRobin) Close() {
	for _, pf := range crr.pfs.Values() {
		pickFirst := pf.(balancer.Balancer)
		pickFirst.Close()
	}
}

// regeneratePicker generates a picker based off persisted child balancer state
// and forwards it upward. This is intended to be fully executed once per
// relevant balancer.Balancer operation into custom round robin balancer.
func (crr *customRoundRobin) regeneratePicker() {
	if crr.inhibitPickerUpdates {
		return
	}

	var readyPickers []balancer.Picker
	for _, bw := range crr.pfs.Values() {
		pickFirst := bw.(*balancerWrapper)
		if pickFirst.state.ConnectivityState == connectivity.Ready {
			readyPickers = append(readyPickers, pickFirst.state.Picker)
		}
	}

	// For determinism, this balancer only updates it's picker when both
	// backends of the example are ready. Thus, no need to keep track of
	// aggregated state and can simply specify this balancer is READY once it
	// has two ready children.
	if len(readyPickers) != 2 {
		return
	}
	picker := &customRoundRobinPicker{
		pickers:      readyPickers,
		chooseSecond: crr.cfg.ChooseSecond,
		next:         0,
	}
	crr.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            picker,
	})
}

type balancerWrapper struct {
	balancer.Balancer   // Simply forward balancer.Balancer operations
	balancer.ClientConn // embed to intercept UpdateState, doesn't deal with SubConns

	crr *customRoundRobin

	state balancer.State
}

// Picker updates from pick first are all triggered by synchronous calls down
// into balancer.Balancer (client conn state updates, resolver errors, subconn
// state updates (through listener callbacks, which is still treated as part of
// balancer API)).
func (bw *balancerWrapper) UpdateState(state balancer.State) {
	bw.state = state
	// Calls back into this inline will be inhibited when part of
	// UpdateClientConnState() and ResolverError(), and regenerate picker will
	// be called manually at the end of those operations. However, for
	// UpdateSubConnState() and subsequent UpdateState(), this needs to update
	// picker, so call this regeneratePicker() here.
	bw.crr.regeneratePicker()
}

type customRoundRobinPicker struct {
	pickers      []balancer.Picker
	chooseSecond uint32
	next         uint32
}

func (crrp *customRoundRobinPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	next := atomic.AddUint32(&crrp.next, 1)
	index := 0
	if next != 0 && next%crrp.chooseSecond == 0 {
		index = 1
	}
	childPicker := crrp.pickers[index%len(crrp.pickers)]
	return childPicker.Pick(info)
}
