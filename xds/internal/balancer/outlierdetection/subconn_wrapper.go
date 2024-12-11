/*
 *
 * Copyright 2022 gRPC authors.
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

package outlierdetection

import (
	"fmt"
	"sync"
	"unsafe"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/resolver"
)

// subConnWrapper wraps every created SubConn in the Outlier Detection Balancer,
// to help track the latest state update from the underlying SubConn, and also
// whether or not this SubConn is ejected.
type subConnWrapper struct {
	balancer.SubConn
	// The following fields are set during object creation and read-only after
	// that.

	listener func(balancer.SubConnState)

	// addressInfo is a pointer to the subConnWrapper's corresponding address
	// map entry, if the map entry exists.
	addressInfo unsafe.Pointer // *addressInfo
	// These two pieces of state will reach eventual consistency due to sync in
	// run(), and child will always have the correctly updated SubConnState.
	// latestDeleveredState is the latest state update from the underlying SubConn. This
	// is used whenever a SubConn gets unejected. This will be the health state
	// if a health listener is being used, otherwise it will be the connectivity
	// state.
	latestDeleveredState balancer.SubConnState
	ejected              bool

	scUpdateCh *buffer.Unbounded

	// addresses is the list of address(es) this SubConn was created with to
	// help support any change in address(es)
	addresses []resolver.Address
	// healthListenerEnabled indicates whether the leaf LB policy is using a
	// generic health listener. When enabled, ejection updates are sent via the
	// health listener instead of the connectivity listener. Once Dualstack
	// changes are complete, all SubConns will be created by pickfirst which
	// uses the health listener.
	healthListenerEnabled bool

	// Access to the following fields are protected by a mutex.
	mu             sync.Mutex
	healthListener func(balancer.SubConnState)
	// latestReceivedConnectivityState is the SubConn most recent connectivity
	// state received from the subchannel. It may not be delivered to the child
	// balancer yet. It is used to ensure a health listener is registered only
	// when the subchannel is READY.
	latestReceivedConnectivityState connectivity.State
}

// eject causes the wrapper to report a state update with the TRANSIENT_FAILURE
// state, and to stop passing along updates from the underlying subchannel.
func (scw *subConnWrapper) eject() {
	scw.scUpdateCh.Put(&ejectionUpdate{
		scw:       scw,
		isEjected: true,
	})
}

// uneject causes the wrapper to report a state update with the latest update
// from the underlying subchannel, and resume passing along updates from the
// underlying subchannel.
func (scw *subConnWrapper) uneject() {
	scw.scUpdateCh.Put(&ejectionUpdate{
		scw:       scw,
		isEjected: false,
	})
}

func (scw *subConnWrapper) String() string {
	return fmt.Sprintf("%+v", scw.addresses)
}

func (scw *subConnWrapper) RegisterHealthListener(listener func(balancer.SubConnState)) {
	scw.mu.Lock()
	defer scw.mu.Unlock()

	if !scw.healthListenerEnabled {
		logger.Errorf("Health listener unexpectedly registered on SubConn %v.", scw)
		return
	}

	if scw.latestReceivedConnectivityState != connectivity.Ready {
		return
	}
	scw.healthListener = listener
	if listener == nil {
		scw.SubConn.RegisterHealthListener(nil)
	} else {
		scw.SubConn.RegisterHealthListener(func(scs balancer.SubConnState) {
			scw.scUpdateCh.Put(&scHealthUpdate{
				scw:   scw,
				state: scs,
			})
		})
	}
}
