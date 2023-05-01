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
	"strings"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/buffer"
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

// ccResolverWrapper is a wrapper on top of cc for resolvers.
// It implements resolver.ClientConn interface.
type ccResolverWrapper struct {
	// The following fields are initialized when the wrapper is created and are
	// read-only afterwards, and therefore can be accessed without a mutex.
	cc                  resolverStateUpdater
	closed              *grpcsync.Event
	channelzID          *channelz.Identifier
	ignoreServiceConfig bool
	outgoingCh          *buffer.Unbounded

	// Outgoing (gRPC --> resolver) and incoming (resolver --> gRPC) calls are
	// guaranteed to execute in a mutually exclusive manner as they are
	// scheduled on the CallbackSerializer. Fields accessed *only* in serializer
	// callbacks, can therefore be accessed without a mutex.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel context.CancelFunc
	resolver         resolver.Resolver
	curState         resolver.State
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
		closed:              grpcsync.NewEvent(),
		channelzID:          opts.channelzID,
		ignoreServiceConfig: opts.bOpts.DisableServiceConfig,
		outgoingCh:          buffer.NewUnbounded(),
		serializer:          grpcsync.NewCallbackSerializer(ctx),
		serializerCancel:    cancel,
	}

	r, err := opts.builder.Build(opts.target, ccr, opts.bOpts)
	if err != nil {
		cancel()
		return nil, err
	}
	ccr.resolver = r
	return ccr, nil
}

func (ccr *ccResolverWrapper) resolveNow(o resolver.ResolveNowOptions) {
	ccr.serializer.Schedule(func(_ context.Context) {
		ccr.resolver.ResolveNow(o)
	})
}

func (ccr *ccResolverWrapper) close() {
	ccr.closed.Fire()

	done := make(chan struct{})
	ccr.serializer.Schedule(func(_ context.Context) {
		ccr.resolver.Close()
		close(done)
	})
	<-done
	ccr.serializerCancel()
}

// UpdateState is called by resolver implementations to report new state to gRPC
// which includes addresses and service config.
func (ccr *ccResolverWrapper) UpdateState(s resolver.State) error {
	errCh := make(chan error, 1)
	ccr.serializer.Schedule(func(_ context.Context) {
		if ccr.closed.HasFired() {
			errCh <- nil
			return
		}
		ccr.addChannelzTraceEvent(s)
		ccr.curState = s
		if err := ccr.cc.updateResolverState(ccr.curState, nil); err == balancer.ErrBadResolverState {
			errCh <- balancer.ErrBadResolverState
			return
		}
		errCh <- nil
	})

	select {
	case err := <-errCh:
		return err
	case <-ccr.closed.Done():
		return nil
	}
}

// ReportError is called by resolver implementations to report errors
// encountered during name resolution to gRPC.
func (ccr *ccResolverWrapper) ReportError(err error) {
	ccr.serializer.Schedule(func(_ context.Context) {
		if ccr.closed.HasFired() {
			return
		}
		channelz.Warningf(logger, ccr.channelzID, "ccResolverWrapper: reporting error to cc: %v", err)
		ccr.cc.updateResolverState(resolver.State{}, err)
	})
}

// NewAddress is called by the resolver implementation to send addresses to
// gRPC.
func (ccr *ccResolverWrapper) NewAddress(addrs []resolver.Address) {
	ccr.serializer.Schedule(func(_ context.Context) {
		if ccr.closed.HasFired() {
			return
		}
		ccr.addChannelzTraceEvent(resolver.State{Addresses: addrs, ServiceConfig: ccr.curState.ServiceConfig})
		ccr.curState.Addresses = addrs
		ccr.cc.updateResolverState(ccr.curState, nil)
	})
}

// NewServiceConfig is called by the resolver implementation to send service
// configs to gRPC.
func (ccr *ccResolverWrapper) NewServiceConfig(sc string) {
	ccr.serializer.Schedule(func(_ context.Context) {
		if ccr.closed.HasFired() {
			return
		}
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
