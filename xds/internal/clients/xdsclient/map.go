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

package xdsclient

import (
	clientsinternal "google.golang.org/grpc/xds/internal/clients/internal"
)

type serverConfigMapEntry struct {
	serverConfig ServerConfig
	value        any
}

// serverConfigMap is a ServerIdentifierMap to arbitrary values taking into
// account ServerConfig.
type serverConfigMap struct {
	// The ServerIdentifierMap is keyed by clients.ServerIdentifier. The fields
	// that we care about are `clients.ServerIdentifier` and
	// `IgnoreResourceDeletion`. Since we need to be able to
	// distinguish between server identifiers with same
	// `clients.ServerIdentifier`, but different `IgnoreResourceDeletion`, we
	// cannot store the `IgnoreResourceDeletion` in the map key.
	//
	// The value type of the map contains a slice of server configs which
	// match the key in their `clients.ServerIdentifier` field and contain the
	// corresponding value associated with them.
	clientsinternal.ServerIdentifierMap
}

type serverConfigMapEntryList []*serverConfigMapEntry

// newServerConfigMap creates a new ServerConfigMap.
func newServerConfigMap() *serverConfigMap {
	return &serverConfigMap{ServerIdentifierMap: *clientsinternal.NewServerIdentifierMap()}
}

// find returns the index of ServerConfig in the serverConfigMapEntry
// slice, or -1 if not present.
func (l serverConfigMapEntryList) find(sc ServerConfig) int {
	for i, entry := range l {
		// Extensions are the only thing to match on here, since `ServerURI`
		// are already equal.
		if entry.serverConfig.IgnoreResourceDeletion == sc.IgnoreResourceDeletion {
			return i
		}
	}
	return -1
}

// get returns the value for the ServerConfig in the map, if
// present.
func (s *serverConfigMap) get(sc ServerConfig) (value any, ok bool) {
	entryList, ok := s.ServerIdentifierMap.Get(sc.ServerIdentifier)
	if !ok {
		return nil, false
	}
	entries := entryList.(serverConfigMapEntryList)
	if entry := entries.find(sc); entry != -1 {
		return entries[entry].value, true
	}
	return nil, false
}

// set updates or adds the value to the server config in the map.
func (s *serverConfigMap) set(sc ServerConfig, value any) {
	entryList, ok := s.ServerIdentifierMap.Get(sc.ServerIdentifier)
	if ok {
		entries := entryList.(serverConfigMapEntryList)
		if entry := entries.find(sc); entry != -1 {
			entries[entry].value = value
			return
		}
	}
	entries := serverConfigMapEntryList{&serverConfigMapEntry{serverConfig: sc, value: value}}
	s.ServerIdentifierMap.Set(sc.ServerIdentifier, entries)
}

// delete removes ServerConfig from the map.
func (s *serverConfigMap) delete(sc ServerConfig) {
	entryList, ok := s.ServerIdentifierMap.Get(sc.ServerIdentifier)
	if !ok {
		return
	}
	entries := entryList.(serverConfigMapEntryList)
	entry := entries.find(sc)
	if entry == -1 {
		return
	}
	if len(entries) == 1 {
		entries = nil
	} else {
		copy(entries[entry:], entries[entry+1:])
		entries = entries[:len(entries)-1]
	}
	s.ServerIdentifierMap.Set(sc.ServerIdentifier, entries)
}

// values returns a slice of all current map values.
func (s *serverConfigMap) values() []any {
	ret := make([]any, 0, s.ServerIdentifierMap.Len())
	for _, si := range s.ServerIdentifierMap.Keys() {
		entryList, _ := s.ServerIdentifierMap.Get(si)
		entries := entryList.(serverConfigMapEntryList)
		for _, entry := range entries {
			ret = append(ret, entry.value)
		}
	}
	return ret
}
