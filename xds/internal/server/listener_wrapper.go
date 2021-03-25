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

// Package server contains internal server-side functionality used by the public
// facing xds package.
package server

import (
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

var logger = grpclog.Component("xds")

func prefixLogger(p *listenerWrapper) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[xds-server-listener %p]", p))
}

// XDSClientInterface wraps the methods on the xdsClient which are required by
// the listenerWrapper.
type XDSClientInterface interface {
	WatchListener(string, func(xdsclient.ListenerUpdate, error)) func()
	BootstrapConfig() *bootstrap.Config
}

// ListenerWrapperParams wraps parameters required to create a listenerWrapper.
type ListenerWrapperParams struct {
	// Listener is the net.Listener passed by the user that is to be wrapped.
	Listener net.Listener
	// ListenerResourceName is the xDS Listener resource to request.
	ListenerResourceName string
	// XDSCredsInUse specifies whether or not the user expressed interest to
	// receive security configuration from the control plane.
	XDSCredsInUse bool
	// XDSClient provides the functionality from the xdsClient required here.
	XDSClient XDSClientInterface
}

// NewListenerWrapper creates a new listenerWrapper with params. It returns a
// net.Listener and a channel which is written to, indicating that the former is
// ready to be passed to grpc.Serve().
func NewListenerWrapper(params ListenerWrapperParams) (net.Listener, <-chan struct{}) {
	lw := &listenerWrapper{
		Listener:      params.Listener,
		name:          params.ListenerResourceName,
		xdsCredsInUse: params.XDSCredsInUse,
		xdsC:          params.XDSClient,

		closed:     grpcsync.NewEvent(),
		goodUpdate: grpcsync.NewEvent(),
	}
	lw.logger = prefixLogger(lw)

	cancelWatch := lw.xdsC.WatchListener(lw.name, lw.handleListenerUpdate)
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

	name          string
	xdsCredsInUse bool
	xdsC          XDSClientInterface
	cancelWatch   func()

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

	// Make sure that the socket address on the received Listener resource
	// matches the address of the net.Listener passed to us by the user. This
	// check is done here instead of at the xdsClient layer because of the
	// following couple of reasons:
	// - xdsClient cannot know the listening address of every listener in the
	//   system, and hence cannot perform this check.
	// - this is a very context-dependent check and only the server has the
	//   appropriate context to perform this check.
	//
	// What this means is that the xdsClient has ACKed a resource which is going
	// to push the server into a "not serving" state. This is not ideal, but
	// this is what we have decided to do. See gRPC A36 for more details.
	// TODO(easwars): Switch to "not serving" if the host:port does not match.
	lisAddr := l.Listener.Addr().String()
	addr, port, err := net.SplitHostPort(lisAddr)
	if err != nil {
		// This is never expected to return a non-nil error since we have made
		// sure that the listener is a TCP listener at the beginning of Serve().
		// This is simply paranoia.
		l.logger.Warningf("Local listener address %q failed to parse as IP:port: %v", lisAddr, err)
		return
	}
	ilc := update.InboundListenerCfg
	if ilc == nil {
		l.logger.Warningf("Missing host:port in Listener updates")
		return
	}
	if ilc.Address != addr || ilc.Port != port {
		l.logger.Warningf("Received host:port (%s:%d) in Listener update does not match local listening address: %s", ilc.Address, ilc.Port, lisAddr)
		return
	}

	l.mu.Lock()
	l.filterChains = ilc.FilterChains
	l.defaultFilterChain = ilc.DefaultFilterChain
	l.mu.Unlock()
	l.goodUpdate.Fire()
	// TODO: Move to serving state on receipt of a good response.
}
