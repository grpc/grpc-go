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

package cache

import (
	"sync"
	"time"
)

type cacheEntry struct {
	item          interface{}
	callback      func()
	timer         *time.Timer
	abortDeleting bool
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

// Store an item to the cache, with the callback to be called when item is
// removed after timeout.
//
// The return item is the one stored in cache. If the same key is used for a
// second time to store an item, before the item is removed, the item won't be
// stored, the return values will be (false, the previous item)..
func (c *TimeoutCache) Store(key, item interface{}, callback func()) (stored bool, storedItem interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		entry = &cacheEntry{
			item:     item,
			callback: callback,
		}
		entry.timer = time.AfterFunc(c.timeout, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if entry.abortDeleting {
				return
			}
			entry.callback()
			delete(c.cache, key)
		})
		c.cache[key] = entry
		return true, item
	}
	return false, entry.item
}

// Retrive the item with the key from the cache.
//
// The item will be removed from the cache, and the timer for this item will be
// stopped. The callback to be called after timeout will never be called.
func (c *TimeoutCache) Retrive(key interface{}) (item interface{}, ok bool) {
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
		entry.abortDeleting = true
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
