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
	value interface{}
}

// AddressMap is a map of addresses to arbitrary values taking into account
// Attributes.  BalancerAttributes are ignored, as are Metadata and Type.
// Multiple accesses may not be performed concurrently.  Must be created via
// NewAddressMap; do not construct directly.
type AddressMap struct {
	m map[string]addressMapEntryList
}

type addressMapEntryList []*addressMapEntry

// NewAddressMap creates a new AddressMap.
func NewAddressMap() *AddressMap {
	return &AddressMap{m: make(map[string]addressMapEntryList)}
}

// find returns the index of addr in the addressMapEntry slice, or -1 if not
// present.
func (l addressMapEntryList) find(addr Address) int {
	for i, entry := range l {
		if entry.addr.equalWithAttributes(addr) {
			return i
		}
	}
	return -1
}

// Get returns the value for the address in the map, if present.
func (a *AddressMap) Get(addr Address) (value interface{}, ok bool) {
	entryList := a.m[addr.Addr]
	if entry := entryList.find(addr); entry != -1 {
		return entryList[entry].value, true
	}
	return nil, false
}

// Set updates or adds the value to the address in the map.
func (a *AddressMap) Set(addr Address, value interface{}) {
	entryList := a.m[addr.Addr]
	if entry := entryList.find(addr); entry != -1 {
		entryList[entry].value = value
		return
	}
	a.m[addr.Addr] = append(entryList, &addressMapEntry{addr: addr, value: value})
}

// Delete removes addr from the map.
func (a *AddressMap) Delete(addr Address) {
	entryList := a.m[addr.Addr]
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
	a.m[addr.Addr] = entryList
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
func (a *AddressMap) Values() []interface{} {
	ret := make([]interface{}, 0, a.Len())
	for _, entryList := range a.m {
		for _, entry := range entryList {
			ret = append(ret, entry.value)
		}
	}
	return ret
}
