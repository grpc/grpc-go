/*
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
 */

// Package edsbalancer implements a balancer to handle EDS responses.
package edsbalancer

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"strconv"
	"sync"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	typepb "github.com/envoyproxy/go-control-plane/envoy/type"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
)

type localityConfig struct {
	weight uint32
	addrs  []resolver.Address
}

// localitiesSamePriority contains the localities with the same priority. It
// manages all localities using a balancerGroup.
type localitiesSamePriority struct {
	bg          *priorityBalancerGroup
	lidToConfig map[internal.Locality]*localityConfig
}

// EDSBalancer does load balancing based on the EDS responses. Note that it
// doesn't implement the balancer interface. It's intended to be used by a high
// level balancer implementation.
//
// The localities are picked as weighted round robin. A configurable child
// policy is used to manage endpoints in each locality.
type EDSBalancer struct {
	cc balancer.ClientConn

	subBalancerBuilder balancer.Builder
	loadStore          lrs.Store

	// maxPriorityPlusOne is The max priority number+1. E.g., with priorities
	// [0,1,2], this will be set to 3. It starts as 0 at init.
	//
	// The purpose of this field is to add/delete new priorityBalancerGroup
	// to/from the linked list.
	//
	// The new priorities can only be those numbers larger than priority max
	// priority number because according to doc, priorities should range from 0
	// (highest) to N (lowest) without skipping.
	maxPriorityPlusOne   uint32
	priorityToLocalities map[uint32]*localitiesSamePriority
	subConnMu            sync.Mutex
	subConnToPriority    map[balancer.SubConn]uint32

	pickerMu    sync.Mutex
	drops       []*dropper
	innerPicker balancer.Picker    // The picker without drop support.
	innerState  connectivity.State // The state of the picker.
}

// NewXDSBalancer create a new EDSBalancer.
func NewXDSBalancer(cc balancer.ClientConn, loadStore lrs.Store) *EDSBalancer {
	xdsB := &EDSBalancer{
		cc:                 cc,
		subBalancerBuilder: balancer.Get(roundrobin.Name),

		priorityToLocalities: make(map[uint32]*localitiesSamePriority),
		subConnToPriority:    make(map[balancer.SubConn]uint32),
		loadStore:            loadStore,
	}
	// Don't start balancer group here. Start it when handling the first EDS
	// response. Otherwise the balancer group will be started with round-robin,
	// and if users specify a different sub-balancer, all balancers in balancer
	// group will be closed and recreated when sub-balancer update happens.
	return xdsB
}

// HandleChildPolicy updates the child balancers handling endpoints. Child
// policy is roundrobin by default. If the specified balancer is not installed,
// the old child balancer will be used.
//
// HandleChildPolicy and HandleEDSResponse must be called by the same goroutine.
func (xdsB *EDSBalancer) HandleChildPolicy(name string, config json.RawMessage) {
	xdsB.updateSubBalancerName(name)
	// TODO: (eds) send balancer config to the new child balancers.
}

func (xdsB *EDSBalancer) updateSubBalancerName(subBalancerName string) {
	if xdsB.subBalancerBuilder.Name() == subBalancerName {
		return
	}
	newSubBalancerBuilder := balancer.Get(subBalancerName)
	if newSubBalancerBuilder == nil {
		grpclog.Infof("EDSBalancer: failed to find balancer with name %q, keep using %q", subBalancerName, xdsB.subBalancerBuilder.Name())
		return
	}
	xdsB.subBalancerBuilder = newSubBalancerBuilder
	for _, lGroup := range xdsB.priorityToLocalities {
		if lGroup == nil || len(lGroup.lidToConfig) == 0 {
			// There's no need to update balancer group there's no locality with
			// this priority.
			continue
		}
		for id, config := range lGroup.lidToConfig {
			// TODO: (eds) add support to balancer group to support smoothly
			//  switching sub-balancers (keep old balancer around until new
			//  balancer becomes ready).
			lGroup.bg.remove(id)
			lGroup.bg.add(id, config.weight, xdsB.subBalancerBuilder)
			lGroup.bg.handleResolvedAddrs(id, config.addrs)
		}
	}
}

// updateDrops compares new drop policies with the old. If they are different,
// it updates the drop policies and send ClientConn an updated picker.
func (xdsB *EDSBalancer) updateDrops(dropPolicies []*xdspb.ClusterLoadAssignment_Policy_DropOverload) {
	var (
		newDrops     []*dropper
		dropsChanged bool
	)
	for i, dropPolicy := range dropPolicies {
		percentage := dropPolicy.GetDropPercentage()
		var (
			numerator   = percentage.GetNumerator()
			denominator uint32
		)
		switch percentage.GetDenominator() {
		case typepb.FractionalPercent_HUNDRED:
			denominator = 100
		case typepb.FractionalPercent_TEN_THOUSAND:
			denominator = 10000
		case typepb.FractionalPercent_MILLION:
			denominator = 1000000
		}
		newDrops = append(newDrops, newDropper(numerator, denominator, dropPolicy.GetCategory()))

		// The following reading xdsB.drops doesn't need mutex because it can only
		// be updated by the code following.
		if dropsChanged {
			continue
		}
		if i >= len(xdsB.drops) {
			dropsChanged = true
			continue
		}
		if oldDrop := xdsB.drops[i]; numerator != oldDrop.numerator || denominator != oldDrop.denominator {
			dropsChanged = true
		}
	}
	if dropsChanged {
		xdsB.pickerMu.Lock()
		xdsB.drops = newDrops
		if xdsB.innerPicker != nil {
			// Update picker with old inner picker, new drops.
			xdsB.cc.UpdateBalancerState(xdsB.innerState, newDropPicker(xdsB.innerPicker, newDrops, xdsB.loadStore))
		}
		xdsB.pickerMu.Unlock()
	}
}

// HandleEDSResponse handles the EDS response and creates/deletes localities and
// SubConns. It also handles drops.
//
// HandleChildPolicy and HandleEDSResponse must be called by the same goroutine.
func (xdsB *EDSBalancer) HandleEDSResponse(edsResp *xdspb.ClusterLoadAssignment) {
	// TODO: Unhandled fields from EDS response:
	//  - edsResp.GetPolicy().GetOverprovisioningFactor()
	//  - locality.GetPriority()
	//  - lbEndpoint.GetMetadata(): contains BNS name, send to sub-balancers
	//    - as service config or as resolved address
	//  - if socketAddress is not ip:port
	//     - socketAddress.GetNamedPort(), socketAddress.GetResolverName()
	//     - resolve endpoint's name with another resolver

	xdsB.updateDrops(edsResp.GetPolicy().GetDropOverloads())

	// Filter out all localities with weight 0.
	//
	// Locality weighted load balancer can be enabled by setting an option in
	// CDS, and the weight of each locality. Currently, without the guarantee
	// that CDS is always sent, we assume locality weighted load balance is
	// always enabled, and ignore all weight 0 localities.
	//
	// In the future, we should look at the config in CDS response and decide
	// whether locality weight matters.
	newLocalitiesWithPriority := make(map[uint32][]*endpointpb.LocalityLbEndpoints)
	for _, locality := range edsResp.Endpoints {
		if locality.GetLoadBalancingWeight().GetValue() == 0 {
			continue
		}
		priority := locality.GetPriority()
		newLocalitiesWithPriority[priority] = append(newLocalitiesWithPriority[priority], locality)
	}

	var maxPriorityPlusOne uint32 // The max priority number.

	for priority, newLocalities := range newLocalitiesWithPriority {
		if maxPriorityPlusOne < priority {
			maxPriorityPlusOne = priority
		}

		lGroup, ok := xdsB.priorityToLocalities[priority]
		if !ok {
			// Create balancer group if it's never created (this is the first
			// time this priority is received). We don't start it here. We will
			// chain into the linked list. It may be started during that (if
			// it's a new lowest priority).
			lGroup = &localitiesSamePriority{
				bg: newPriorityBalancerGroup(
					xdsB.ccWrapperWithPriority(priority), xdsB.loadStore,
				),
				lidToConfig: make(map[internal.Locality]*localityConfig),
			}
			xdsB.priorityToLocalities[priority] = lGroup
		}

		// newLocalitiesSet contains all names of localitis in the new EDS
		// response for the same priority. It's used to delete localities that
		// are removed in the new EDS response.
		newLocalitiesSet := make(map[internal.Locality]struct{})
		for _, locality := range newLocalities {
			// One balancer for each locality.

			l := locality.GetLocality()
			if l == nil {
				grpclog.Warningf("xds: received LocalityLbEndpoints with <nil> Locality")
				continue
			}
			lid := internal.Locality{
				Region:  l.Region,
				Zone:    l.Zone,
				SubZone: l.SubZone,
			}
			newLocalitiesSet[lid] = struct{}{}

			newWeight := locality.GetLoadBalancingWeight().GetValue()
			var newAddrs []resolver.Address
			for _, lbEndpoint := range locality.GetLbEndpoints() {
				socketAddress := lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()
				address := resolver.Address{
					Addr: net.JoinHostPort(socketAddress.GetAddress(), strconv.Itoa(int(socketAddress.GetPortValue()))),
				}
				if xdsB.subBalancerBuilder.Name() == weightedroundrobin.Name &&
					lbEndpoint.GetLoadBalancingWeight().GetValue() != 0 {
					address.Metadata = &weightedroundrobin.AddrInfo{
						Weight: lbEndpoint.GetLoadBalancingWeight().GetValue(),
					}
				}
				newAddrs = append(newAddrs, address)
			}
			var weightChanged, addrsChanged bool
			config, ok := lGroup.lidToConfig[lid]
			if !ok {
				// A new balancer, add it to balancer group and balancer map.
				lGroup.bg.add(lid, newWeight, xdsB.subBalancerBuilder)
				config = &localityConfig{
					weight: newWeight,
				}
				lGroup.lidToConfig[lid] = config

				// weightChanged is false for new locality, because there's no need to
				// update weight in bg.
				addrsChanged = true
			} else {
				// Compare weight and addrs.
				if config.weight != newWeight {
					weightChanged = true
				}
				if !reflect.DeepEqual(config.addrs, newAddrs) {
					addrsChanged = true
				}
			}

			if weightChanged {
				config.weight = newWeight
				lGroup.bg.changeWeight(lid, newWeight)
			}

			if addrsChanged {
				config.addrs = newAddrs
				lGroup.bg.handleResolvedAddrs(lid, newAddrs)
			}
		}

		// Delete localities that are removed in the latest response.
		for lid := range lGroup.lidToConfig {
			if _, ok := newLocalitiesSet[lid]; !ok {
				lGroup.bg.remove(lid)
				delete(lGroup.lidToConfig, lid)
			}
		}
	}

	// Delete priorities that are removed in the latest response. We don't clse
	// them. They will be closed when they are removed from the chain list.
	for p := range xdsB.priorityToLocalities {
		if _, ok := newLocalitiesWithPriority[p]; !ok {
			delete(xdsB.priorityToLocalities, p)
		}
	}

	// We will create balancer groups for 0-max_priority_number, even if some of
	// them may not be included in the EDS response. The field doc in the proto
	// file actually says priorities should range from 0 (highest) to N (lowest)
	// without skipping.
	//
	// All priority bgs are chained into a linked list. Adding one in the middle
	// is tricky because we need to start it if the one in use is a lower
	// priority, but not if it's a higher priority. Having empty balancer groups
	// ready makes this easier to handle.

	maxPriorityPlusOne++
	if xdsB.maxPriorityPlusOne == maxPriorityPlusOne {
		// If no new priority to add, and no priority to remove.
		return
	}

	oldMaxPriorityPlusOne := xdsB.maxPriorityPlusOne
	xdsB.maxPriorityPlusOne = maxPriorityPlusOne
	if oldMaxPriorityPlusOne < maxPriorityPlusOne {
		// New priorities were added. The priorityBalancerGroups are already
		// created. Chain them into the chain list.
		var (
			lower       *priorityBalancerGroup
			manualStart bool
		)
		if lGroup, ok := xdsB.priorityToLocalities[oldMaxPriorityPlusOne-1]; ok {
			lower = lGroup.bg
		} else {
			// lower will be nil if oldMaxPriority is 0 (at init). And we want
			// to manually start the highest priority.
			manualStart = true
		}
		for i := maxPriorityPlusOne; i > oldMaxPriorityPlusOne; i-- {
			temp := xdsB.priorityToLocalities[i-1].bg
			temp.setNext(lower)
			lower = temp
		}
		if manualStart {
			lower.start()
		}
	}

	if oldMaxPriorityPlusOne > maxPriorityPlusOne {
		// Some priorities were removed. The priorityBalancerGroups are already
		// deleted from the map. Remove them from the chain list and close them.
		if lGroup, ok := xdsB.priorityToLocalities[maxPriorityPlusOne-1]; ok {
			// Remove next from the last valid item.
			lGroup.bg.setNext(nil)
		}
		for i := maxPriorityPlusOne; i < oldMaxPriorityPlusOne; i++ {
			temp := xdsB.priorityToLocalities[i].bg
			temp.setNext(nil)
			temp.close()
		}
	}
}

// HandleSubConnStateChange handles the state change and update pickers accordingly.
func (xdsB *EDSBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	xdsB.subConnMu.Lock()
	var lGroup *localitiesSamePriority
	if p, ok := xdsB.subConnToPriority[sc]; ok {
		if s == connectivity.Shutdown {
			// Only delete sc from the map when state changed to Shutdown.
			delete(xdsB.subConnToPriority, sc)
		}
		lGroup = xdsB.priorityToLocalities[p]
	}
	xdsB.subConnMu.Unlock()
	if lGroup == nil {
		grpclog.Infof("EDSBalancer: priority not found for sc state change")
		return
	}
	if bg := lGroup.bg; bg != nil {
		bg.handleSubConnStateChange(sc, s)
	}
}

// Close closes the balancer.
func (xdsB *EDSBalancer) Close() {
	for _, lGroup := range xdsB.priorityToLocalities {
		if bg := lGroup.bg; bg != nil {
			bg.close()
		}
	}
}

func (xdsB *EDSBalancer) ccWrapperWithPriority(priority uint32) *edsBalancerWrapperCC {
	return &edsBalancerWrapperCC{
		ClientConn: xdsB.cc,
		priority:   priority,
		parent:     xdsB,
	}
}

func (xdsB *EDSBalancer) newSubConn(priority uint32, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	sc, err := xdsB.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	xdsB.subConnMu.Lock()
	xdsB.subConnToPriority[sc] = priority
	xdsB.subConnMu.Unlock()
	return sc, nil
}

// npdateBalancerState is to wrap the picker in a dropPicker.
func (xdsB *EDSBalancer) updateBalancerState(s connectivity.State, p balancer.Picker) {
	xdsB.pickerMu.Lock()
	defer xdsB.pickerMu.Unlock()
	xdsB.innerPicker = p
	xdsB.innerState = s
	// Don't reset drops when it's a state change.
	xdsB.cc.UpdateBalancerState(s, newDropPicker(p, xdsB.drops, xdsB.loadStore))
}

// edsBalancerWrapperCC implements the balancer.ClientConn API and get passed to
// each balancer group. It contains the locality priority.
type edsBalancerWrapperCC struct {
	balancer.ClientConn
	priority uint32
	parent   *EDSBalancer
}

func (ebwcc *edsBalancerWrapperCC) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return ebwcc.parent.newSubConn(ebwcc.priority, addrs, opts)
}
func (ebwcc *edsBalancerWrapperCC) UpdateBalancerState(state connectivity.State, picker balancer.Picker) {
	ebwcc.parent.updateBalancerState(state, picker)
}

type dropPicker struct {
	drops     []*dropper
	p         balancer.Picker
	loadStore lrs.Store
}

func newDropPicker(p balancer.Picker, drops []*dropper, loadStore lrs.Store) *dropPicker {
	return &dropPicker{
		drops:     drops,
		p:         p,
		loadStore: loadStore,
	}
}

func (d *dropPicker) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	var (
		drop     bool
		category string
	)
	for _, dp := range d.drops {
		if dp.drop() {
			drop = true
			category = dp.category
			break
		}
	}
	if drop {
		if d.loadStore != nil {
			d.loadStore.CallDropped(category)
		}
		return nil, nil, status.Errorf(codes.Unavailable, "RPC is dropped")
	}
	// TODO: (eds) don't drop unless the inner picker is READY. Similar to
	// https://github.com/grpc/grpc-go/issues/2622.
	return d.p.Pick(ctx, opts)
}
