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
	"slices"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/experimental/balancer/hostname"
	"google.golang.org/grpc/experimental/resolver/locality"
	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
)

const (
	// Name is the name of the autosharding balancer.
	Name   = "autosharding_experimental"
	prefix = "[autosharding-lb %p] "
)

var logger = grpclog.Component("autosharding")

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
		ClientConn:            cc,
		uuid:                  uuid.New().String(),
		endpointMap:           make(map[string]*endpointState),
		endpointsByAddrs:      resolver.NewEndpointMap[*endpointState](),
		serializer:            grpcsync.NewCallbackSerializer(ctx),
		serializerCancel:      cancel,
		state:                 connectivity.Connecting,
		initialConnectPending: true,
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
// An endpointState is tied to a specific endpointsharding child, which is
// identified by the set of addresses in the endpoint. The same endpointState
// object is retained (and updated in place) across resolver updates for as long
// as an endpoint with the same set of addresses is present.
type endpointState struct {
	index             int                // Index of the endpoint within the NR update
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
	mu                      sync.Mutex
	lbCfg                   LBConfig                  // The latest balancer config received from the resolver.
	lastResolverErr         error                     // The latest error received from the resolver. Non-nil value results in the channel moving to TF with an error picker.
	assignment              *sharding.Assignment      // The latest assignment received from the autosharding client.
	assignmentErr           error                     // The latest error received from the autosharding client.
	autoshardingClientClose func()                    // The function to close the autosharding client (which also releases its gRPC channel). Nil before the first client is created.
	autoshardingClientGen   int                       // The generation number of the autosharding client. Incremented each time a new client is created.
	autoshardingTarget      string                    // The autosharding target used to create the autosharding client.
	endpointMap             map[string]*endpointState // Map of hostname to endpointState for all endpoints received from the resolver.
	// Map of endpoint (keyed by its set of addresses) to endpointState. This
	// contains the same endpointState objects as endpointMap, but is keyed the
	// same way as the children of the endpointsharding balancer. It is used to
	// apply child state updates, since a hostname may be reassigned to a
	// different endpoint by a resolver update, while an update from a child
	// always corresponds to a specific set of addresses.
	endpointsByAddrs    *resolver.EndpointMap[*endpointState]
	sliceMap            *sliceMap          // The latest sliceMap generated from the latest assignment and endpointMap.
	inhibitChildUpdates bool               // Set to true to inhibit updates from the child balancer when processing a new resolver update.
	closed              bool               // Set to true when the balancer is closed.
	state               connectivity.State // The most recently reported connectivity state.

	// The endpoint on which a connection attempt was most recently triggered
	// by connectToRandomIdleEndpointLocked (as opposed to by a pick), until it
	// reports that it has moved out of IDLE. See
	// connectToRandomIdleEndpointLocked for details.
	pendingConnectEndpoint *endpointState

	// The following fields are used to avoid reporting IDLE before the first
	// connection attempt is made. Reporting IDLE after reporting CONNECTING
	// would reset the failover timer in the priority LB policy (if that is our
	// parent), and moving back to CONNECTING when a pick triggers a connection
	// attempt would start a new failover timer, thereby delaying failover.
	//
	// initialConnectPending is set to true at build time, and is cleared when
	// either:
	// - at least one endpoint is seen to have moved out of IDLE (when computing
	//   the aggregated connectivity state), or
	// - TRANSIENT_FAILURE is reported. After that, the priority LB policy (if
	//   that is our parent) would have cancelled its failover timer, and
	//   reporting IDLE is harmless.
	//
	// While it is set, CONNECTING is reported instead of IDLE (which is the
	// state computed by the aggregation rules while all endpoints are IDLE),
	// and the rules for proactively triggering connection attempts are not
	// applied. Instead, when the first usable result is received from the
	// autosharding client, a timer is started to give queued RPCs a chance to
	// trigger a connection attempt to the endpoint they are routed to. If the
	// timer fires while initialConnectPending is still set, a connection
	// attempt is triggered to a random IDLE endpoint, and the timer is
	// restarted (in case that endpoint is removed by a resolver update before
	// it reports a new state).
	//
	// seenExitIdle is set when ExitIdle is called while initialConnectPending is
	// set and the initial connect timer is not running. If it is set when the
	// first usable result is received, a connection attempt is triggered to a
	// random IDLE endpoint immediately. If ExitIdle is called while the timer
	// is running, a connection attempt is triggered to a random IDLE endpoint
	// immediately, and the timer is left running.
	initialConnectPending   bool
	initialConnectTimerStop func() bool // Stops the initial connect timer. Nil if the timer is not running.
	initialConnectTimerGen  int         // Incremented every time the timer is started or stopped, to ignore stale timer callbacks.
	seenExitIdle            bool
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
		b.updateStateAndPickerLocked(true)
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

	// Process the received configuration.
	if err := b.handleNewConfiguration(ccs.ResolverState, newConfig); err != nil {
		return err
	}
	return childErr
}

// hostnameFromEndpoint extracts the hostname from the given resolver.Endpoint.
// If the endpoint has a hostname, it returns that. Otherwise, it falls back to
// using the first address in the endpoint's Addresses list.
//
// The endpoint must have at least one address in its Addresses list.
func hostnameFromEndpoint(ep resolver.Endpoint) string {
	if h := hostname.FromEndpoint(ep); h != "" {
		return h
	}
	return ep.Addresses[0].Addr
}

// handleNewEndpointsLocked processes the new endpoints received from the
// resolver and updates endpointMap and endpointsByAddrs accordingly. It returns
// the filtered list of endpoints to be passed to the endpointsharding child.
//
// The endpointState for an endpoint is looked up (and reused in place) using
// the endpoint's set of addresses, not its hostname. The connectivity state,
// picker and exitIdle function stored in an endpointState belong to a specific
// endpointsharding child, and endpointsharding identifies its children by their
// set of addresses. If a hostname is moved to a different set of addresses, the
// state of the old child must not be carried over to the new one.
func (b *autoshardingBalancer) handleNewEndpointsLocked(endpoints []resolver.Endpoint) []resolver.Endpoint {
	filteredEndpoints := make([]resolver.Endpoint, 0, len(endpoints))

	newEndpointMap := make(map[string]*endpointState)
	newEndpointsByAddrs := resolver.NewEndpointMap[*endpointState]()
	for _, ep := range endpoints {
		// Filter out endpoints with zero addresses.
		if len(ep.Addresses) == 0 {
			b.logger.Warningf("Ignoring endpoint %v with no addresses", ep)
			continue
		}

		// Filter out endpoints based on duplicate hostname.
		hostname := hostnameFromEndpoint(ep)
		if _, ok := newEndpointMap[hostname]; ok {
			b.logger.Warningf("Ignoring duplicate endpoint with hostname %q", hostname)
			continue
		}

		// Filter out endpoints based on duplicate addresses. This is because
		// endpointsharding drops children with duplicate addresses, and we want
		// to ensure that the endpointMap and sliceMap are consistent with what
		// the child balancer sees.
		if _, ok := newEndpointsByAddrs.Get(ep); ok {
			b.logger.Warningf("Ignoring endpoint %v with duplicate addresses", ep)
			continue
		}
		filteredEndpoints = append(filteredEndpoints, ep)

		// Reuse the existing endpointState if the endpointsharding child for
		// this set of addresses already exists. Otherwise, create a new one.
		// The child state of a new endpoint will be populated when the
		// endpointsharding child reports its state.
		es, ok := b.endpointsByAddrs.Get(ep)
		if !ok {
			es = &endpointState{}
		}
		es.index = len(newEndpointMap)
		newEndpointMap[hostname] = es
		newEndpointsByAddrs.Set(ep, es)
	}
	b.endpointMap = newEndpointMap
	b.endpointsByAddrs = newEndpointsByAddrs
	return filteredEndpoints
}

// handleNewConfiguration processes the new balancer configuration received from
// the resolver and updates the autosharding balancer's state accordingly. It
// handles changes to the channel factory key and autosharding target, creating
// new autosharding clients as needed. It also updates the lastResolverErr field
// to reflect any errors encountered during processing.
func (b *autoshardingBalancer) handleNewConfiguration(state resolver.State, newConfig *LBConfig) error {
	clientClose, err := b.handleNewConfigurationInternal(state, newConfig)
	if clientClose != nil {
		clientClose()
	}
	return err
}

// handleNewConfigurationInternal does the work of handleNewConfiguration while
// holding mu. It returns a function to close the previous autosharding client
// (if one needs to be closed), which must be invoked without holding mu.
func (b *autoshardingBalancer) handleNewConfigurationInternal(state resolver.State, newConfig *LBConfig) (clientClose func(), _ error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// If there is an error processing the configuration that is going to result
	// in the LB policy moving the channel to TF with an erroring picker, there
	// is no reason to keep the old autosharding client (and the gRPC channel
	// owned by it) open. So, we set the return value such that it closes any
	// existing autosharding client.
	//
	// We also reset the previous assignment/error because when we later process
	// the new configuration successfully, we would want to queue RPCs until we
	// receive a result from the autoshardingClient instead of using the
	// previous assignment/error values.
	defer func() {
		if b.lastResolverErr != nil {
			clientClose = b.autoshardingClientClose
			b.autoshardingClientClose = nil
			b.autoshardingTarget = ""
			b.autoshardingClientGen++

			b.assignment = nil
			b.assignmentErr = nil
		}
	}()

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
		return nil, balancer.ErrBadResolverState
	}

	provider := grpc.ClientConnProviderFromResolverState(state)
	if provider == nil {
		b.lastResolverErr = internal.ErrNoChannelFactory
		return nil, balancer.ErrBadResolverState
	}

	// Ensure %s string substitution for "Locality" is taken into account for
	// the autosharding_target field.
	locality := locality.FromResolverState(state)
	newTarget := strings.Replace(newConfig.AutoShardingTarget, "%s", locality, 1)

	// A new autosharding client is required if this is the first valid
	// configuration, or if the channel factory key or the autosharding target
	// has changed.
	channelFactoryKeyChanged := newConfig.ChannelFactoryKey != b.lbCfg.ChannelFactoryKey
	authshardingTargetChanged := newTarget != b.autoshardingTarget
	createNewAutoshardingClient := b.autoshardingClientClose == nil || channelFactoryKeyChanged || authshardingTargetChanged
	b.lbCfg = *newConfig
	if !createNewAutoshardingClient {
		return nil, nil
	}

	// Get a gRPC channel from the channel factory every time a new
	// autosharding client is created, even if the channel factory key has not
	// changed. The channel factory is expected to return a reference to an
	// existing channel for the same key, instead of creating a new one. The
	// autosharding client takes ownership of the channel, and releases it when
	// it is closed.
	if b.logger.V(2) {
		b.logger.Infof("Getting a gRPC channel with channel_factory_key: %q", newConfig.ChannelFactoryKey)
	}
	channel, channelClose, err := provider(newConfig.ChannelFactoryKey)
	if err != nil {
		if b.logger.V(2) {
			b.logger.Infof("Failed to get gRPC channel with channel_factory_key: %q, error: %v", newConfig.ChannelFactoryKey, err)
		}
		// If channel creation fails, pass a lame channel to the autosharding
		// client. This results in the client immediately returning an error to
		// the LB policy. And this will result in RPCs using fallback (if
		// enabled) or failing.
		channel = &lameChannel{err: status.Errorf(codes.Unavailable, "autosharding: failed to create gRPC channel to sharding service: %v", err)}
		channelClose = nil
	}

	// There is a small window of time when both the old and new autosharding
	// clients are active. We increment the generation number to ensure that
	// callbacks from the old client are ignored.
	//
	// The new client is created before the old one is closed (by the caller),
	// so that a channel shared between the two is not closed in between.
	b.autoshardingClientGen++
	curGen := b.autoshardingClientGen
	newClientClose := internal.NewAutoshardingClient(sharding.ClientOptions{
		CC:                       channel,
		CCClose:                  channelClose,
		AutoshardingTarget:       newTarget,
		UUID:                     b.uuid,
		InitialAssignmentTimeout: time.Duration(newConfig.InitialAssignmentTimeout),
		OnAssignmentUpdate:       func(assignment *sharding.Assignment) { b.onAssignmentUpdate(curGen, assignment) },
		OnAssignmentError:        func(err error) { b.onAssignmentError(curGen, err) },
	})
	prevClientClose := b.autoshardingClientClose
	b.autoshardingClientClose = newClientClose
	b.autoshardingTarget = newTarget
	return prevClientClose, nil
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

		firstResult := b.assignment == nil && b.assignmentErr == nil
		b.assignment = assignment
		b.assignmentErr = nil
		if firstResult {
			b.maybeStartInitialConnectLocked()
		}
		b.updateStateAndPickerLocked(true)
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

		firstResult := b.assignment == nil && b.assignmentErr == nil
		b.assignment = nil
		b.assignmentErr = err
		if firstResult {
			b.maybeStartInitialConnectLocked()
		}
		b.updateStateAndPickerLocked(true)
	})
}

// maybeStartInitialConnectLocked is called after the first result from the
// autosharding client has been stored. If the result can be used to route RPCs,
// and no endpoint has moved out of IDLE yet, it ensures that a connection
// attempt is eventually triggered.
//
// If ExitIdle was called before this result was received, there may be no RPCs
// queued, and a connection attempt is triggered to a random IDLE endpoint
// immediately. Otherwise, a timer is started to give queued RPCs a chance to
// trigger a connection attempt to the endpoint they are routed to. In both
// cases, the timer acts as a backstop that triggers a connection attempt to a
// random IDLE endpoint if no endpoint moves out of IDLE before it fires.
func (b *autoshardingBalancer) maybeStartInitialConnectLocked() {
	usable := b.assignment != nil || b.lbCfg.EnableFallback
	if !usable || !b.initialConnectPending {
		return
	}
	if b.seenExitIdle {
		b.connectToRandomIdleEndpointLocked()
	}
	b.startInitialConnectTimerLocked()
}

// startInitialConnectTimerLocked starts (or restarts) the initial connect
// timer.
func (b *autoshardingBalancer) startInitialConnectTimerLocked() {
	b.stopInitialConnectTimerLocked()
	gen := b.initialConnectTimerGen
	b.initialConnectTimerStop = internal.AfterFunc(internal.InitialConnectDelay, func() {
		b.onInitialConnectTimer(gen)
	})
}

// stopInitialConnectTimerLocked stops the initial connect timer, if running.
// It also ensures that a callback from a timer that has already fired is
// ignored.
func (b *autoshardingBalancer) stopInitialConnectTimerLocked() {
	b.initialConnectTimerGen++
	if b.initialConnectTimerStop != nil {
		b.initialConnectTimerStop()
		b.initialConnectTimerStop = nil
	}
}

// onInitialConnectTimer is invoked when the initial connect timer fires. If no
// endpoint has moved out of IDLE yet, it triggers a connection attempt to a
// random IDLE endpoint and restarts the timer. The timer is restarted to handle
// the case where the chosen endpoint is removed by a resolver update before it
// reports a new state.
func (b *autoshardingBalancer) onInitialConnectTimer(gen int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed || gen != b.initialConnectTimerGen {
		return
	}
	b.initialConnectTimerStop = nil
	if !b.initialConnectPending {
		return
	}
	if b.logger.V(2) {
		b.logger.Infof("Initial connect timer fired with no endpoint out of IDLE, triggering connection attempt to a random endpoint")
	}
	b.connectToRandomIdleEndpointLocked()
	b.startInitialConnectTimerLocked()
}

// clearInitialConnectLocked clears all state associated with avoiding
// reporting IDLE before the first connection attempt is made.
func (b *autoshardingBalancer) clearInitialConnectLocked() {
	b.initialConnectPending = false
	b.seenExitIdle = false
	b.stopInitialConnectTimerLocked()
}

// connectToRandomIdleEndpointLocked triggers a connection attempt to an IDLE
// endpoint chosen at random, unless an endpoint is already CONNECTING.
//
// Connection attempts are triggered asynchronously (the endpointsharding child
// runs ExitIdle in a goroutine), so this may be invoked again before the
// chosen endpoint reports that it has moved out of IDLE. To avoid triggering
// connection attempts to multiple endpoints in that case, the chosen endpoint
// is remembered, and is reused as long as it is still IDLE.
func (b *autoshardingBalancer) connectToRandomIdleEndpointLocked() {
	idle := make([]*endpointState, 0, len(b.endpointMap))
	for _, es := range b.endpointMap {
		if es.connectivityState == connectivity.Connecting {
			return
		}
		if es.connectivityState == connectivity.Idle && es.exitIdle != nil {
			idle = append(idle, es)
		}
	}
	if len(idle) == 0 {
		return
	}
	if p := b.pendingConnectEndpoint; p != nil && slices.Contains(idle, p) {
		p.exitIdle()
		return
	}
	// Sort by index so that the choice is a deterministic function of the
	// random number, since map iteration order is random.
	slices.SortFunc(idle, func(a, b *endpointState) int { return a.index - b.index })
	es := idle[internal.RandIntN(len(idle))]
	b.pendingConnectEndpoint = es
	es.exitIdle()
}

// updateStateLocked records the given state as the most recently reported
// state and reports it along with the given picker to the parent.
func (b *autoshardingBalancer) updateStateLocked(state connectivity.State, picker balancer.Picker) {
	b.state = state
	b.ClientConn.UpdateState(balancer.State{ConnectivityState: state, Picker: picker})
}

// updateStateAndPickerLocked updates the picker based on the current state of
// the balancer. It computes the aggregated connectivity state based on the
// child states of the endpoints and updates the ClientConn with the new state
// and picker.
//
// If regenerateSliceMap is true, the sliceMap is rebuilt from the current
// endpointMap and assignment before anything else. This is done ahead of the
// early returns below so that the sliceMap is never left out of sync with the
// endpointMap, regardless of whether a new picker is produced by this call.
func (b *autoshardingBalancer) updateStateAndPickerLocked(regenerateSliceMap bool) {
	if regenerateSliceMap {
		if b.logger.V(2) {
			b.logger.Infof("Regenerating slice map")
		}
		b.sliceMap = buildSliceMap(b.endpointMap, b.assignment)
	}

	if b.inhibitChildUpdates {
		return
	}

	// If the resolver returned an error (before returning any valid
	// configuration) or the most recent update from the resolver was invalid
	// (for example, no channel factory was found in the resolver state or it
	// contained no endpoints), report TRANSIENT_FAILURE and surface the error
	// in failing RPCs.
	if b.lastResolverErr != nil {
		b.clearInitialConnectLocked()
		b.updateStateLocked(connectivity.TransientFailure, base.NewErrPicker(b.lastResolverErr))
		return
	}

	// No assignment or error from the autosharding client yet. Queue RPCs.
	if b.assignment == nil && b.assignmentErr == nil {
		// TODO(easwars/mbissa): Add autosharding_assignment_pending as the
		// delay type here once A121 is ready.
		b.updateStateLocked(connectivity.Connecting, base.NewErrPicker(balancer.ErrNoSubConnAvailable))
		return
	}

	// If the autosharding client returned an error and fallback is disabled,
	// report TRANSIENT_FAILURE and surface the error in failing RPCs. If
	// fallback is enabled, we will continue to use the fallback pool for picks.
	if b.assignmentErr != nil && !b.lbCfg.EnableFallback {
		b.clearInitialConnectLocked()
		b.updateStateLocked(connectivity.TransientFailure, base.NewErrPicker(b.assignmentErr))
		return
	}

	// Compute the aggregated connectivity state. This also clears
	// initialConnectPending if any endpoint has moved out of IDLE.
	aggState := b.computeAggregatedConnectivityStateLocked()

	// If no endpoint has moved out of IDLE yet, report CONNECTING instead of
	// IDLE, and do not proactively trigger connection attempts. See the comment
	// on initialConnectPending for details.
	if aggState == connectivity.Idle && b.initialConnectPending {
		b.updateStateLocked(connectivity.Connecting, newPicker(b.endpointMap, b.sliceMap, &b.lbCfg))
		return
	}

	// The specific behavior that will enable this LB policy to stop reporting
	// TRANSIENT_FAILURE even when it is not receiving picks will be that
	// whenever this policy receives a subchannel connectivity state update or a
	// resolver update, if the aggregated connectivity state is
	// TRANSIENT_FAILURE or CONNECTING and there are no endpoints in CONNECTING
	// state, the policy will choose one of the endpoints in IDLE state (if any)
	// at random to trigger a connection attempt on. - A119
	if aggState == connectivity.Connecting || aggState == connectivity.TransientFailure {
		b.connectToRandomIdleEndpointLocked()
	}

	b.updateStateLocked(aggState, newPicker(b.endpointMap, b.sliceMap, &b.lbCfg))
}

// computeAggregatedConnectivityStateLocked computes the aggregated connectivity
// state of the autosharding balancer based on the following rules:
//   - If there is at least one endpoint in READY state, report READY.
//   - If there are 2 or more endpoints in TRANSIENT_FAILURE state, report
//     TRANSIENT_FAILURE.
//   - If there is at least one endpoint in CONNECTING state, report CONNECTING.
//   - If there is one endpoint in TRANSIENT_FAILURE and there is more than one
//     endpoint, report state CONNECTING.
//   - If there is at least one endpoint in Idle state, report Idle.
//   - Otherwise, report TRANSIENT_FAILURE.
//
// Since it already iterates over all endpoints, it also clears
// initialConnectPending once any endpoint has moved out of IDLE, since the
// aggregated state can no longer be IDLE after that.
func (b *autoshardingBalancer) computeAggregatedConnectivityStateLocked() connectivity.State {
	var nums [5]int
	for _, es := range b.endpointMap {
		nums[es.connectivityState]++
	}

	if b.initialConnectPending && nums[connectivity.Idle] != len(b.endpointMap) {
		b.clearInitialConnectLocked()
	}

	switch {
	case nums[connectivity.Ready] > 0:
		return connectivity.Ready
	case nums[connectivity.TransientFailure] > 1:
		return connectivity.TransientFailure
	case nums[connectivity.Connecting] > 0:
		return connectivity.Connecting
	case nums[connectivity.TransientFailure] == 1 && len(b.endpointMap) > 1:
		return connectivity.Connecting
	case nums[connectivity.Idle] > 0:
		return connectivity.Idle
	default:
		return connectivity.TransientFailure
	}
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
	// invalid configuration (e.g. missing channel factory in resolver state).
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

// ExitIdle is called when the channel is explicitly asked to connect (for
// example, when the application calls Connect() on the channel). Connections
// are otherwise triggered from picks or from child balancer state changes.
func (b *autoshardingBalancer) ExitIdle() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	if b.initialConnectPending {
		if b.initialConnectTimerStop != nil {
			// The first usable result has been received from the autosharding
			// client, and the initial connect timer is running. Don't wait for
			// it to fire. Trigger a connection attempt to a random endpoint
			// now. The timer is left running as a backstop, in case that
			// endpoint is removed before it reports a new state.
			b.connectToRandomIdleEndpointLocked()
		} else {
			// Record that ExitIdle was called, so that a connection attempt
			// can be triggered as soon as the first usable result is received
			// from the autosharding client, instead of waiting for the initial
			// connect timer.
			b.seenExitIdle = true
		}
	}
	if b.state == connectivity.Idle {
		b.connectToRandomIdleEndpointLocked()
	}
}

func (b *autoshardingBalancer) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.clearInitialConnectLocked()
	clientClose := b.autoshardingClientClose
	b.autoshardingClientClose = nil
	b.serializerCancel()
	b.lastResolverErr = nil
	b.endpointMap = nil
	b.pendingConnectEndpoint = nil
	b.endpointsByAddrs = resolver.NewEndpointMap[*endpointState]()
	b.sliceMap = nil
	b.assignment = nil
	b.assignmentErr = nil
	b.mu.Unlock()

	// Called outside of the lock to avoid deadlocks caused by lock order
	// inversion between the autosharding balancer and the child balancer.
	// Closing the autosharding client also releases its gRPC channel.
	if clientClose != nil {
		clientClose()
	}
	b.child.Close()

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
		// Look up the endpointState using the endpoint's set of addresses,
		// which is how endpointsharding identifies its children. Looking up by
		// hostname is unsafe because a resolver update may have reassigned the
		// hostname to a different endpoint, while this update could contain
		// state for the child associated with the old endpoint.
		es, ok := b.endpointsByAddrs.Get(cs.Endpoint)
		if !ok {
			// This can happen when a child update races with a resolver update
			// that removed the endpoint.
			if b.logger.V(2) {
				b.logger.Infof("Ignoring child state for unknown endpoint %v", cs.Endpoint)
			}
			continue
		}
		es.connectivityState = cs.State.ConnectivityState
		es.picker = cs.State.Picker
		es.exitIdle = cs.ExitIdle
		if es == b.pendingConnectEndpoint && es.connectivityState != connectivity.Idle {
			b.pendingConnectEndpoint = nil
		}
	}
	b.updateStateAndPickerLocked(false)
}

// lameChannel is a gRPC channel that always returns the configured error for
// any RPC.
type lameChannel struct {
	err error
}

func (c *lameChannel) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return c.err
}

func (c *lameChannel) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, c.err
}
