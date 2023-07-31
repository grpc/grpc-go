/*
 *
 * Copyright 2019 gRPC authors.
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

package deterministicsubsetting

import (
	"encoding/json"

	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

// LBConfig is the config for the outlier detection balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	ClientIndex   *uint64 `json:"client_index,omitempty"`
	SubsetSize    uint64  `json:"subset_size,omitempty"`
	SortAddresses bool    `json:"sort_addresses,omitempty"`

	ChildPolicy *iserviceconfig.BalancerConfig `json:"child_policy,omitempty"`
}

// For UnmarshalJSON to work correctly and set defaults without infinite
// recursion.
type lbConfig LBConfig

func (lbc *LBConfig) UnmarshalJSON(j []byte) error {
	// Default top layer values.
	lbc.SubsetSize = 10
	// Unmarshal JSON on a type with zero values for methods, including
	// UnmarshalJSON. Overwrites defaults, leaves alone if not. typecast to
	// avoid infinite recursion by not recalling this function and causing stack
	// overflow.
	return json.Unmarshal(j, (*lbConfig)(lbc))
}
