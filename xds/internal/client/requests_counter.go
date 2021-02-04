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
	"sync/atomic"
)

const defaultMaxRequests uint32 = 1024

type servicesRequestsCounter struct {
	mu       sync.Mutex
	services map[string]*ServiceRequestsCounter
}

var src = &servicesRequestsCounter{
	services: make(map[string]*ServiceRequestsCounter),
}

// ServiceRequestsCounter is used to track the total inflight requests for a
// service with the provided name.
type ServiceRequestsCounter struct {
	ServiceName string
	maxRequests uint32
	numRequests uint32
}

// GetServiceRequestsCounter returns the ServiceRequestsCounter with the
// provided serviceName. If one does not exist, it creates it.
func GetServiceRequestsCounter(serviceName string) *ServiceRequestsCounter {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.services[serviceName]
	if !ok {
		c = &ServiceRequestsCounter{ServiceName: serviceName, maxRequests: defaultMaxRequests}
		src.services[serviceName] = c
	}
	return c
}

// SetMaxRequests updates the max requests for a service's counter.
func SetMaxRequests(serviceName string, maxRequests *uint32) {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.services[serviceName]
	if !ok {
		c = &ServiceRequestsCounter{ServiceName: serviceName}
		src.services[serviceName] = c
	}
	if maxRequests != nil {
		c.maxRequests = *maxRequests
	} else {
		c.maxRequests = defaultMaxRequests
	}
}

// StartRequest starts a request for a service, incrementing its number of
// requests by 1. Returns an error if the max number of requests is exceeded.
func (c *ServiceRequestsCounter) StartRequest() error {
	if atomic.LoadUint32(&c.numRequests) >= atomic.LoadUint32(&c.maxRequests) {
		return fmt.Errorf("max requests %v exceeded on service %v", c.maxRequests, c.ServiceName)
	}
	atomic.AddUint32(&c.numRequests, 1)
	return nil
}

// EndRequest ends a request for a service, decrementing its number of requests
// by 1.
func (c *ServiceRequestsCounter) EndRequest() {
	atomic.AddUint32(&c.numRequests, ^uint32(0))
}

// ClearCounterForTesting clears the counter for the service. Should be only
// used in tests.
func ClearCounterForTesting(serviceName string) {
	src.mu.Lock()
	defer src.mu.Unlock()
	c, ok := src.services[serviceName]
	if !ok {
		return
	}
	c.maxRequests = defaultMaxRequests
	c.numRequests = 0
}
