/*
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

// Package cache implements caches to be used in gRPC.
package cache

import (
	"sync"
	"time"
)

type cacheEntry struct {
	item     interface{}
	callback func()
	timer    *time.Timer
	// deleted is set to true when timer.Stop() fails. This can happen when
	// Remove() races with the timer (timer fires at the same time
	// timer.stop() is called).
	//
	// This variable needs to be checked before deleting the entry and calling
	// callback, to make sure the deleting is canceled.
	deleted bool
}

// TimeoutCache is a cache with items to be deleted after a timeout.
type TimeoutCache struct {
	mu      sync.Mutex
	timeout time.Duration
	cache   map[interface{}]*cacheEntry
}

// NewTimeoutCache creates a TimeoutCache with the given timeout.
func NewTimeoutCache(timeout time.Duration) *TimeoutCache {
	return &TimeoutCache{
		timeout: timeout,
		cache:   make(map[interface{}]*cacheEntry),
	}
}

// Add adds an item to the cache, with the specified callback to be called when
// the item is removed from the cache upon timeout. If the item is removed from
// the cache using a call to Remove before the timeout expires, the callback
// will not be called.
//
// If the Add was successful, it returns (newly added item, true). If there is
// an existing entry for the specified key, the cache entry is not be updated
// with the specified item and it returns (existing item, false).
func (c *TimeoutCache) Add(key, item interface{}, callback func()) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.cache[key]; ok {
		return e.item, false
	}

	entry := &cacheEntry{
		item:     item,
		callback: callback,
	}
	entry.timer = time.AfterFunc(c.timeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if entry.deleted {
			// Abort deleting even if timer fires. This mean there was a race
			// between stopping timer and the timer itself.
			return
		}
		entry.callback()
		delete(c.cache, key)
	})
	c.cache[key] = entry
	return item, true
}

// Remove the item with the key from the cache.
//
// The item will be removed from the cache, and the timer for this item will be
// stopped. The callback to be called after timeout will never be called.
func (c *TimeoutCache) Remove(key interface{}) (item interface{}, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.retrieveAndRemoveItemUnlocked(key)
	if !ok {
		return nil, false
	}
	return entry.item, true
}

// retrieveAndRemoveItemUnlocked removes and returns the item with key. It
// doesn't call the callback.
//
// caller must hold c.mu.
func (c *TimeoutCache) retrieveAndRemoveItemUnlocked(key interface{}) (*cacheEntry, bool) {
	entry, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	delete(c.cache, key)
	if !entry.timer.Stop() {
		// If stop was not successful, the timer has fired (this can only happen
		// in a race). But the deleting function is blocked on c.mu because the
		// mutex was held by the caller of this function.
		//
		// Set deleted to true to abort the deleting function. When the lock is
		// released, the delete function will acquire the lock, check the value
		// of deleted and return.
		entry.deleted = true
	}
	return entry, true
}

// Clear removes all entries, but doesn't run the callbacks.
func (c *TimeoutCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.cache {
		c.retrieveAndRemoveItemUnlocked(key)
	}
}
