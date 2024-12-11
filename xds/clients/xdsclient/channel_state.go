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

package xdsclient

import (
	"sync/atomic"

	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/clients"
)

// channelState represents the state of an xDS channel.
// It receives callbacks for events on the underlying ADS stream and invokes
// corresponding callbacks on interested authoririties.
type channelState struct {
	parent       *XDSClient
	serverConfig *clients.ServerConfig
	logger       *igrpclog.PrefixLogger // Logger for this authority.

	// Access to the following fields should be protected by the parent's
	// channelsMu.
	channel               *xdsChannel
	interestedAuthorities map[string]*authorityState
}

func (cs *channelState) adsStreamFailure(err error) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for _, as := range cs.interestedAuthorities {
		as.adsStreamFailure(cs.serverConfig, err)
	}
}

func (cs *channelState) adsResourceUpdate(typ ResourceType, updates map[string]dataAndErrTuple, md updateMetadata, onDone func()) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()

	if len(cs.interestedAuthorities) == 0 {
		onDone()
		return
	}

	authorityCnt := new(atomic.Int64)
	authorityCnt.Add(int64(len(cs.interestedAuthorities)))
	done := func() {
		if authorityCnt.Add(-1) == 0 {
			onDone()
		}
	}
	for _, as := range cs.interestedAuthorities {
		as.adsResourceUpdate(cs.serverConfig, typ, updates, md, done)
	}
}

func (cs *channelState) adsResourceDoesNotExist(typ ResourceType, resourceName string) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for _, as := range cs.interestedAuthorities {
		as.adsResourceDoesNotExist(typ, resourceName)
	}
}
