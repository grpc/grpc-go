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
 */

package cache

import (
	"strconv"
	"testing"
	"time"
)

const (
	testCacheTimeout = 100 * time.Millisecond
)

func (c *TimeoutCache) getForTesting(key interface{}) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.cache[key]
	return r, ok
}

// Test that items will be kept in cache, and be removed after timeout. Callback
// will also be called after timeout.
func TestCacheExpire(t *testing.T) {
	const k, v = 1, "1"
	c := NewTimeoutCache(testCacheTimeout)

	callbackChan := make(chan struct{})
	c.Store(k, v, func() { close(callbackChan) })

	if gotV, ok := c.getForTesting(k); !ok || gotV.item != v {
		t.Fatalf("After store(), before timeout, from cache got: %v, %v, want %v, %v", gotV.item, ok, v, true)
	}

	select {
	case <-callbackChan:
	case <-time.After(testCacheTimeout * 2):
		t.Fatalf("timeout waiting for callback")
	}

	if _, ok := c.getForTesting(k); ok {
		t.Fatalf("After store(), after timeout, from cache got: _, %v, want _, %v", ok, false)
	}
}

// Test that retrieve returns the cached item, and also cancels the timer.
func TestCacheRetrieve(t *testing.T) {
	const k, v = 1, "1"
	c := NewTimeoutCache(testCacheTimeout)

	callbackChan := make(chan struct{})
	c.Store(k, v, func() { close(callbackChan) })

	if got, ok := c.getForTesting(k); !ok || got.item != v {
		t.Fatalf("After store(), before timeout, from cache got: %v, %v, want %v, %v", got.item, ok, v, true)
	}

	time.Sleep(testCacheTimeout / 2)

	gotV, gotOK := c.Retrive(k)
	if !gotOK || gotV != v {
		t.Fatalf("After store(), before timeout, Retrieve() got: %v, %v, want %v, %v", gotV, gotOK, v, true)
	}

	if _, ok := c.getForTesting(k); ok {
		t.Fatalf("After store(), before timeout, after Retrieve(), from cache got: _, %v, want _, %v", ok, false)
	}

	select {
	case <-callbackChan:
		t.Fatalf("unexpected callback after retrieve")
	case <-time.After(testCacheTimeout * 2):
	}
}

// Test that if the timer to an item from cache fires at the same time that
// Retrieve() cancels the timer, it doesn't cause deadlock.
func TestCache_Retrieve_Timeout_New_Race(t *testing.T) {
	c := NewTimeoutCache(time.Nanosecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			// Store starts a timer with 1 ns timeout, then Retrieve will race
			// with the timer.
			c.Store(i, strconv.Itoa(i), func() {})
			c.Retrive(i)
		}
		close(done)
	}()

	select {
	case <-time.After(time.Second):
		t.Fatalf("Test didn't finish within 1 second. Deadlock")
	case <-done:
	}
}
