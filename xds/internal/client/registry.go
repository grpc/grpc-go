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

package client

import (
	"fmt"
	"sync"
)

// We maintain a global registry of xDS client objects to be shared between the
// resolver and the balancer. The former is the one which creates the client
// object, and as part of the creation process, it is added to the registry.
// The registry key of the client is passed to the balancer which looks up the
// client object using the GetFromRegistry exported method.
var registry *clientRegistry

func init() {
	registry = newClientRegistry()
}

// GetFromRegistry returns the xds client object corresponding to the provided
// key. It returns an error if no client could be found for the provided key.
func GetFromRegistry(key string) (*Client, error) {
	return registry.get(key)
}

// clientRegistry is an over simplified registry of xds client objects,
// implemented as a map, keyed by an monotonically increasing integer,
// represented as a string.
// The registry can be safely accessed by multiple concurrent goroutines.
type clientRegistry struct {
	mu      sync.Mutex
	nextKey uint64
	clients map[string]*Client
}

// The only reason for making this a function is so that it could be used from
// the test to create a registry object in the same way that it is done from
// init().
func newClientRegistry() *clientRegistry {
	return &clientRegistry{clients: make(map[string]*Client)}
}

func (cr *clientRegistry) add(client *Client) string {
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

func (cr *clientRegistry) get(key string) (*Client, error) {
	cr.mu.Lock()
	c, ok := cr.clients[key]
	if !ok {
		cr.mu.Unlock()
		return nil, fmt.Errorf("xds: xdsClient with key %s not found", key)
	}
	cr.mu.Unlock()
	return c, nil
}
