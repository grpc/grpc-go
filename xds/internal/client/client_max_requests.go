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

type clientMaxRequests struct {
	mu       sync.Mutex
	services map[string]serviceInfo
}

type serviceInfo struct {
	circuitBreaking bool
	maxRequests     uint32
	numRequests     uint32
}

var c clientMaxRequests

func init() {
	c.services = make(map[string]serviceInfo)
	UpdateService("", false, 0)
}

// UpdateService updates the service with the provided service name, or creates
// it if it doesn't exist.
func UpdateService(serviceName string, circuitBreaking bool, maxRequests uint32) {
	fmt.Println("UpdateService", serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	sInfo, ok := c.services[serviceName]
	if !ok {
		sInfo = serviceInfo{numRequests: 0}
	}
	sInfo.circuitBreaking = circuitBreaking
	sInfo.maxRequests = maxRequests
	c.services[serviceName] = sInfo
}

// StartRequest starts a request for a service, incrementing its number of
// requests by 1. Returns an error if circuit brekaing is on and the max number
// of requests is exceeded.
func StartRequest(serviceName string) error {
	fmt.Println("StartRequest", serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	sInfo, ok := c.services[serviceName]
	if !ok {
		return fmt.Errorf("service name %v not identified", serviceName)
	}
	sInfo.numRequests++
	if sInfo.circuitBreaking && sInfo.numRequests > sInfo.maxRequests {
		return fmt.Errorf("max requests %v exceeded on service %v", sInfo.maxRequests, serviceName)
	}
	c.services[serviceName] = sInfo
	return nil
}

// EndRequest ends a request for a service, decrementing its number of requests
// by 1.
func EndRequest(serviceName string) error {
	fmt.Println("EndRequest", serviceName)
	c.mu.Lock()
	defer c.mu.Unlock()
	sInfo, ok := c.services[serviceName]
	if !ok {
		return fmt.Errorf("service name %v not identified", serviceName)
	}
	sInfo.numRequests--
	c.services[serviceName] = sInfo
	return nil
}
