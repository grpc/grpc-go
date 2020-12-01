/*
 *
 * Copyright 2020 gRPC authors.
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

func init() {
	src.services = make(map[string]*ServiceRequestsCounter)
}

type servicesRequestsCounter struct {
	mu       sync.Mutex
	services map[string]*ServiceRequestsCounter
}

var src servicesRequestsCounter

// ServiceRequestsCounter is used to track the total inflight requests for a
// service with the provided name.
type ServiceRequestsCounter struct {
	mu          sync.Mutex
	ServiceName string
	maxRequests uint32
	numRequests uint32
}

// NewServiceRequestsCounter creates a new ServiceRequestsCounter that is
// internally tracked by this package and returns a pointer to it. If one with
// the serviceName already exists, returns a pointer to it.
func NewServiceRequestsCounter(serviceName string) *ServiceRequestsCounter {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.services[serviceName]
	if !ok {
		c = &ServiceRequestsCounter{ServiceName: serviceName}
		src.services[serviceName] = c
	}
	return c
}

// SetMaxRequests updates the max requests for a service's counter.
func (c *ServiceRequestsCounter) SetMaxRequests(maxRequests uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxRequests = maxRequests
}

// StartRequest starts a request for a service, incrementing its number of
// requests by 1. Returns an error if the max number of requests is exceeded.
func (c *ServiceRequestsCounter) StartRequest() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.numRequests+1 > c.maxRequests {
		return fmt.Errorf("max requests %v exceeded on service %v", c.maxRequests, c.ServiceName)
	}
	c.numRequests++
	return nil
}

// EndRequest ends a request for a service, decrementing its number of requests
// by 1.
func (c *ServiceRequestsCounter) EndRequest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numRequests--
}
