// +build go1.10

/*
 *
 * Copyright 2020 gRPC authors.
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

// Package rls implements the RLS LB policy.
package rls

import (
	"time"
)

const (
	rlsBalancerName = "rls"
	// This is max duration that we are willing to cache RLS responses. If the
	// service config doesn't specify a value for max_age or if it specified a
	// value greater that this, we will use this value instead.
	maxMaxAge = 5 * time.Minute
	// If lookup_service_timeout is not specified in the service config, we use
	// a default of 10 seconds.
	defaultLookupServiceTimeout = 10 * time.Second
	// This is set to the targetNameField in the child policy config during
	// service config validation.
	dummyChildPolicyTarget = "target_name_to_be_filled_in_later"
)

// rlsBB helps build RLS load balancers and parse the service config to be
// passed to the RLS load balancer.
type rlsBB struct {
	// TODO(easwars): Implement the Build() method and register the builder.
}

// Name returns the name of the RLS LB policy and helps implement the
// balancer.Balancer interface.
func (*rlsBB) Name() string {
	return rlsBalancerName
}
