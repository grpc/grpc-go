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

package internal

import "google.golang.org/grpc/xds/internal/clients"

type serverIdentifierMapEntry struct {
	serverIdentifier clients.ServerIdentifier
	value            any
}

// ServerIdentifierMap is a map of server URIs to arbitrary values taking into
// account clients.ServerIdentifier Extensions. Multiple accesses may not be
// performed concurrently.  Must be created via NewServersMap; do not construct
// directly.
type ServerIdentifierMap struct {
	// The underlying map is keyed by ServerURI string. The  fields that we
	// care about are `ServerURI` and `Extensions`. Since we need to be able to
	// distinguish between server identifiers with same `ServerURI`, but different
	// `Extensions`, we cannot store the `Extensions` in the map key.
	//
	// The value type of the map contains a slice of server identifiers which
	// match the key in their `ServerURI` field and contain the corresponding
	// value associated with them.
	m map[string]serverIdentifierMapEntryList
}

type serverIdentifierMapEntryList []*serverIdentifierMapEntry

// NewServerIdentifierMap creates a new ServerIdentifierMap.
func NewServerIdentifierMap() *ServerIdentifierMap {
	return &ServerIdentifierMap{m: make(map[string]serverIdentifierMapEntryList)}
}

// find returns the index of clients.ServerIdentifier in the
// serverIdentifierMapEntry slice, or -1 if not present.
func (l serverIdentifierMapEntryList) find(si clients.ServerIdentifier) int {
	for i, entry := range l {
		// Extensions are the only thing to match on here, since `ServerURI`
		// are already equal.
		if ServerIdentifierEqual(entry.serverIdentifier, si) {
			return i
		}
	}
	return -1
}

// Get returns the value for the clients.ServerIdentifier in the map, if
// present.
func (s *ServerIdentifierMap) Get(si clients.ServerIdentifier) (value any, ok bool) {
	entryList := s.m[si.ServerURI]
	if entry := entryList.find(si); entry != -1 {
		return entryList[entry].value, true
	}
	return nil, false
}

// Set updates or adds the value to the server identifier in the map.
func (s *ServerIdentifierMap) Set(si clients.ServerIdentifier, value any) {
	if _, ok := si.Extensions.(interface{ Equal(any) bool }); si.Extensions != nil && !ok {
		return
	}
	entryList := s.m[si.ServerURI]
	if entry := entryList.find(si); entry != -1 {
		entryList[entry].value = value
		return
	}
	s.m[si.ServerURI] = append(entryList, &serverIdentifierMapEntry{serverIdentifier: si, value: value})
}

// Delete removes clients.ServerIdentifier from the map.
func (s *ServerIdentifierMap) Delete(si clients.ServerIdentifier) {
	entryList := s.m[si.ServerURI]
	entry := entryList.find(si)
	if entry == -1 {
		return
	}
	if len(entryList) == 1 {
		entryList = nil
	} else {
		copy(entryList[entry:], entryList[entry+1:])
		entryList = entryList[:len(entryList)-1]
	}
	s.m[si.ServerURI] = entryList
}

// Len returns the number of entries in the map.
func (s *ServerIdentifierMap) Len() int {
	ret := 0
	for _, entryList := range s.m {
		ret += len(entryList)
	}
	return ret
}

// Keys returns a slice of all current map keys.
func (s *ServerIdentifierMap) Keys() []clients.ServerIdentifier {
	ret := make([]clients.ServerIdentifier, 0, s.Len())
	for _, entryList := range s.m {
		for _, entry := range entryList {
			ret = append(ret, entry.serverIdentifier)
		}
	}
	return ret
}

// Values returns a slice of all current map values.
func (s *ServerIdentifierMap) Values() []any {
	ret := make([]any, 0, s.Len())
	for _, entryList := range s.m {
		for _, entry := range entryList {
			ret = append(ret, entry.value)
		}
	}
	return ret
}
