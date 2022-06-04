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

type subConnWrapper struct {
	balancer.SubConn

	// "The subchannel wrappers created by the outlier_detection LB policy will
	// hold a reference to its map entry in the LB policy, if that map entry
	// exists." - A50
	obj unsafe.Pointer // *object
	// These two pieces of state will reach eventual consistency due to sync in
	// run(), and child will always have the correctly updated SubConnState.
	latestState balancer.SubConnState
	ejected     bool

	scUpdateCh *buffer.Unbounded

	addresses []resolver.Address
}

// eject(): "The wrapper will report a state update with the TRANSIENT_FAILURE
// state, and will stop passing along updates from the underlying subchannel."
func (scw *subConnWrapper) eject() {
	scw.scUpdateCh.Put(&ejectedUpdate{
		scw:     scw,
		ejected: true,
	})
}

// uneject(): "The wrapper will report a state update with the latest update
// from the underlying subchannel, and resume passing along updates from the
// underlying subchannel."
func (scw *subConnWrapper) uneject() {
	scw.scUpdateCh.Put(&ejectedUpdate{
		scw:     scw,
		ejected: false,
	})
}
