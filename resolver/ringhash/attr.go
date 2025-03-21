/*
 *
 * Copyright 2025 gRPC authors.
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

type hashKeyType string

// hashKeyKey is the key to store the ring hash key attribute in
// a resolver.Endpoint attribute.
const hashKeyKey = hashKeyType("hash_key")

// SetEndpointHashKey sets the hash key for this endpoint. Combined with the
// ring_hash load balancing policy, it allows placing the endpoint on the ring
// based on an arbitrary string instead of the IP address.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func SetEndpointHashKey(ep resolver.Endpoint, hashKey string) resolver.Endpoint {
	if hashKey == "" {
		return ep
	}
	ep.Attributes = ep.Attributes.WithValue(hashKeyKey, hashKey)
	return ep
}

// GetEndpointHashKey returns the hash key attribute of addr. If this attribute
// is not set, it returns the empty string.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func GetEndpointHashKey(ep resolver.Endpoint) string {
	hashKey, _ := ep.Attributes.Value(hashKeyKey).(string)
	return hashKey
}
