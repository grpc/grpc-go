/*
 *
 * Copyright 2021 gRPC authors.
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

// Package server contains server-side functionality used by the public facing
// xds package.
package server

import (
	"net"
	"sync"

	"google.golang.org/grpc/credentials/tls/certprovider"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

// ListenerWrapperParams wraps parameters required to create a listenerWrapper.
type ListenerWrapperParams struct {
	// Listener is the net.Listener passed by the user that is to be wrapped.
	Listener net.Listener
	// Logger is used to log messages with a configured prefix.
	Logger *internalgrpclog.PrefixLogger
	// ListenerResourceName is the xDS Listener resource to request.
	ListenerResourceName string
	// XDSCredsInUse specifies whether or not the user expressed interest to
	// receive security configuration from the control plane.
	XDSCredsInUse bool
	// CertProviderConfigs contains the parsed certificate provider
	// configuration from the bootstrap file.
	CertProviderConfigs map[string]*certprovider.BuildableConfig
	// WatchFunc is the function to be used to register a Listener watch. It
	// returns a function that is to be to used to cancel the watch, when this
	// listenerWrapper is no longer interested in the resource being watched.
	WatchFunc func(string, func(xdsclient.ListenerUpdate, error)) func()
	// BuildProvider is the function to be used to build certificate provider
	// instances. The arguments passed to it are the certificate provider
	// configs from bootstrap, certificate provider instance name, certificate
	// name, whether an identity certificate is requested and whether a root
	// certificate is requested.
	BuildProviderFunc func(map[string]*certprovider.BuildableConfig, string, string, bool, bool) (certprovider.Provider, error)
}

// NewListenerWrapper creates a new listenerWrapper with params.
func NewListenerWrapper(params ListenerWrapperParams) (net.Listener, <-chan struct{}) {
	lw := &listenerWrapper{
		Listener:            params.Listener,
		logger:              params.Logger,
		name:                params.ListenerResourceName,
		xdsCredsInUse:       params.XDSCredsInUse,
		certProviderConfigs: params.CertProviderConfigs,
		watchFunc:           params.WatchFunc,
		buildProviderFunc:   params.BuildProviderFunc,

		closed:     grpcsync.NewEvent(),
		goodUpdate: grpcsync.NewEvent(),
	}

	cancelWatch := lw.watchFunc(lw.name, lw.handleListenerUpdate)
	lw.logger.Infof("Watch started on resource name %v", lw.name)
	lw.cancelWatch = func() {
		cancelWatch()
		lw.logger.Infof("Watch cancelled on resource name %v", lw.name)
	}
	return lw, lw.goodUpdate.Done()
}

// listenerWrapper wraps the net.Listener associated with the listening address
// passed to Serve(). It also contains all other state associated with this
// particular invocation of Serve().
type listenerWrapper struct {
	net.Listener
	logger *internalgrpclog.PrefixLogger

	// TODO: Maintain serving state of this listener.

	name                string
	xdsCredsInUse       bool
	certProviderConfigs map[string]*certprovider.BuildableConfig
	watchFunc           func(string, func(xdsclient.ListenerUpdate, error)) func()
	cancelWatch         func()
	buildProviderFunc   func(map[string]*certprovider.BuildableConfig, string, string, bool, bool) (certprovider.Provider, error)

	// This is used to notify that a good update has been received and that
	// Serve() can be invoked on the underlying gRPC server. Using an event
	// instead of a vanilla channel simplifies the update handler as it need not
	// keep track of whether the received update is the first one or not.
	goodUpdate *grpcsync.Event
	// A small race exists in the xdsClient code between the receipt of an xDS
	// response and the user cancelling the associated watch. In this window,
	// the registered callback may be invoked after the watch is canceled, and
	// the user is expected to work around this. This event signifies that the
	// listener is closed (and hence the watch is cancelled), and we drop any
	// updates received in the callback if this event has fired.
	closed *grpcsync.Event

	// Filter chains received as part of the last good update. The reason for
	// using an rw lock here is that this field will be read by all connections
	// during their server-side handshake (in the hot path), but writes to this
	// happen rarely (when we get a Listener resource update).
	mu                 sync.RWMutex
	filterChains       []*xdsclient.FilterChain
	defaultFilterChain *xdsclient.FilterChain
}

// Accept blocks on an Accept() on the underlying listener, and wraps the
// returned net.connWrapper with the configured certificate providers.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	// TODO: Close connections if in "non-serving" state.
	return &connWrapper{Conn: c, parent: l}, nil
}

// Close closes the underlying listener. It also cancels the xDS watch
// registered in Serve() and closes any certificate provider instances created
// based on security configuration received in the LDS response.
func (l *listenerWrapper) Close() error {
	l.closed.Fire()
	l.Listener.Close()
	if l.cancelWatch != nil {
		l.cancelWatch()
	}
	return nil
}

func (l *listenerWrapper) handleListenerUpdate(update xdsclient.ListenerUpdate, err error) {
	if l.closed.HasFired() {
		l.logger.Warningf("Resource %q received update: %v with error: %v, after listener was closed", l.name, update, err)
		return
	}

	// TODO: Handle resource-not-found errors by moving to not-serving state.
	if err != nil {
		// We simply log an error here and hope we get a successful update
		// in the future. The error could be because of a timeout or an
		// actual error, like the requested resource not found. In any case,
		// it is fine for the server to hang indefinitely until Stop() is
		// called.
		l.logger.Warningf("Received error for resource %q: %+v", l.name, err)
		return
	}
	l.logger.Infof("Received update for resource %q: %+v", l.name, update)

	l.mu.Lock()
	l.filterChains = update.FilterChains
	l.defaultFilterChain = update.DefaultFilterChain
	l.mu.Unlock()
	l.goodUpdate.Fire()
	// TODO: Move to serving state on receipt of a good response.
}
