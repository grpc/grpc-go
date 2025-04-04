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

package pickfirstleaf

import (
	"google.golang.org/grpc/resolver"
)

type keyType string

const key = keyType("grpc.balancer.pickfirstleaf.address.metadata")

// subConnMap is similar to a resolver.AddressMapV2[*scData] but it doesn't
// ignore the Address.Metadata field in the map keys.
type subConnMap struct {
	subConns *resolver.AddressMapV2[data]
}

type data struct {
	subConnData *scData
	// originalAddr stores the address without the metadata attribute.
	originalAddr resolver.Address
}

func newSubConnMap() *subConnMap {
	return &subConnMap{
		subConns: resolver.NewAddressMapV2[data](),
	}
}

func (s *subConnMap) get(addr resolver.Address) (value *scData, ok bool) {
	d, ok := s.subConns.Get(addrWithMetadaAttribute(addr))
	if !ok {
		return nil, false
	}
	return d.subConnData, true
}

func (s *subConnMap) set(addr resolver.Address, value *scData) {
	data := data{
		subConnData:  value,
		originalAddr: addr,
	}
	s.subConns.Set(addrWithMetadaAttribute(addr), data)
}

func (s *subConnMap) deleteKey(addr resolver.Address) {
	s.subConns.Delete(addrWithMetadaAttribute(addr))
}

func (s *subConnMap) length() int {
	return s.subConns.Len()
}

func (s *subConnMap) values() []*scData {
	wrappedValues := s.subConns.Values()
	vals := make([]*scData, 0, len(wrappedValues))
	for _, wv := range wrappedValues {
		vals = append(vals, wv.subConnData)
	}
	return vals
}

func (s *subConnMap) keys() []resolver.Address {
	wrappedValues := s.subConns.Values()
	keys := make([]resolver.Address, 0, len(wrappedValues))
	for _, wv := range wrappedValues {
		keys = append(keys, wv.originalAddr)
	}
	return keys
}

// addrWithMetadaAttribute stores the address Metadata as an Attribute to make
// the underlying AddressMap consider Metadata while comparing Addresses.
func addrWithMetadaAttribute(addr resolver.Address) resolver.Address {
	addr.Attributes = addr.Attributes.WithValue(key, addr.Metadata)
	return addr
}
