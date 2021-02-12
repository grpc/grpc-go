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
	"sync"
	"sync/atomic"
	"testing"
)

type counterTest struct {
	name              string
	maxRequests       uint32
	numRequests       uint32
	expectedSuccesses uint32
	expectedErrors    uint32
}

var tests = []counterTest{
	{
		name:              "does-not-exceed-max-requests",
		maxRequests:       1024,
		numRequests:       1024,
		expectedSuccesses: 1024,
		expectedErrors:    0,
	},
	{
		name:              "exceeds-max-requests",
		maxRequests:       32,
		numRequests:       64,
		expectedSuccesses: 32,
		expectedErrors:    32,
	},
}

func resetServiceRequestsCounter() {
	src = &servicesRequestsCounter{
		services: make(map[string]*ServiceRequestsCounter),
	}
}

func testCounter(t *testing.T, test counterTest) {
	SetMaxRequests(test.name, &test.maxRequests)
	requestsStarted := make(chan struct{})
	requestsSent := sync.WaitGroup{}
	requestsSent.Add(int(test.numRequests))
	requestsDone := sync.WaitGroup{}
	requestsDone.Add(int(test.numRequests))
	var lastError atomic.Value
	var successes, errors uint32
	for i := 0; i < int(test.numRequests); i++ {
		go func() {
			counter := GetServiceRequestsCounter(test.name)
			defer requestsDone.Done()
			err := counter.StartRequest()
			if err == nil {
				atomic.AddUint32(&successes, 1)
			} else {
				atomic.AddUint32(&errors, 1)
				lastError.Store(err)
			}
			requestsSent.Done()
			if err == nil {
				<-requestsStarted
				counter.EndRequest()
			}
		}()
	}
	requestsSent.Wait()
	close(requestsStarted)
	requestsDone.Wait()
	loadedError := lastError.Load()
	if test.expectedErrors > 0 && loadedError == nil {
		t.Error("no error when error expected")
	}
	if test.expectedErrors == 0 && loadedError != nil {
		t.Errorf("error starting request: %v", loadedError.(error))
	}
	if successes != test.expectedSuccesses || errors != test.expectedErrors {
		t.Errorf("unexpected number of (successes, errors), expected (%v, %v), encountered (%v, %v)", test.expectedSuccesses, test.expectedErrors, successes, errors)
	}
}

func (s) TestRequestsCounter(t *testing.T) {
	resetServiceRequestsCounter()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testCounter(t, test)
		})
	}
}

func (s) TestGetServiceRequestsCounter(t *testing.T) {
	resetServiceRequestsCounter()
	for _, test := range tests {
		counterA := GetServiceRequestsCounter(test.name)
		counterB := GetServiceRequestsCounter(test.name)
		if counterA != counterB {
			t.Errorf("counter %v %v != counter %v %v", counterA, *counterA, counterB, *counterB)
		}
	}
}

func startRequests(t *testing.T, n uint32, max uint32, counter *ServiceRequestsCounter) {
	SetMaxRequests(counter.ServiceName, &max)
	for i := uint32(0); i < n; i++ {
		if err := counter.StartRequest(); err != nil {
			t.Fatalf("error starting initial request: %v", err)
		}
	}
}

func (s) TestSetMaxRequestsIncreased(t *testing.T) {
	resetServiceRequestsCounter()
	const serviceName string = "set-max-requests-increased"
	var initialMax uint32 = 16
	counter := GetServiceRequestsCounter(serviceName)
	startRequests(t, initialMax, initialMax, counter)
	if err := counter.StartRequest(); err == nil {
		t.Fatal("unexpected success on start request after max met")
	}
	newMax := initialMax + 1
	SetMaxRequests(counter.ServiceName, &newMax)
	if err := counter.StartRequest(); err != nil {
		t.Fatalf("unexpected error on start request after max increased: %v", err)
	}
}

func (s) TestSetMaxRequestsDecreased(t *testing.T) {
	resetServiceRequestsCounter()
	const serviceName string = "set-max-requests-decreased"
	var initialMax uint32 = 16
	counter := GetServiceRequestsCounter(serviceName)
	startRequests(t, initialMax-1, initialMax, counter)
	newMax := initialMax - 1
	SetMaxRequests(counter.ServiceName, &newMax)
	if err := counter.StartRequest(); err == nil {
		t.Fatalf("unexpected success on start request after max decreased: %v", err)
	}
}
