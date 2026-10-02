/*
 *
 * Copyright 2026 gRPC authors.
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

// Package autosharding implements the autosharding load balancing policy.
package autosharding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/autosharding/internal"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/balancer/lazy"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/experimental/balancer/hostname"
	"google.golang.org/grpc/experimental/resolver/locality"
	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	// Name is the name of the autosharding balancer.
	Name   = "autosharding_experimental"
	prefix = "[autosharding-lb %p] "
)

var (
	logger              = grpclog.Component("autosharding")
	errNoChannelFactory = fmt.Errorf("autosharding: no channel factory found in resolver state")
)

func prefixLogger(p *autoshardingBalancer) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefix, p))
}

func lazyPickFirstBuilder(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return lazy.NewBalancer(cc, opts, balancer.Get(pickfirst.Name).Build)
}

func init() {
	balancer.Register(bb{})

	internal.NewAutoshardingClient = sharding.NewClient
}

type bb struct{}

func (bb) Name() string {
	return Name
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(s)
}

func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	ctx, cancel := context.WithCancel(context.Background())

	b := &autoshardingBalancer{
		ClientConn:       cc,
		uuid:             uuid.New().String(),
		endpointMap:      make(map[string]*endpointState),
		serializer:       grpcsync.NewCallbackSerializer(ctx),
		serializerCancel: cancel,
	}

	// Configure endpointsharding to not automatically reconnect a child that
	// has moved to IDLE. This is the behavior we want for autosharding (similar
	// to ring_hash). Reconnecting is handled when an RPC matches the child
	// policy and the child is in IDLE.
	esOpts := endpointsharding.Options{DisableAutoReconnect: true}
	b.child = endpointsharding.NewBalancer(b, opts, lazyPickFirstBuilder, esOpts)

	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
	return b
}

// endpointState represents the state associated with an endpoint in the LB policy.
//
//lint:ignore U1000 Struct fields planned for future implementation
type endpointState struct {
	index             int                // Index of the endpoint within the NR update
	endpoint          resolver.Endpoint  // The actual endpoint returned by the NR
	connectivityState connectivity.State // The connectivity state of the child balancer for this endpoint.
	picker            balancer.Picker    // The picker for the child balancer for this endpoint.
	exitIdle          func()             // Function to exit the child balancer from IDLE state.
}

type autoshardingBalancer struct {
	// The following fields are initialized at build time and never modified
	// afterwards.
	balancer.ClientConn // To intercept UpdateState calls from the child balancer.
	logger              *internalgrpclog.PrefixLogger
	child               balancer.Balancer
	uuid                string

	// Used to run callbacks from the autosharding client in a serialized
	// manner. This ensures that these callbacks are non-blocking and simplifies
	// the implementation of the autoshardingClient.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel func()

	// The following fields are protected by mu, since they are accessed from:
	// - Balancer API methods, which are themselves guaranteed to be called
	//   serially
	// - UpdateState calls from child policies
	// - Updates from the autosharding client
	mu                       sync.Mutex
	lbCfg                    LBConfig                  // The latest balancer config received from the resolver.
	lastResolverErr          error                     // The latest error received from the resolver. Non-nil value results in the channel moving to TF with an error picker.
	assignment               *sharding.Assignment      // The latest assignment received from the autosharding client.
	assignmentErr            error                     // The latest error received from the autosharding client.
	autoshardingChannel      grpc.ClientConnInterface  // The gRPC channel to the autosharding service.
	autoshardingChannelClose func()                    // The function to close the gRPC channel to the autosharding service.
	autoshardingClientClose  func()                    // The function to close the autosharding client. Nil before the first client is created.
	autoshardingClientGen    int                       // The generation number of the autosharding client. Incremented each time a new client is created.
	autoshardingTarget       string                    // The autosharding target used to create the autosharding client.
	endpointMap              map[string]*endpointState // Map of hostname to endpointState for all endpoints received from the resolver.
	sliceMap                 *sliceMap                 // The latest sliceMap generated from the latest assignment and endpointMap.
	inhibitChildUpdates      bool                      // Set to true to inhibit updates from the child balancer when processing a new resolver update.
	shouldRegenerateSliceMap bool                      // Set to true when the order or count of endpoints changes when processing a new resolver update.
	closed                   bool                      // Set to true when the balancer is closed.
}

func (b *autoshardingBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	newConfig, ok := ccs.BalancerConfig.(*LBConfig)
	if !ok {
		return fmt.Errorf("autosharding: unexpected balancer config with type: %T", ccs.BalancerConfig)
	}
	if b.logger.V(2) {
		b.logger.Infof("Received new balancer config: %+v", pretty.ToJSON(newConfig))
	}

	// Update the picker at the end of this method to reflect the new
	// endpoints and/or configuration.
	defer func() {
		b.mu.Lock()
		b.inhibitChildUpdates = false
		b.updateStateAndPickerLocked()
		b.mu.Unlock()
	}()

	b.mu.Lock()
	b.inhibitChildUpdates = true
	filteredEndpoints := b.handleNewEndpointsLocked(ccs.ResolverState.Endpoints)
	b.mu.Unlock()

	// Ensure that the ResolverState passed to the endpointsharding child
	// balancer has the filtered endpoints. This is important because the child
	// balancer will use the list of endpoints to create pick_first children for
	// each endpoint, and we don't want to create children for endpoints that
	// have no addresses or are duplicates.
	rs := ccs.ResolverState
	rs.Endpoints = filteredEndpoints

	// Make pickfirst children use health listeners for outlier detection
	// and health checking to work.
	childCCS := balancer.ClientConnState{ResolverState: pickfirst.EnableHealthListener(rs)}

	// Send endpoints down to the endpointsharding child balancer and let it
	// handle creating (lazily) and deleting pick_first children as endpoints
	// come and go. Note that the endpointsharding child will call UpdateState
	// inline where we will update the childState of each endpoint in the
	// endpointMap.
	childErr := b.child.UpdateClientConnState(childCCS)
	if err := b.handleNewConfiguration(ccs.ResolverState, newConfig); err != nil {
		return err
	}
	return childErr
}

func hostnameFromEndpoint(ep resolver.Endpoint) string {
	hostname := hostname.FromEndpoint(ep)
	if hostname == "" {
		hostname = ep.Addresses[0].Addr
	}
	return hostname
}

// handleNewEndpointsLocked processes the new endpoints received from the
// resolver and updates the endpointMap accordingly. It also determines if the
// order or count of endpoints has changed, which would require regenerating the
// slice map in updateStateAndPickerLocked().
func (b *autoshardingBalancer) handleNewEndpointsLocked(endpoints []resolver.Endpoint) []resolver.Endpoint {
	filteredEndpoints := make([]resolver.Endpoint, 0, len(endpoints))

	orderOrCountChanged := false
	seenHostnames := make(map[string]bool)
	seenEndpoints := resolver.NewEndpointMap[bool]()
	newEndpointMap := make(map[string]*endpointState)
	for _, ep := range endpoints {
		// Filter out endpoints with zero addresses.
		if len(ep.Addresses) == 0 {
			b.logger.Warningf("Ignoring endpoint %v with no addresses", ep)
			continue
		}

		// Filter out endpoints based on duplicate hostname.
		hostname := hostnameFromEndpoint(ep)
		if seenHostnames[hostname] {
			b.logger.Warningf("Ignoring duplicate endpoint with hostname %q", hostname)
			continue
		}

		// Filter out endpoints based on duplicate addresses. This is because
		// endpointsharding drops children with duplicate addresses, and we want
		// to ensure that the endpointMap and sliceMap are consistent with what
		// the child balancer sees.
		if _, ok := seenEndpoints.Get(ep); ok {
			b.logger.Warningf("Ignoring endpoint %v with duplicate addresses", ep)
			continue
		}

		seenHostnames[hostname] = true
		seenEndpoints.Set(ep, true)
		filteredEndpoints = append(filteredEndpoints, ep)

		idx := len(newEndpointMap)
		oldEpState, ok := b.endpointMap[hostname]
		if ok {
			if oldEpState.index != idx {
				// The index of the endpoint has changed.
				orderOrCountChanged = true
			}
		} else {
			// The endpoint is new.
			orderOrCountChanged = true
		}

		newEndpointMap[hostname] = &endpointState{
			index:    idx,
			endpoint: ep,
		}
		// There is no need to preserve the child state from the previous
		// endpoint map, as the endpointsharding child balancer will report the
		// new child states in its UpdateState call inline when we call
		// UpdateClientConnState on it (which happens in our
		// UpdateClientConnState once we return from this method). This is
		// simply to decouple the autosharding balancer from the
		// endpointsharding child's implementation details.
		if oldEpState != nil {
			newEndpointMap[hostname].connectivityState = oldEpState.connectivityState
			newEndpointMap[hostname].picker = oldEpState.picker
			newEndpointMap[hostname].exitIdle = oldEpState.exitIdle
		}
	}
	if len(newEndpointMap) != len(b.endpointMap) {
		// The count of endpoints has changed.
		orderOrCountChanged = true
	}
	b.endpointMap = newEndpointMap
	if orderOrCountChanged {
		b.shouldRegenerateSliceMap = true
	}
	return filteredEndpoints
}

// handleNewConfiguration processes the new balancer configuration received from
// the resolver and updates the autosharding balancer's state accordingly. It
// handles changes to the channel factory key and autosharding target, creating
// new gRPC channels and autosharding clients as needed. It also updates the
// lastResolverErr field to reflect any errors encountered during processing.
func (b *autoshardingBalancer) handleNewConfiguration(state resolver.State, newConfig *LBConfig) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Reset the lastResolverErr to nil as we start to process the new
	// configuration. A non-nil value for lastResolverErr results in the overall
	// connectivity state of the balancer being set to TRANSIENT_FAILURE and an
	// erroring picker being returned to the channel.
	b.lastResolverErr = nil

	// We handle the empty endpoints case here instead of in
	// handleNewEndpointsLocked() because we want to allow the endpointsharding
	// child to be updated with an empty list of endpoints, so that it can close
	// child policies associated with endpoints that have been removed.
	if len(b.endpointMap) == 0 {
		b.lastResolverErr = internal.ErrNoEndpointsFromNR
		return balancer.ErrBadResolverState
	}

	provider := grpc.ClientConnProviderFromResolverState(state)
	if provider == nil {
		b.lastResolverErr = errNoChannelFactory
		return balancer.ErrBadResolverState
	}

	// Reuse the existing gRPC channel unless channel_factory_key has changed.
	channel := b.autoshardingChannel
	var cancel func()
	createNewAutoshardingClient := false
	newAutoshardingChannelCreated := false
	if b.autoshardingClientClose == nil || newConfig.ChannelFactoryKey != b.lbCfg.ChannelFactoryKey {
		if b.logger.V(2) {
			b.logger.Infof("Creating a new gRPC channel with channel_factory_key: %q", newConfig.ChannelFactoryKey)
		}
		var err error
		channel, cancel, err = provider(newConfig.ChannelFactoryKey)
		if err != nil {
			b.lastResolverErr = fmt.Errorf("autosharding: failed to create gRPC channel for key %q: %v", newConfig.ChannelFactoryKey, err)
			return balancer.ErrBadResolverState
		}

		newAutoshardingChannelCreated = true
		createNewAutoshardingClient = true
	}

	// Ensure %s string substitution for "Locality" is taken into account for
	// the autosharding_target field.
	locality := locality.FromResolverState(state)
	newTarget := strings.Replace(newConfig.AutoShardingTarget, "%s", locality, 1)
	if b.autoshardingClientClose == nil || newTarget != b.autoshardingTarget {
		createNewAutoshardingClient = true
	}

	if createNewAutoshardingClient {
		// There is a small window of time when both the old and new
		// autosharding clients are active. We increment the generation number
		// to ensure that callbacks from the old client are ignored.
		b.autoshardingClientGen++
		curGen := b.autoshardingClientGen
		cancel := internal.NewAutoshardingClient(sharding.ClientOptions{
			CC:                       channel,
			AutoshardingTarget:       newTarget,
			UUID:                     b.uuid,
			InitialAssignmentTimeout: time.Duration(newConfig.InitialAssignmentTimeout),
			OnAssignmentUpdate: func(assignment *sharding.Assignment) {
				b.onAssignmentUpdate(curGen, assignment)
			},
			OnAssignmentError: func(err error) {
				b.onAssignmentError(curGen, err)
			},
		})
		if b.autoshardingClientClose != nil {
			b.autoshardingClientClose()
		}
		b.autoshardingClientClose = cancel
		b.autoshardingTarget = newTarget
	}

	if newAutoshardingChannelCreated {
		if b.autoshardingChannelClose != nil {
			b.autoshardingChannelClose()
		}
		b.autoshardingChannel = channel
		b.autoshardingChannelClose = cancel
	}
	b.lbCfg = *newConfig
	return nil
}

// onAssignmentUpdate is called by the autosharding client when a new assignment
// is received from the autosharding service. A callback is queued in the
// serializer that updates the assignment and assignmentErr fields and triggers
// a picker update. This is to ensure that the callback is non-blocking.
func (b *autoshardingBalancer) onAssignmentUpdate(gen int, assignment *sharding.Assignment) {
	b.serializer.TrySchedule(func(context.Context) {
		b.mu.Lock()
		defer b.mu.Unlock()

		if b.closed || gen != b.autoshardingClientGen {
			return
		}

		b.assignment = assignment
		b.assignmentErr = nil
		b.shouldRegenerateSliceMap = true
		b.updateStateAndPickerLocked()
	})
}

// onAssignmentError is called by the autosharding client when an error occurs
// while fetching an assignment from the autosharding service. A callback is
// queued in the serializer that updates the assignment and assignmentErr fields
// and triggers a picker update. This is to ensure that the callback is
// non-blocking.
func (b *autoshardingBalancer) onAssignmentError(gen int, err error) {
	b.serializer.TrySchedule(func(context.Context) {
		b.mu.Lock()
		defer b.mu.Unlock()

		if b.closed || gen != b.autoshardingClientGen {
			return
		}

		b.assignment = nil
		b.assignmentErr = err
		b.shouldRegenerateSliceMap = true
		b.updateStateAndPickerLocked()
	})
}

// updateStateAndPickerLocked updates the picker based on the current state of
// the balancer. It computes the aggregated connectivity state based on the
// child states of the endpoints and updates the ClientConn with the new state
// and picker. This method must be called with mu locked.
func (b *autoshardingBalancer) updateStateAndPickerLocked() {
	if b.inhibitChildUpdates {
		return
	}

	// If the resolver returned an error (before returning any valid
	// configuration) or the most recent update from the resolver was invalid
	// (for example, the channel_factory_key was invalid or it contained no
	// endpoints), report TRANSIENT_FAILURE and surface the error in failing
	// RPCs.
	if b.lastResolverErr != nil {
		b.ClientConn.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(b.lastResolverErr),
		})
		return
	}

	// No assignment or error from the autosharding client yet. Queue RPCs.
	if b.assignment == nil && b.assignmentErr == nil {
		// TODO(easwars/mbissa): Add autosharding_assignment_pending as the
		// delay type here once A121 is ready.
		b.ClientConn.UpdateState(balancer.State{
			ConnectivityState: connectivity.Idle,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		})
		return
	}

	// If the autosharding client returned an error and fallback is disabled,
	// report TRANSIENT_FAILURE and surface the error in failing RPCs. If
	// fallback is enabled, we will continue to use the fallback pool for picks.
	if b.assignmentErr != nil && !b.lbCfg.EnableFallback {
		b.ClientConn.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(b.assignmentErr),
		})
		return
	}

	// Compute the aggregated connectivity state of the
	// autosharding balancer based on the following rules.
	//   - If there is at least one endpoint in READY state, report READY.
	//   - If there are 2 or more endpoints in TRANSIENT_FAILURE state, report
	//     TRANSIENT_FAILURE.
	//   - If there is at least one endpoint in CONNECTING state, report CONNECTING.
	//   - If there is one endpoint in TRANSIENT_FAILURE and there is more than one
	//     endpoint, report state CONNECTING.
	//   - If there is at least one endpoint in Idle state, report Idle.
	//   - Otherwise, report TRANSIENT_FAILURE.
	var nums [5]int
	var firstIdle *endpointState
	for _, es := range b.endpointMap {
		s := es.connectivityState
		nums[s]++
		if s == connectivity.Idle {
			// Pick the first endpoint in IDLE state to trigger a connection
			// attempt on. While A119 does not specify which IDLE endpoint to
			// pick, we pick the one with the lowest index to ensure
			// deterministic behavior. This is required because the
			// endpointsharding child runs ExitIdle() in a goroutine. So, if we
			// pick a random IDLE endpoint, it is possible that another state
			// update arrives before the goroutine runs and moves the endpoint
			// to CONNECTING. In this case, we would end up attempting to
			// connect to another endpoint since the order of map iteration is
			// not deterministic.
			if firstIdle == nil || es.index < firstIdle.index {
				firstIdle = es
			}
		}
	}

	aggState := connectivity.TransientFailure
	if nums[connectivity.Ready] > 0 {
		aggState = connectivity.Ready
	} else if nums[connectivity.TransientFailure] > 1 {
		aggState = connectivity.TransientFailure
	} else if nums[connectivity.Connecting] > 0 {
		aggState = connectivity.Connecting
	} else if nums[connectivity.TransientFailure] == 1 && len(b.endpointMap) > 1 {
		aggState = connectivity.Connecting
	} else if nums[connectivity.Idle] > 0 {
		aggState = connectivity.Idle
	}

	// The specific behavior that will enable this LB policy to stop reporting
	// TRANSIENT_FAILURE even when it is not receiving picks will be that
	// whenever this policy receives a subchannel connectivity state update or a
	// resolver update, if the aggregated connectivity state is
	// TRANSIENT_FAILURE or CONNECTING and there are no endpoints in CONNECTING
	// state, the policy will choose one of the endpoints in IDLE state (if any)
	// to trigger a connection attempt on. - A119
	if aggState == connectivity.Connecting || aggState == connectivity.TransientFailure {
		if nums[connectivity.Connecting] == 0 && firstIdle != nil {
			firstIdle.exitIdle()
		}
	}

	if b.shouldRegenerateSliceMap {
		if b.logger.V(2) {
			b.logger.Infof("Regenerating slice map")
		}
		b.sliceMap = buildSliceMap(b.endpointMap, b.assignment)
		b.shouldRegenerateSliceMap = false
	}

	newPicker := newPicker(b.endpointMap, b.sliceMap, &b.lbCfg)
	b.ClientConn.UpdateState(balancer.State{
		ConnectivityState: aggState,
		Picker:            newPicker,
	})
}

func (b *autoshardingBalancer) ResolverError(err error) {
	if b.logger.V(2) {
		b.logger.Infof("Received resolver error: %v", err)
	}

	// If we haven't received a good update from the resolver yet, we want to
	// put the channel in TF when the endpointsharding child calls UpdateState
	// after we pass down the error.
	//
	// The reason for checking len(b.endpointMap) == 0 is to ensure that we
	// received valid endpoints from the name resolver previously. And the
	// reason for checking b.autoshardingClientClose == nil is to ensure that a
	// previous update with valid endpoints was not rejected due to otherwise
	// invalid configuration (e.g. invalid channel_factory_key).
	b.mu.Lock()
	if len(b.endpointMap) == 0 || b.autoshardingClientClose == nil {
		b.lastResolverErr = err
	}
	b.mu.Unlock()

	b.child.ResolverError(err)
}

func (b *autoshardingBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	// UpdateSubConnState is deprecated.
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

func (b *autoshardingBalancer) ExitIdle() {
	// ExitIdle implementation is a no-op because connections are either
	// triggered from picks or from child balancer state changes.
}

func (b *autoshardingBalancer) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	if b.autoshardingClientClose != nil {
		b.autoshardingClientClose()
		b.autoshardingClientClose = nil
	}
	if b.autoshardingChannelClose != nil {
		b.autoshardingChannelClose()
		b.autoshardingChannel = nil
		b.autoshardingChannelClose = nil
	}
	b.serializerCancel()
	b.lastResolverErr = nil
	b.endpointMap = nil
	b.sliceMap = nil
	b.assignment = nil
	b.assignmentErr = nil
	b.mu.Unlock()

	// Called outside of the lock to avoid deadlocks caused by lock order
	// inversion between the autosharding balancer and the child balancer.
	b.child.Close()
	b.child = nil

	<-b.serializer.Done()
	if b.logger.V(2) {
		b.logger.Infof("Shutdown")
	}
}

// UpdateState is called by the endpointsharding child to report new state. The
// autosharding balancer uses this to update the childState of each endpoint in
// its endpointMap, and then calls updateStateAndPickerLocked() to update the
// picker based on the new child states.
func (b *autoshardingBalancer) UpdateState(state balancer.State) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		if b.logger.V(2) {
			b.logger.Infof("Received new state from child balancer but balancer is closed: %+v", pretty.ToJSON(state))
		}
		return
	}

	if b.logger.V(2) {
		b.logger.Infof("Received new state from child balancer: %+v", pretty.ToJSON(state))
	}

	childStates := endpointsharding.ChildStatesFromPicker(state.Picker)
	for _, cs := range childStates {
		hostname := hostnameFromEndpoint(cs.Endpoint)
		if es, ok := b.endpointMap[hostname]; ok {
			es.connectivityState = cs.State.ConnectivityState
			es.picker = cs.State.Picker
			es.exitIdle = cs.ExitIdle
		} else {
			b.logger.Warningf("Received child state for unknown endpoint with hostname %q", hostname)
		}
	}
	b.updateStateAndPickerLocked()
}
