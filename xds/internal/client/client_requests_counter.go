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
	src.services = make(map[string]serviceInfo)
}

type servicesRequestsCounter struct {
	mu       sync.Mutex
	services map[string]serviceInfo
}

type serviceInfo struct {
	circuitBreaking bool
	maxRequests     uint32
	numRequests     uint32
}

var src servicesRequestsCounter

// ServiceRequestsCounter is used to track the total inflight requests for a
// service with the provided name.
type ServiceRequestsCounter struct {
	ServiceName string
}

// UpdateCounter updates the configuration for a service, or creates it if it
// doesn't exist. Pass nil to disable circuit breaking for a service.
func (c *ServiceRequestsCounter) UpdateCounter(maxRequests *uint32) {
	src.mu.Lock()
	defer src.mu.Unlock()
	sInfo, ok := src.services[c.ServiceName]
	if !ok {
		sInfo = serviceInfo{numRequests: 0}
	}
	sInfo.circuitBreaking = maxRequests != nil
	if maxRequests != nil {
		sInfo.maxRequests = *maxRequests
	}
	src.services[c.ServiceName] = sInfo
}

// StartRequest starts a request for a service, incrementing its number of
// requests by 1. Returns an error if circuit breaking is on and the max number
// of requests is exceeded.
func (c *ServiceRequestsCounter) StartRequest() error {
	src.mu.Lock()
	defer src.mu.Unlock()
	sInfo, ok := src.services[c.ServiceName]
	if !ok {
		return fmt.Errorf("service name %v not identified", c.ServiceName)
	}
	sInfo.numRequests++
	fmt.Println("StartRequest:", c.ServiceName, sInfo.circuitBreaking, sInfo.maxRequests, sInfo.numRequests)
	if sInfo.circuitBreaking && sInfo.numRequests > sInfo.maxRequests {
		return fmt.Errorf("max requests %v exceeded on service %v", sInfo.maxRequests, c.ServiceName)
	}
	src.services[c.ServiceName] = sInfo
	return nil
}

// EndRequest ends a request for a service, decrementing its number of requests
// by 1.
func (c *ServiceRequestsCounter) EndRequest() error {
	src.mu.Lock()
	defer src.mu.Unlock()
	sInfo, ok := src.services[c.ServiceName]
	if !ok {
		return fmt.Errorf("service name %v not identified", c.ServiceName)
	}
	sInfo.numRequests--
	fmt.Println("EndRequest:", c.ServiceName, sInfo.maxRequests, sInfo.numRequests)
	src.services[c.ServiceName] = sInfo
	return nil
}
