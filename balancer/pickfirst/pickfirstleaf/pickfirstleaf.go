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

// Package pickfirstleaf contains the pick_first load balancing policy which
// will be the universal leaf policy after dualstack changes are implemented.
//
// Deprecated: This package is deprecated. Please use the balancer/pickfirst
// package instead.
package pickfirstleaf

import (
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/resolver"
)

// Name is the name of the pick_first_leaf balancer.
// Deprecated: Use the balancer/pickfirst package's Name instead.
const Name = "pick_first"

// EnableHealthListener updates the state to configure pickfirst for using a
// generic health listener.
// Deprecated: Use the balancer/pickfirst package's EnableHealthListener
// instead.
func EnableHealthListener(state resolver.State) resolver.State {
	return pickfirst.EnableHealthListener(state)
}
