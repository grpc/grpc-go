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

// Package ringhash implements resolver related functions for the ring_hash
// load balancing policy.
package ringhash

import (
	"google.golang.org/grpc/resolver"
)

// hashKeyKey is the key to store the ring hash key attribute in
// resolver.Address attribute.
const hashKeyKey = hashKeyType("hash_key")

type hashKeyType string

// SetAddrHashKey sets the hash key for this address. Combined with the
// ring_hash load balancing policy, it allows placing the address on the ring
// based on an arbitrary string instead of the IP address.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func SetAddrHashKey(addr resolver.Address, hashKey string) resolver.Address {
	if hashKey == "" {
		return addr
	}
	addr.BalancerAttributes = addr.BalancerAttributes.WithValue(hashKeyKey, hashKey)
	return addr
}

// GetAddrHashKey returns the ring hash key attribute of addr. If this attribute
// is not set, it returns an empty string.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func GetAddrHashKey(addr resolver.Address) string {
	hashKey, _ := addr.BalancerAttributes.Value(hashKeyKey).(string)
	return hashKey
}
