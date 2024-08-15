/*
 *
 * Copyright 2021 gRPC authors.
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

package rls

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/testutils/stats"
)

var (
	cacheKeys = []cacheKey{
		{path: "0", keys: "a"},
		{path: "1", keys: "b"},
		{path: "2", keys: "c"},
		{path: "3", keys: "d"},
		{path: "4", keys: "e"},
	}

	longDuration  = 10 * time.Minute
	shortDuration = 1 * time.Millisecond
	cacheEntries  []*cacheEntry
)

func initCacheEntries() {
	// All entries have a dummy size of 1 to simplify resize operations.
	cacheEntries = []*cacheEntry{
		{
			// Entry is valid and minimum expiry time has not expired.
			expiryTime:        time.Now().Add(longDuration),
			earliestEvictTime: time.Now().Add(longDuration),
			size:              1,
		},
		{
			// Entry is valid and is in backoff.
			expiryTime:   time.Now().Add(longDuration),
			backoffTime:  time.Now().Add(longDuration),
			backoffState: &backoffState{timer: time.NewTimer(longDuration)},
			size:         1,
		},
		{
			// Entry is valid, and not in backoff.
			expiryTime: time.Now().Add(longDuration),
			size:       1,
		},
		{
			// Entry is invalid.
			expiryTime: time.Time{}.Add(shortDuration),
			size:       1,
		}, // Different scenarios - does he loop through these for different scenarios...?
		{
			// Entry is invalid valid and backoff has expired.
			expiryTime:        time.Time{}.Add(shortDuration),
			backoffExpiryTime: time.Time{}.Add(shortDuration),
			size:              1,
		},
	}
}

func (s) TestLRU_BasicOperations(t *testing.T) {
	initCacheEntries()
	// Create an LRU and add some entries to it.
	lru := newLRU()
	for _, k := range cacheKeys {
		lru.addEntry(k)
	}

	// Get the least recent entry. This should be the first entry we added.
	if got, want := lru.getLeastRecentlyUsed(), cacheKeys[0]; got != want {
		t.Fatalf("lru.getLeastRecentlyUsed() = %v, want %v", got, want)
	}

	// Iterate through the slice of keys we added earlier, making them the most
	// recent entry, one at a time. The least recent entry at that point should
	// be the next entry from our slice of keys.
	for i, k := range cacheKeys {
		lru.makeRecent(k)

		lruIndex := (i + 1) % len(cacheKeys)
		if got, want := lru.getLeastRecentlyUsed(), cacheKeys[lruIndex]; got != want {
			t.Fatalf("lru.getLeastRecentlyUsed() = %v, want %v", got, want)
		}
	}

	// Iterate through the slice of keys we added earlier, removing them one at
	// a time The least recent entry at that point should be the next entry from
	// our slice of keys, except for the last one because the lru will be empty.
	for i, k := range cacheKeys {
		lru.removeEntry(k)

		var want cacheKey
		if i < len(cacheKeys)-1 {
			want = cacheKeys[i+1]
		}
		if got := lru.getLeastRecentlyUsed(); got != want {
			t.Fatalf("lru.getLeastRecentlyUsed() = %v, want %v", got, want)
		}
	}
}

func (s) TestDataCache_BasicOperations(t *testing.T) {
	initCacheEntries() // Same 5 scenarios for the different tests...
	dc := newDataCache(5, nil, &stats.NoopMetricsRecorder{}, "")
	for i, k := range cacheKeys {
		dc.addEntry(k, cacheEntries[i])
	}
	// This above could be smoke check, and then immediately assert on cache size and number of entries...




	for i, k := range cacheKeys {
		entry := dc.getEntry(k)
		if !cmp.Equal(entry, cacheEntries[i], cmp.AllowUnexported(cacheEntry{}, backoffState{}), cmpopts.IgnoreUnexported(time.Timer{})) {
			t.Fatalf("Data cache lookup for key %v returned entry %v, want %v", k, entry, cacheEntries[i])
		}
	}
}

func (s) TestDataCache_AddForcesResize(t *testing.T) {
	initCacheEntries()
	dc := newDataCache(1, nil, &stats.NoopMetricsRecorder{}, "")

	// The first entry in cacheEntries has a minimum expiry time in the future.
	// This entry would stop the resize operation since we do not evict entries
	// whose minimum expiration time is in the future. So, we do not use that
	// entry in this test. The entry being added has a running backoff timer.
	evicted, ok := dc.addEntry(cacheKeys[1], cacheEntries[1])
	if evicted || !ok {
		t.Fatalf("dataCache.addEntry() returned (%v, %v) want (false, true)", evicted, ok)
	}

	// Add another entry leading to the eviction of the above entry which has a
	// running backoff timer. The first return value is expected to be true.
	backoffCancelled, ok := dc.addEntry(cacheKeys[2], cacheEntries[2])
	if !backoffCancelled || !ok {
		t.Fatalf("dataCache.addEntry() returned (%v, %v) want (true, true)", backoffCancelled, ok)
	}

	// Add another entry leading to the eviction of the above entry which does not
	// have a running backoff timer. This should evict the above entry, but the
	// first return value is expected to be false.
	backoffCancelled, ok = dc.addEntry(cacheKeys[3], cacheEntries[3])
	if backoffCancelled || !ok {
		t.Fatalf("dataCache.addEntry() returned (%v, %v) want (false, true)", backoffCancelled, ok)
	}
}

func (s) TestDataCache_Resize(t *testing.T) {
	initCacheEntries()
	dc := newDataCache(5, nil, &stats.NoopMetricsRecorder{}, "")
	for i, k := range cacheKeys {
		dc.addEntry(k, cacheEntries[i])
	}

	// The first cache entry (with a key of cacheKeys[0]) that we added has an
	// earliestEvictTime in the future. As part of the resize operation, we
	// traverse the cache in least recently used order, and this will be first
	// entry that we will encounter. And since the earliestEvictTime is in the
	// future, the resize operation will stop, leaving the cache bigger than
	// what was asked for.
	if dc.resize(1) {
		t.Fatalf("dataCache.resize() returned true, want false")
	}
	if dc.currentSize != 5 {
		t.Fatalf("dataCache.size is %d, want 5", dc.currentSize)
	}
	// Can resize without actual assertions...

	// Remove the entry with earliestEvictTime in the future and retry the
	// resize operation.
	dc.removeEntryForTesting(cacheKeys[0])
	if !dc.resize(1) {
		t.Fatalf("dataCache.resize() returned false, want true")
	}
	if dc.currentSize != 1 {
		t.Fatalf("dataCache.size is %d, want 1", dc.currentSize)
	}
}

func (s) TestDataCache_EvictExpiredEntries(t *testing.T) {
	initCacheEntries()
	dc := newDataCache(5, nil, &stats.NoopMetricsRecorder{}, "")
	for i, k := range cacheKeys {
		dc.addEntry(k, cacheEntries[i])
	}

	// The last two entries in the cacheEntries list have expired, and will be
	// evicted. The first three should still remain in the cache.
	if !dc.evictExpiredEntries() {
		t.Fatal("dataCache.evictExpiredEntries() returned false, want true")
	}
	if dc.currentSize != 3 {
		t.Fatalf("dataCache.size is %d, want 3", dc.currentSize)
	}
	for i := 0; i < 3; i++ {
		entry := dc.getEntry(cacheKeys[i])
		if !cmp.Equal(entry, cacheEntries[i], cmp.AllowUnexported(cacheEntry{}, backoffState{}), cmpopts.IgnoreUnexported(time.Timer{})) {
			t.Fatalf("Data cache lookup for key %v returned entry %v, want %v", cacheKeys[i], entry, cacheEntries[i])
		}
	}
}

func (s) TestDataCache_ResetBackoffState(t *testing.T) {
	type fakeBackoff struct {
		backoff.Strategy
	}

	initCacheEntries()
	dc := newDataCache(5, nil, &stats.NoopMetricsRecorder{}, "")
	for i, k := range cacheKeys {
		dc.addEntry(k, cacheEntries[i])
	}

	newBackoffState := &backoffState{bs: &fakeBackoff{}}
	if updatePicker := dc.resetBackoffState(newBackoffState); !updatePicker {
		t.Fatal("dataCache.resetBackoffState() returned updatePicker is false, want true")
	}

	// Make sure that the entry with no backoff state was not touched.
	if entry := dc.getEntry(cacheKeys[0]); cmp.Equal(entry.backoffState, newBackoffState, cmp.AllowUnexported(backoffState{})) {
		t.Fatal("dataCache.resetBackoffState() touched entries without a valid backoffState")
	}

	// Make sure that the entry with a valid backoff state was reset.
	entry := dc.getEntry(cacheKeys[1])
	if diff := cmp.Diff(entry.backoffState, newBackoffState, cmp.AllowUnexported(backoffState{})); diff != "" {
		t.Fatalf("unexpected diff in backoffState for cache entry after dataCache.resetBackoffState(): %s", diff)
	}
}

var cacheEntriesMetricsTests = []*cacheEntry{
	{size: 3},
	{size: 3},
	{size: 3},
	{size: 3},
	{size: 3},
}

func (s) TestDataCache_Metrics(t *testing.T) {
	tmr := stats.NewTestMetricsRecorder(t)
	// Add the 5 cache entries...any non determinism I need to poll for or is this processing synchronous?
	initCacheEntries()
	dc := newDataCache(50, nil, tmr, "")
	// Should I check labels emissions...just tests gRPC target, rls server target, and uuid which idk is even worth testing...
	dc.updateRLSServerTarget("rls-server-target") // synchronized from test, this is guaranteed to happen before cache operations triggered from balancer...
	for i, k := range cacheKeys {
		dc.addEntry(k, cacheEntriesMetricsTests[i])
	}

	// 5 total entries, size 3 each, so should record 5 entries and 15
	// size.

	// PR out for smoke tests...and then further discuss others
	tmr.AssertDataForMetric("grpc.lb.rls.cache_entries", 5)
	// could just run it and see what value gets added from sync processing...
	// It would also provide me insight...
	tmr.AssertDataForMetric("grpc.lb.rls.cache_size", 15)

	// Resize down the cache to 3 entries. This should scale down the cache to 3
	// entries with 3 bytes each, so should record 3 entries and 9 size.
	dc.resize(9) // evicts, should scale down, maybe make cache entries with same length
	tmr.AssertDataForMetric("grpc.lb.rls.cache_entries", 3)
	tmr.AssertDataForMetric("grpc.lb.rls.cache_size", 9)


	// Update an entry to have size 5. This should reflect in the size metrics,
	// which will increase by 2 to 11, while the number of cache entries should
	// stay same.
	dc.updateEntrySize(cacheEntriesMetricsTests[4], 5) // is this deterministic, if not do this in separate test, higher level operations are unit tested for functionality I think this gets all done...
	// this writes to global var...need to cleanup
	defer func() {
		cacheEntriesMetricsTests[4].size = 3
	}() // coupled assertions he might ask to make this a t-test or something...

	tmr.AssertDataForMetric("grpc.lb.rls.cache_entries", 3)
	tmr.AssertDataForMetric("grpc.lb.rls.cache_size", 11) // write comments for the high level explanations...

	// Delete this scaled up cache key. This should scale down the cache to 2
	// entries, and remove 5 size so cache size should be 6.
	dc.deleteAndCleanup(cacheKeys[4], cacheEntriesMetricsTests[4])
	tmr.AssertDataForMetric("grpc.lb.rls.cache_entries", 2)
	tmr.AssertDataForMetric("grpc.lb.rls.cache_size", 6)
} // When get back scale this up and figure out picker smoke test and then send out for review before 1:1 (smoke test but not complicated sceanrios for picker...could I really not scale that up non deterministic...)

// What other operations could trigger?

// This tests all 3 plus a layer above resize based off recently used...
// Let's see if he wants me to split it up...this is done for cache

// discuss picker/e2e non determinism
// for picker smoke test to induce needs to pick - could try a failure

// smoke test for all 3 we don't check labels anyway so just induce all 3

// nondeterminism for e2e tests...

// This is done so write picker sanity check....

/*
func (s) TestDataCache_OtherOperations(t *testing.T) {
	dc := newDataCache(5, nil, tmr, "")
	dc.resize()

}

func (s) TestDataCache_BasicMetrics(t *testing.T) {

	// There's 5 cache entries...after all processing check size (trivial case
	// is add and then I can play around) or other operations...or some
	// permutation of operations...

	// Can I deterministically induce expired entries?

	// fmr here...

	// cache operations which trigger size and entries emissions

	// add entry and remove entry?

	// what does initCacheEntries do?

	// what 5 operations can trigger...

	tmr := stats.NewTestMetricsRecorder(t) // what does t do?
	// what does this do?

	dc := newDataCache(/*max size, shouldn't affect metrics, nil, tmr, "grpc-target") // should show up in labels...

	// Direct 5 operations that can trigger cache size...
	// What setup does this need to work properly...

	dc.updateEntrySize()

	dc.addEntry()

	dc.deleteAndCleanup() // what data does this need to get it working?


	// Assertion operations for cache tests:
	tmr.AssertDataForMetric("grpc.lb.rls.cache_entries", 5) // If keeping in init cache, but do I need those get timestamps relative to testing run
	tmr.AssertDataForMetric("grpc.lb.rls.cache_size", /*I don't know how many bytes this ends up being...)

	// Do I want to scale these up to have labels?

	// Assertion operations for picker tests:
	// Smoke tests (try and get these two done before 1:1, can even push out to look at)
	tmr.AssertDataForMetrics("grpc.lb.rls.default_target_picks", 1) // how do I even induce this without full picker state/in a balancer...



	// Higher layer cache operations:
	dc.updateRLSServerTarget() // test this somehow, not thread safe but accesses into it protected by higher layer...


    // Don't need to retest this, these are tested by unit tests for functionality
	// and call the underlying methods, tell Doug this...


	dc.evictExpiredEntries() // Higher level operation, updateRLSServerTarget should reflect in labels emitted from here...

	dc.resetBackoffState() // what does this do? I think a no-op so don't need to test...is this even worth testing?

	// This calls a lot of underlying operations...
	// see his other tests?
	dc.resize(/*size) // resizes max size...I think this is a higher level operation, does Easwar invoke cache operations directly or implicitly through cache entry?

	dc.stop()


	// newCache creates all the data structures needed...

	// verify labels?


	// in flight PR has check on addEntry...


	// Not safe for concurrent access...this test happens before does that for you

	// T-Test:
	// Do a cache operation...on same cache?

	// synchronous processing...
	// Expect emission for cacheSize and cacheEntries...

	// Not at startup...
	dc.updateRLSServerTarget() // this should happen first...check if this is received...is it even possible to receive a cache operation before this is called
	// in the balancer?

	// update the RLS Server target. For the RLS Balancer, cache operations always trigger
	// after the first RLS Server target is received from a config update.
	// This is a required label, so it doesn't make sense to get any cache metrics emissions
	// without this label present...what does balancer do? Look at tests above too...

	// on first config update might resize cache it seems...

	// We're good successfully writes rls server target before every
	// cache operation that could trigger...

	// See if GCS can build off commit on master and see metrics

	// at the very least get a smoke test working...

	// smoke test - 5 entries add, check

	// picker - barebones emissions...make sure it makes sense
	// what is minimum required to get this working...at the very least scale up his test
	// and see what metrics get emitted...?

	// Three buckets - normal target picks, default target picks, rls failures
	// first two have enum...so to trigger a smoke check need to make a Pick...

	// Whether through full balancer or not I do not know...how easy would it be to set up smoke test...


	// and ask Frank to test it...with new commit and make sure dashboards show up

}
*/
