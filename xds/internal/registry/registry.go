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

// Package registry provides a global registry of xDS client objects to be
// shared between the xds resolver and balancer implementations. The former
// creates the client object (during which it is added to the registry) and
// passes the registry key the latter.
package registry

import (
	"fmt"
	"sync"
)

// KeyName is the attributes key used to pass the xdsClient's registry ID (in
// the resolver state passed from the xdsResolver to the xdsBalancer)
const KeyName = "registryKey"

var registry *clientRegistry

func init() {
	registry = &clientRegistry{clients: make(map[string]interface{})}
}

// Get returns the xds client object corresponding key. It returns an error if
// no client could be found for key.
func Get(key string) (interface{}, error) {
	return registry.get(key)
}

// AddTo adds client to the registry and returns it's registry ID.
func AddTo(client interface{}) string {
	return registry.add(client)
}

// RemoveFrom removes the client corresponding to key from the registry.
func RemoveFrom(key string) {
	registry.remove(key)
}

// clientRegistry is an over simplified registry of xds client objects,
// implemented as a map, keyed by an monotonically increasing integer,
// represented as a string.
// The registry can be safely accessed by multiple concurrent goroutines.
type clientRegistry struct {
	mu      sync.Mutex
	nextKey uint64
	// We store the xdsClient object as an interface{} in the map because we
	// mock out the client in tests (using different interfaces, based on where
	// they are used from).
	clients map[string]interface{}
}

func (cr *clientRegistry) add(client interface{}) string {
	cr.mu.Lock()
	key := fmt.Sprintf("%d", cr.nextKey)
	cr.clients[key] = client
	cr.nextKey++
	cr.mu.Unlock()
	return key
}

func (cr *clientRegistry) remove(key string) {
	cr.mu.Lock()
	delete(cr.clients, key)
	cr.mu.Unlock()
}

func (cr *clientRegistry) get(key string) (interface{}, error) {
	cr.mu.Lock()
	c, ok := cr.clients[key]
	if !ok {
		cr.mu.Unlock()
		return nil, fmt.Errorf("xds: xdsclient with key %s not found", key)
	}
	cr.mu.Unlock()
	return c, nil
}
