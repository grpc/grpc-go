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
 *
 */

package wrrlocality

import "testing"

// perhaps move parseconfig here

// what other high level functionality are we testing?

// e2e tests with RPC's?


// an atomic unit (xds_wrr_locality_experimental + weighted_target).

// also test the preparation of weighted target
// from the map

// the existing e2e tests testing the behavior should stay the same
// weighted target config preparation will have to change

// unit test of the combining of the map with the config

// does the e2e test of this balancer as top level balancer of channel even map?

// what are we even verifying unit test wise do I even need a test Client Conn?

func (s) TestChildBasicOperations(t *testing.T) {

}


// child - it's just pass through so just test this?

// weighted target implicitly comes coupled with a child endpoint picking policy
// though


// if weighted target is hardcoded (am I sure I want this?)

// then I need to test the UpdateClientConnState
// on the eventual children's endpoint picking policy that it received right config
// and the direct child received the right config

// how do we verify this balancer works correctly - pull logic about weighted target preperation
// in cluster resolver.

// I actually want to test endpoint picking policy as well