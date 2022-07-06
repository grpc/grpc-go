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
	"unsafe"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/resolver"
)

// subConnWrapper wraps every created SubConn in the Outlier Detection Balancer.
// It is used to store whether the SubConn has been ejected or not, and also to
// store the latest state for use when the SubConn gets unejected. It also
// stores the addresses the SubConn was created with to support any change in
// address(es).
type subConnWrapper struct {
	balancer.SubConn

	// addressInfo is a pointer to the subConnWrapper's corresponding address
	// map entry, if the map entry exists.
	addressInfo unsafe.Pointer // *addressInfo
	// These two pieces of state will reach eventual consistency due to sync in
	// run(), and child will always have the correctly updated SubConnState.
	latestState balancer.SubConnState
	ejected     bool

	scUpdateCh *buffer.Unbounded

	addresses []resolver.Address
}

// eject causes the wrapper to report a state update with the TRANSIENT_FAILURE
// state, and to stop passing along updates from the underlying subchannel.
func (scw *subConnWrapper) eject() {
	scw.scUpdateCh.Put(&ejectedUpdate{
		scw:     scw,
		ejected: true,
	})
}

// uneject causes the wrapper to report a state update with the latest update
// from the underlying subchannel, and resume passing along updates from the
// underlying subchannel.
func (scw *subConnWrapper) uneject() {
	scw.scUpdateCh.Put(&ejectedUpdate{
		scw:     scw,
		ejected: false,
	})
}
