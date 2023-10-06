/*
 *
 * Copyright 2021 gRPC authors.
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

package resolver

type addressMapEntry struct {
	addr  Address
	value any
}

// AddressMap is a map of addresses to arbitrary values taking into account
// Attributes.  BalancerAttributes are ignored, as are Metadata and Type.
// Multiple accesses may not be performed concurrently.  Must be created via
// NewAddressMap; do not construct directly.
type AddressMap struct {
	// The underlying map is keyed by an Address with fields that we don't care
	// about being set to their zero values. The only fields that we care about
	// are `Addr`, `ServerName` and `Attributes`. Since we need to be able to
	// distinguish between addresses with same `Addr` and `ServerName`, but
	// different `Attributes`, we cannot store the `Attributes` in the map key.
	//
	// The comparison operation for structs work as follows:
	//  Struct values are comparable if all their fields are comparable. Two
	//  struct values are equal if their corresponding non-blank fields are equal.
	//
	// The value type of the map contains a slice of addresses which match the key
	// in their `Addr` and `ServerName` fields and contain the corresponding value
	// associated with them.
	m map[Address]addressMapEntryList
}

func toMapKey(addr *Address) Address {
	return Address{Addr: addr.Addr, ServerName: addr.ServerName}
}

type addressMapEntryList []*addressMapEntry

// NewAddressMap creates a new AddressMap.
func NewAddressMap() *AddressMap {
	return &AddressMap{m: make(map[Address]addressMapEntryList)}
}

// find returns the index of addr in the addressMapEntry slice, or -1 if not
// present.
func (l addressMapEntryList) find(addr Address) int {
	for i, entry := range l {
		// Attributes are the only thing to match on here, since `Addr` and
		// `ServerName` are already equal.
		if entry.addr.Attributes.Equal(addr.Attributes) {
			return i
		}
	}
	return -1
}

// Get returns the value for the address in the map, if present.
func (a *AddressMap) Get(addr Address) (value any, ok bool) {
	addrKey := toMapKey(&addr)
	entryList := a.m[addrKey]
	if entry := entryList.find(addr); entry != -1 {
		return entryList[entry].value, true
	}
	return nil, false
}

// Set updates or adds the value to the address in the map.
func (a *AddressMap) Set(addr Address, value any) {
	addrKey := toMapKey(&addr)
	entryList := a.m[addrKey]
	if entry := entryList.find(addr); entry != -1 {
		entryList[entry].value = value
		return
	}
	a.m[addrKey] = append(entryList, &addressMapEntry{addr: addr, value: value})
}

// Delete removes addr from the map.
func (a *AddressMap) Delete(addr Address) {
	addrKey := toMapKey(&addr)
	entryList := a.m[addrKey]
	entry := entryList.find(addr)
	if entry == -1 {
		return
	}
	if len(entryList) == 1 {
		entryList = nil
	} else {
		copy(entryList[entry:], entryList[entry+1:])
		entryList = entryList[:len(entryList)-1]
	}
	a.m[addrKey] = entryList
}

// Len returns the number of entries in the map.
func (a *AddressMap) Len() int {
	ret := 0
	for _, entryList := range a.m {
		ret += len(entryList)
	}
	return ret
}

// Keys returns a slice of all current map keys.
func (a *AddressMap) Keys() []Address {
	ret := make([]Address, 0, a.Len())
	for _, entryList := range a.m {
		for _, entry := range entryList {
			ret = append(ret, entry.addr)
		}
	}
	return ret
}

// Values returns a slice of all current map values.
func (a *AddressMap) Values() []any {
	ret := make([]any, 0, a.Len())
	for _, entryList := range a.m {
		for _, entry := range entryList {
			ret = append(ret, entry.value)
		}
	}
	return ret
}

type endpointNode struct {
	addrs map[string]int
	val   any
}

// Equal returns whether the unordered set of addrs counts are the same between
// the endpoint nodes.
func (en endpointNode) Equal(en2 endpointNode) bool {
	if len(en.addrs) != len(en2.addrs) {
		return false
	}
	for addr, count := range en.addrs {
		if count2, ok := en2.addrs[addr]; !ok || count != count2 {
			return false
		}
	}
	return true
}

func toEndpointNode(endpoint Endpoint) endpointNode {
	en := make(map[string]int)
	for _, addr := range endpoint.Addresses {
		en[addr.Addr]++
	}
	return endpointNode{
		addrs: en,
	}
}

// EndpointMap is a map of endpoints to arbitrary values keyed on only the
// unordered set of address strings within an endpoint. This map is not thread
// safe, cannot access multiple times concurrently. The zero value for an
// EndpointMap is an empty EndpointMap ready to use.
type EndpointMap struct {
	// Use a slice instead of a map is because slices or maps cannot be used as
	// map keys. Thus, for efficiency store as a map and compare to others so
	// each node comparison is o(n), where n is the number of unique addresses
	// in the endpoint rather than having to sort a list on each comparison
	// which is n(log(n)) (cannot use slices for the latter case or maps for the
	// former case as map keys, so needs to be a list).
	endpoints []endpointNode // Cannot use maps as a map key. Thus, keep a list, and o(n) search. Not on RPC fast path so efficiency not as important.
}

// Get returns the value for the address in the map, if present.
func (em *EndpointMap) Get(e Endpoint) (value any, ok bool) {
	en := toEndpointNode(e)
	if i := em.find(en); i != -1 {
		return em.endpoints[i].val, true
	}
	return nil, false
}

// Set updates or adds the value to the address in the map.
func (em *EndpointMap) Set(e Endpoint, value any) {
	en := toEndpointNode(e)
	if i := em.find(en); i != -1 {
		em.endpoints[i].val = value
		return
	}
	en.val = value
	em.endpoints = append(em.endpoints, en)
}

// Len returns the number of entries in the map.
func (em *EndpointMap) Len() int {
	return len(em.endpoints)
}

// Keys returns a slice of all current map keys, as endpoints specifying the
// addresses present in the endpoint keys, in which uniqueness is determined by
// the unordered set of addresses. Thus, endpoint information returned is not
// the full endpoint data but can be used for EndpointMap accesses.
func (em *EndpointMap) Keys() []Endpoint { // TODO: Persist the whole endpoint data to return in nodes? No use case now, but one could come up in future.
	ret := make([]Endpoint, 0, len(em.endpoints))
	for _, en := range em.endpoints {
		var endpoint Endpoint
		for addr, count := range en.addrs {
			for i := 0; i < count; i++ {
				endpoint.Addresses = append(endpoint.Addresses, Address{
					Addr: addr,
				})
			}
		}
		ret = append(ret, endpoint)
	}
	return ret
}

// Values returns a slice of all current map values.
func (em *EndpointMap) Values() []any {
	ret := make([]any, 0, len(em.endpoints))
	for _, en := range em.endpoints {
		ret = append(ret, en.val)
	}
	return ret
}

// find returns the index of endpoint in the EndpointMap endpoints slice, or -1
// if not present. The comparisons are done on the unordered set of the
// addresses within an endpoint.
func (em EndpointMap) find(e endpointNode) int {
	for i, endpoint := range em.endpoints {
		if endpoint.Equal(e) {
			return i
		}
	}
	return -1
}

// Delete removes the specified endpoint from the map.
func (em *EndpointMap) Delete(e Endpoint) {
	en := toEndpointNode(e)
	entry := em.find(en)
	if entry == -1 {
		return
	}
	if len(em.endpoints) == 1 {
		em.endpoints = nil
	}
	copy(em.endpoints[entry:], em.endpoints[entry+1:])
	em.endpoints = em.endpoints[:len(em.endpoints)-1]
}
