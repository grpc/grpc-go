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

// Package cache provides an LRU cache implementation to be used by the RLS LB
// policy to cache RLS response data.
package cache

import (
	"container/list"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/status"
)

// Key represents the cache key used to uniquely identify a cache entry.
type Key struct {
	// Path is the full path of the incoming RPC request.
	Path string
	// KeyMap is a stringified version of the RLS request keys built using the
	// RLS keyBuilder. Since map is not a Type which is comparable in Go, it
	// cannot be part of the key for another map (the LRU cache is implemented
	// using a native map type).
	KeyMap string
}

// Entry wraps all the data to be stored in a cache entry.
type Entry struct {
	// ExpiryTime is the absolute time at which the data cached as part of this
	// entry stops being valid. When an RLS request succeeds, this is set to
	// the current time plus the max_age field from the LB policy config. An
	// entry with this field in the past is not used to process picks.
	ExpiryTime time.Time
	// StaleTime is the absolute time after which this entry will be
	// proactively refreshed if we receive a request for it. When an RLS
	// request succeeds, this is set to the current time plus the stale_age
	// from the LB policy config.
	StaleTime time.Time
	// BackoffExpiryTime is the absolute time at which the backoff period for
	// this entry ends.  When an RLS request fails, this is set to the current
	// time plus twice the backoff time.
	BackoffExpiryTime time.Time
	// EarliestEvictTime is the absolute time before which this entry should
	// not be evicted from the cache. This is set to a default value of 5
	// seconds when the entry is created. This is required to make sure that a
	// new entry added to the cache is not evicted before the RLS response
	// arrives (usually when the cache is too small).
	EarliestEvictTime time.Time
	// CallStatus stores the RPC status of the previous RLS request. Picks for
	// entries with a non-OK status are failed with the error stored here.
	CallStatus *status.Status
	// Backoff contains all backoff related state. When an RLS request
	// succeeds, backoff state is reset.
	Backoff *BackoffState
	// HeaderData is received in an RLS response and is to be sent in the
	// X-Google-RLS-Data header for matching RPCs.
	HeaderData string
	// TODO(easwars): Add support to store the ChildPolicy here. Need a
	// balancerWrapper type to be implemented for this.
}

// BackoffState wraps all backoff related state associated with a cache entry.
type BackoffState struct {
	// Retries keeps track of the number of RLS failures, to be able to
	// determine the amount of time to backoff before the next attempt.
	Retries int
	// Backoff is an exponential backoff implementation which returns the
	// amount of time to backoff, given the number of retries.
	Backoff backoff.Strategy
	// Timer fires when the backoff period ends and incoming requests after
	// this will trigger a new RLS request.
	Timer *time.Timer
	// Callback provided by the LB policy to be notified when the backoff timer
	// expires. This will trigger a new picker to be returned to the
	// ClientConn, to force queued up RPCs to be retried.
	Callback func()
}

// lruEntry is the actual entry which is stored in the LRU cache. It requires
// the key (in addition to the actual entry) as well, since the onEvicted
// callback and expiry timer would need the key to perform their job.
type lruEntry struct {
	key  Key
	val  *Entry
	size int
}

// LRU is a cache with a least recently used eviction policy.
//
// It is not safe for concurrent access. The RLS LB policy will provide a mutex
// which will synchronize access to this cache from its multiple users: the LB
// policy, picker and the expiry timer.
type LRU struct {
	maxSize   int
	usedSize  int
	onEvicted func(Key, *Entry)

	ll    *list.List
	cache map[Key]*list.Element
}

// NewLRU creates a cache.LRU with a size limit of maxSize and the provided
// eviction callback.
//
// Currently, only the cache.Key and the HeaderData field from cache.Entry
// count towards the size of the cache (other overhead per cache entry is not
// counted). The cache could temporarily exceed the configured maxSize because
// we want the entries to spend a configured minimum amount of time in the
// cache before they are LRU evicted (so that all the work performed in sending
// an RLS request and caching the response is not a total waste).
//
// The provided onEvited callback must not attempt to re-add the entry inline
// and the RLS LB policy does not have a need to do that.
//
// The cache package trusts the RLS policy (its only user) to supply a default
// minimum non-zero maxSize, in the event that the ServiceConfig does not
// provide a value for it.
func NewLRU(maxSize int, onEvicted func(Key, *Entry)) *LRU {
	return &LRU{
		maxSize:   maxSize,
		onEvicted: onEvicted,
		ll:        list.New(),
		cache:     make(map[Key]*list.Element),
	}
}

// TODO(easwars): If required, make this function more sophisticated.
func entrySize(key Key, value *Entry) int {
	return len(key.Path) + len(key.KeyMap) + len(value.HeaderData)
}

// removeToFit removes older entries from the cache to make room for a new
// entry of size newSize.
func (lru *LRU) removeToFit(newSize int) {
	for lru.usedSize+newSize > lru.maxSize {
		elem := lru.ll.Back()
		if elem == nil {
			// This is a corner case where the cache is empty, but the new entry
			// to be added is bigger than maxSize.
			grpclog.Info("rls: newly added cache entry exceeds cache maxSize")
			return
		}

		entry := elem.Value.(*lruEntry).val
		if t := entry.EarliestEvictTime; !t.IsZero() && t.Before(time.Now()) {
			// When the oldest entry is too new (it hasn't even spent a default
			// minimum amount of time in the cache), we abort and allow the
			// cache to grow bigger than the configured maxSize.
			grpclog.Info("rls: LRU eviction finds oldest entry to be too new. Allowing cache to exceed maxSize momentarily")
			return
		}
		lru.removeOldest()
	}
}

// Add adds a new entry to the cache.
func (lru *LRU) Add(key Key, value *Entry) {
	size := entrySize(key, value)
	elem, ok := lru.cache[key]
	if !ok {
		lru.removeToFit(size)
		lru.usedSize += size
		elem := lru.ll.PushFront(&lruEntry{key, value, size})
		lru.cache[key] = elem
		return
	}

	lruE := elem.Value.(*lruEntry)
	sizeDiff := size - lruE.size
	lru.removeToFit(sizeDiff)
	lruE.val = value
	lruE.size = size
	lru.ll.MoveToFront(elem)
	lru.usedSize += sizeDiff
}

// Remove removes a cache entry wth key key, if one exists.
func (lru *LRU) Remove(key Key) {
	if elem, ok := lru.cache[key]; ok {
		lru.removeElement(elem)
	}
}

func (lru *LRU) removeOldest() {
	elem := lru.ll.Back()
	if elem != nil {
		lru.removeElement(elem)
	}
}

func (lru *LRU) removeElement(e *list.Element) {
	lruE := e.Value.(*lruEntry)
	lru.ll.Remove(e)
	delete(lru.cache, lruE.key)
	lru.usedSize -= lruE.size
	if lru.onEvicted != nil {
		lru.onEvicted(lruE.key, lruE.val)
	}
}

// Get returns a cache entry with key key.
func (lru *LRU) Get(key Key) *Entry {
	elem, ok := lru.cache[key]
	if !ok {
		return nil
	}
	lru.ll.MoveToFront(elem)
	return elem.Value.(*lruEntry).val
}
