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

package xds

import (
	"net"
	"sync"

	"google.golang.org/grpc/credentials/tls/certprovider"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

// listenerWrapper wraps the net.Listener associated with the listening address
// passed to Serve(). It also contains all other state associated with this
// particular invocation of Serve().
type listenerWrapper struct {
	net.Listener
	logger *internalgrpclog.PrefixLogger

	// TODO: Maintain serving state of this listener.

	// Name of the Listener resource associated with this wrapper.
	name string
	// Func to cancel the watch on the associated Listener resource.
	// Invoked when this listenerWrapper is closed.
	cancelWatch func()
	// A small race exists in the xdsClient code between the receipt of an xDS
	// response and the user cancelling the associated watch. In this window,
	// the registered callback may be invoked after the watch is canceled, and
	// the user is expected to work around this. This event signifies that the
	// listener is closed (and hence the watch is cancelled), and we drop any
	// updates received in the callback if this event has fired.
	closed *grpcsync.Event
	// Set to true when the user has configured xDS credentials. If false, the
	// security configuration will not be processed.
	xdsCredsInUse bool
	// certProviderConfigs contains a mapping from certificate provider plugin
	// instance names to parsed buildable configs. This is used at handshake
	// time to build certificate providers.
	certProviderConfigs map[string]*certprovider.BuildableConfig

	// Filter chains received as part of the last good update. The reason for
	// using an rw lock here is that this field will be read by all connections
	// during their server-side handshake (in the hot path), but writes to this
	// happen rarely (when we get a Listener resource update).
	mu                 sync.RWMutex
	filterChains       []*xdsclient.FilterChain
	defaultFilterChain *xdsclient.FilterChain
}

// Accept blocks on an Accept() on the underlying listener, and wraps the
// returned net.Conn with the configured certificate providers.
func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	// TODO: Close connections if in "non-serving" state.
	return &conn{Conn: c, parent: l}, nil
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

// listenerUpdate wraps the information received from a registered LDS watcher.
type listenerUpdate struct {
	lds        xdsclient.ListenerUpdate // received update
	err        error                    // received error
	goodUpdate *grpcsync.Event          // event to fire upon a good update
}

func (l *listenerWrapper) handleListenerUpdate(update listenerUpdate) {
	if l.closed.HasFired() {
		l.logger.Warningf("Resource %q received update: %v with error: %v, after listener was closed", l.name, update.lds, update.err)
		return
	}

	// TODO: Handle resource-not-found errors by moving to not-serving state.
	if update.err != nil {
		// We simply log an error here and hope we get a successful update
		// in the future. The error could be because of a timeout or an
		// actual error, like the requested resource not found. In any case,
		// it is fine for the server to hang indefinitely until Stop() is
		// called.
		l.logger.Warningf("Received error for resource %q: %+v", l.name, update.err)
		return
	}
	l.logger.Infof("Received update for resource %q: %+v", l.name, update.lds)

	l.mu.Lock()
	l.filterChains = update.lds.FilterChains
	l.defaultFilterChain = update.lds.DefaultFilterChain
	l.mu.Unlock()
	update.goodUpdate.Fire()
	// TODO: Move to serving state on receipt of a good response.
}
