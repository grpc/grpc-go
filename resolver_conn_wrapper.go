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
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// resolverStateUpdater wraps the single method used by ccResolverWrapper to
// report a state update from the actual resolver implementation.
type resolverStateUpdater interface {
	updateResolverState(s resolver.State, err error) error
}

type ccrMode int

const (
	ccrModeActive = iota
	ccrModeIdleOrClosed
	ccrModeExitingIdle
)

// ccResolverWrapper is a wrapper on top of cc for resolvers.
// It implements resolver.ClientConn interface.
type ccResolverWrapper struct {
	// The following fields are initialized when the wrapper is created and are
	// read-only afterwards, and therefore can be accessed without a mutex.
	cc                  resolverStateUpdater
	channelzID          *channelz.Identifier
	ignoreServiceConfig bool
	opts                ccResolverWrapperOpts

	// All incoming (resolver --> gRPC) calls are guaranteed to execute in a
	// mutually exclusive manner as they are scheduled on the serializer.
	// Fields accessed *only* in these serializer callbacks, can therefore be
	// accessed without a mutex.
	curState resolver.State

	// mu guards access to the below fields. Access to the serializer and its
	// cancel function needs to be mutex protected because they are overwritten
	// when the wrapper exits idle mode.
	mu               sync.Mutex
	serializer       *grpcsync.CallbackSerializer // To serialize all incoming calls.
	serializerCancel context.CancelFunc           // To close the serializer at close/enterIdle time.
	mode             ccrMode                      // Tracks the current mode of the wrapper.
	resolver         resolver.Resolver            // Accessed only from outgoing calls.
	pendingResolve   *resolver.ResolveNowOptions  // Set when there is a pending call to ResolveNow().
}

// ccResolverWrapperOpts wraps the arguments to be passed when creating a new
// ccResolverWrapper.
type ccResolverWrapperOpts struct {
	target     resolver.Target       // User specified dial target to resolve.
	builder    resolver.Builder      // Resolver builder to use.
	bOpts      resolver.BuildOptions // Resolver build options to use.
	channelzID *channelz.Identifier  // Channelz identifier for the channel.
}

// newCCResolverWrapper uses the resolver.Builder to build a Resolver and
// returns a ccResolverWrapper object which wraps the newly built resolver.
func newCCResolverWrapper(cc resolverStateUpdater, opts ccResolverWrapperOpts) (*ccResolverWrapper, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ccr := &ccResolverWrapper{
		cc:                  cc,
		channelzID:          opts.channelzID,
		ignoreServiceConfig: opts.bOpts.DisableServiceConfig,
		opts:                opts,
		serializer:          grpcsync.NewCallbackSerializer(ctx),
		serializerCancel:    cancel,
	}

	// Cannot hold the lock at build time because the resolver can send an
	// update or error inline and these incoming calls grab the lock to schedule
	// a callback in the serializer.
	r, err := opts.builder.Build(opts.target, ccr, opts.bOpts)
	if err != nil {
		cancel()
		return nil, err
	}

	// Any error reported by the resolver at build time that leads to a
	// re-resolution request from the balancer is dropped by grpc until we
	// return from this function. So, we don't have to handle pending resolveNow
	// requests here.
	ccr.mu.Lock()
	ccr.resolver = r
	ccr.mu.Unlock()

	return ccr, nil
}

func (ccr *ccResolverWrapper) resolveNow(o resolver.ResolveNowOptions) {
	ccr.mu.Lock()
	defer ccr.mu.Unlock()

	switch ccr.mode {
	case ccrModeActive:
		ccr.resolver.ResolveNow(o)
	case ccrModeIdleOrClosed:
		// Do nothing.
	case ccrModeExitingIdle:
		// Set the `pendingResolve` field here so that the re-resolution request
		// is handled properly after we exit idle mode.
		ccr.pendingResolve = &o
	}
}

func (ccr *ccResolverWrapper) close() {
	channelz.Info(logger, ccr.channelzID, "ccResolverWrapper: closing")
	ccr.handleCloseAndEnterIdle()
}

func (ccr *ccResolverWrapper) enterIdleMode() {
	channelz.Info(logger, ccr.channelzID, "ccResolverWrapper: entering idle mode")
	ccr.handleCloseAndEnterIdle()
}

// handleCloseAndEnterIdle is invoked with the channel is being closed or when
// it enters idle mode upon expiry of idle_timeout.
func (ccr *ccResolverWrapper) handleCloseAndEnterIdle() {
	ccr.mu.Lock()
	if ccr.mode != ccrModeActive {
		ccr.mu.Unlock()
		return
	}

	// Close the serializer to ensure that no more calls from the resolver are
	// handled, before actually closing the resolver.
	ccr.serializerCancel()
	ccr.mode = ccrModeIdleOrClosed
	done := ccr.serializer.Done
	r := ccr.resolver
	ccr.mu.Unlock()

	// Give enqueued callbacks a chance to finish before closing the balancer.
	<-done

	// Resolver close needs to be called outside the lock because these methods
	// are generally blocking and don't return until they stop any pending
	// goroutines and cleanup allocated resources. And since the main goroutine
	// of the resolver might be reporting an error or state update at the same
	// time as close, and the former needs to grab the lock to schedule a
	// callback on the serializer, it will lead to a deadlock if we hold the
	// lock here.
	r.Close()
}

// exitIdleMode is invoked by grpc when the channel exits idle mode either
// because of an RPC or because of an invocation of the Connect() API. This
// recreates the resolver that was closed previously when entering idle mode.
func (ccr *ccResolverWrapper) exitIdleMode() error {
	channelz.Info(logger, ccr.channelzID, "ccResolverWrapper: exiting idle mode")

	ccr.mu.Lock()
	ccr.mode = ccrModeExitingIdle
	ctx, cancel := context.WithCancel(context.Background())
	ccr.serializer = grpcsync.NewCallbackSerializer(ctx)
	ccr.serializerCancel = cancel
	ccr.mu.Unlock()

	// See newCCResolverWrapper() to see why we cannot hold the lock here.
	r, err := ccr.opts.builder.Build(ccr.opts.target, ccr, ccr.opts.bOpts)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to build resolver when exiting idle mode: %v", err)
	}

	ccr.mu.Lock()
	ccr.mode = ccrModeActive
	ccr.resolver = r
	if ccr.pendingResolve != nil {
		ccr.resolver.ResolveNow(*ccr.pendingResolve)
		ccr.pendingResolve = nil
	}
	ccr.mu.Unlock()
	return nil
}

// serializerScheduleLocked is a convenience method to schedule a function to be
// run on the serializer while holding ccr.mu.
func (ccr *ccResolverWrapper) serializerScheduleLocked(f func(context.Context)) {
	ccr.mu.Lock()
	ccr.serializer.Schedule(f)
	ccr.mu.Unlock()
}

// UpdateState is called by resolver implementations to report new state to gRPC
// which includes addresses and service config.
func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error {
	// We cannot use serializerScheduleLocked() here because we need to ensure
	// that these two operations execute atomically:
	// - checking that the wrapper is not closed or idle, and
	// - scheduling the function in the serializer
	//
	// If we do those steps in a non atomic way, i.e grab the lock, check the
	// mode, release the lock, then use the serializerScheduleLocked() method,
	// we could run into a race where the wrapper is active when we check the
	// mode, but enters idle or is closed before we schedule the function on the
	// serializer. This would lead to the scheduled function never executing,
	// and since we block on the error value returned by that function, we would
	// end up blocking forever. This requirement does not exist for other
	// incoming calls because we don't have to return a value to the caller, and
	// therefore it is fine if the scheduled function never executes (because
	// the wrapper is closed or enters idle before it gets to run).
	ccr.mu.Lock()
	if ccr.mode == ccrModeIdleOrClosed {
		ccr.mu.Unlock()
		return nil
	}
	errCh := make(chan error, 1)
	ccr.serializer.Schedule(func(context.Context) {
		ccr.addChannelzTraceEvent(s)
		ccr.curState = s
		if err := ccr.cc.updateResolverState(ccr.curState, nil); err == balancer.ErrBadResolverState {
			errCh <- balancer.ErrBadResolverState
			return
		}
		errCh <- nil
	})
	ccr.mu.Unlock()
	return <-errCh
}

// ReportError is called by resolver implementations to report errors
// encountered during name resolution to gRPC.
func (ccr *ccResolverWrapper) ReportError(err error) {
	ccr.serializerScheduleLocked(func(_ context.Context) {
		channelz.Warningf(logger, ccr.channelzID, "ccResolverWrapper: reporting error to cc: %v", err)
		ccr.cc.updateResolverState(resolver.State{}, err)
	})
}

// NewAddress is called by the resolver implementation to send addresses to
// gRPC.
func (ccr *ccResolverWrapper) NewAddress(addrs []resolver.Address) {
	ccr.serializerScheduleLocked(func(_ context.Context) {
		ccr.addChannelzTraceEvent(resolver.State{Addresses: addrs, ServiceConfig: ccr.curState.ServiceConfig})
		ccr.curState.Addresses = addrs
		ccr.cc.updateResolverState(ccr.curState, nil)
	})
}

// NewServiceConfig is called by the resolver implementation to send service
// configs to gRPC.
func (ccr *ccResolverWrapper) NewServiceConfig(sc string) {
	ccr.serializerScheduleLocked(func(_ context.Context) {
		channelz.Infof(logger, ccr.channelzID, "ccResolverWrapper: got new service config: %s", sc)
		if ccr.ignoreServiceConfig {
			channelz.Info(logger, ccr.channelzID, "Service config lookups disabled; ignoring config")
			return
		}
		scpr := parseServiceConfig(sc)
		if scpr.Err != nil {
			channelz.Warningf(logger, ccr.channelzID, "ccResolverWrapper: error parsing service config: %v", scpr.Err)
			return
		}
		ccr.addChannelzTraceEvent(resolver.State{Addresses: ccr.curState.Addresses, ServiceConfig: scpr})
		ccr.curState.ServiceConfig = scpr
		ccr.cc.updateResolverState(ccr.curState, nil)
	})
}

// ParseServiceConfig is called by resolver implementations to parse a JSON
// representation of the service config.
func (ccr *ccResolverWrapper) ParseServiceConfig(scJSON string) *serviceconfig.ParseResult {
	return parseServiceConfig(scJSON)
}

// addChannelzTraceEvent adds a channelz trace event containing the new
// state received from resolver implementations.
func (ccr *ccResolverWrapper) addChannelzTraceEvent(s resolver.State) {
	var updates []string
	var oldSC, newSC *ServiceConfig
	var oldOK, newOK bool
	if ccr.curState.ServiceConfig != nil {
		oldSC, oldOK = ccr.curState.ServiceConfig.Config.(*ServiceConfig)
	}
	if s.ServiceConfig != nil {
		newSC, newOK = s.ServiceConfig.Config.(*ServiceConfig)
	}
	if oldOK != newOK || (oldOK && newOK && oldSC.rawJSONString != newSC.rawJSONString) {
		updates = append(updates, "service config updated")
	}
	if len(ccr.curState.Addresses) > 0 && len(s.Addresses) == 0 {
		updates = append(updates, "resolver returned an empty address list")
	} else if len(ccr.curState.Addresses) == 0 && len(s.Addresses) > 0 {
		updates = append(updates, "resolver returned new addresses")
	}
	channelz.Infof(logger, ccr.channelzID, "Resolver state updated: %s (%v)", pretty.ToJSON(s), strings.Join(updates, "; "))
}
