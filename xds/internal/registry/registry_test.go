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
 *
 */

package registry

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

func numItemsForTesting() int {
	return registry.numItems()
}

func invalidateForTesting() {
	registry.invalidate()
}

func (cr *clientRegistry) numItems() int {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return len(cr.clients)
}

func (cr *clientRegistry) invalidate() {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.clients = make(map[string]interface{})
}

const (
	numRoutines = 10
	numOps      = 100
)

// addConcurrent adds a bunch of items to the registry by spawning a bunch of
// goroutines.
func addConcurrent(t *testing.T) []string {
	t.Helper()

	keys := make([][]string, numRoutines)
	var wg sync.WaitGroup
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				k := AddTo(nil)
				t.Logf("added: %v", k)
				// keys[index] = append(keys[index], AddTo(nil))
				keys[index] = append(keys[index], k)
			}
		}(i)
	}
	wg.Wait()

	allKeys := []string{}
	for _, k := range keys {
		allKeys = append(allKeys, k...)
	}

	if len(allKeys) != numRoutines*numOps {
		t.Fatalf("gotKeys: %v, wantKeys: %v", len(allKeys), numRoutines*numOps)
	}
	return allKeys
}

// TestAddTo verifies that there are no entries with duplicate keys after
// concurrents adds to the registry.
func TestAddTo(t *testing.T) {
	invalidateForTesting()
	allKeys := addConcurrent(t)

	sort.Strings(allKeys)
	for i := 0; i < len(allKeys)-1; i++ {
		if allKeys[i] == allKeys[i+1] {
			t.Fatal("registry contains two elements with the same key")
		}
	}
}

func TestRemoveFrom(t *testing.T) {
	invalidateForTesting()
	allKeys := addConcurrent(t)

	var wg sync.WaitGroup
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := index * numOps; j < (index+1)*numOps; j++ {
				RemoveFrom(allKeys[j])
			}
		}(i)
	}
	wg.Wait()

	if got := numItemsForTesting(); got != 0 {
		t.Fatalf("registry still has %v items, should have 0", got)
	}
}

func TestGet(t *testing.T) {
	invalidateForTesting()
	allKeys := addConcurrent(t)

	errCh := make(chan error, numRoutines)
	var wg sync.WaitGroup
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := index * numOps; j < (index+1)*numOps; j++ {
				if _, err := Get(allKeys[j]); err != nil {
					errCh <- fmt.Errorf("registry.Get(%v) returned error: %v", allKeys[j], err)
					return
				}
			}
			errCh <- nil
		}(i)
	}
	wg.Wait()

	for i := 0; i < numRoutines; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}
