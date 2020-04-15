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

// Package weightedroundrobin defines a weighted roundrobin balancer.
package weightedroundrobin

import "google.golang.org/grpc/attributes"

const (
	// Name is the name of weighted_round_robin balancer.
	Name = "weighted_round_robin"

	// Attribute key used to store AddrInfo in the Attributes field of
	// resolver.Address.
	attributeKey = "/balancer/weightedroundrobin/addrInfo"
)

// AddrInfo will be stored inside Address metadata in order to use weighted roundrobin
// balancer.
type AddrInfo struct {
	Weight uint32
}

// AddAddrInfoToAttributes returns a new Attributes containing all key/value
// pairs in a with ai being added to it.
func AddAddrInfoToAttributes(ai *AddrInfo, a *attributes.Attributes) *attributes.Attributes {
	return a.WithValues(attributeKey, ai)
}

// GetAddrInfoFromAttributes returns the AddrInfo stored in a. Returns nil if no
// AddrInfo is present in a.
func GetAddrInfoFromAttributes(a *attributes.Attributes) *AddrInfo {
	if a == nil {
		return nil
	}
	ai := a.Value(attributeKey)
	if ai == nil {
		return nil
	}
	if _, ok := ai.(*AddrInfo); !ok {
		return nil
	}
	return ai.(*AddrInfo)
}
