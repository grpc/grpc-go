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

package clusterresolver

import (
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

type edsResourceWatcher interface {
	WatchEndpoints(string, func(xdsresource.EndpointsUpdate, error)) func()
}

type edsDiscoveryMechanism struct {
	cancel func()

	update         xdsresource.EndpointsUpdate
	updateReceived bool
}

func (er *edsDiscoveryMechanism) lastUpdate() (interface{}, bool) {
	if !er.updateReceived {
		return nil, false
	}
	return er.update, true
}

func (er *edsDiscoveryMechanism) resolveNow() {
}

func (er *edsDiscoveryMechanism) stop() {
	er.cancel()
}

// newEDSResolver returns an implementation of the endpointsResolver interface
// that uses EDS to resolve the given name to endpoints.
func newEDSResolver(nameToWatch string, watcher edsResourceWatcher, topLevelResolver topLevelResolver) *edsDiscoveryMechanism {
	ret := &edsDiscoveryMechanism{}
	ret.cancel = watcher.WatchEndpoints(nameToWatch, func(update xdsresource.EndpointsUpdate, err error) {
		if err != nil {
			topLevelResolver.onError(err)
			return
		}
		ret.update = update
		ret.updateReceived = true
		topLevelResolver.onUpdate()
	})
	return ret
}
