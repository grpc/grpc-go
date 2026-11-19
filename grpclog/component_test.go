/*
 *
 * Copyright 2026 gRPC authors.
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

package grpclog

import (
	"fmt"
	"sync"
	"testing"
)

// TestComponent verifies that Component returns a non-nil logger and that
// subsequent calls with the same name return the cached instance.
func TestComponent(t *testing.T) {
	const name = "test-component"
	c1 := Component(name)
	if c1 == nil {
		t.Fatalf("Component(%q) = nil, want non-nil", name)
	}
	c2 := Component(name)
	if c1 != c2 {
		t.Fatalf("Component(%q) returned a different instance on second call; got %p, want %p", name, c2, c1)
	}
}

// TestComponentConcurrent tests that Component can be safely called from
// multiple goroutines concurrently, verifying both cache hits and concurrent
// insertions without data races.
//
// This reflects runtime scenarios where multiple channels, dynamic xDS client
// streams (such as LRS, ADS, authority in internal/xds/clients/...), or custom
// balancers and resolvers invoke Component() at runtime in parallel.
func TestComponentConcurrent(t *testing.T) {
	const (
		numGoroutines = 50
		numIterations = 100
	)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(workerID int) {
			defer wg.Done()
			for j := range numIterations {
				// Alternately request shared and distinct component names to exercise
				// both read hits and concurrent writes.
				sharedName := fmt.Sprintf("shared-component-%d", j%5)
				if c := Component(sharedName); c == nil {
					t.Errorf("Component(%q) = nil, want non-nil", sharedName)
				}

				uniqueName := fmt.Sprintf("unique-component-%d-%d", workerID, j)
				if c := Component(uniqueName); c == nil {
					t.Errorf("Component(%q) = nil, want non-nil", uniqueName)
				}
			}
		}(i)
	}

	wg.Wait()
}

// BenchmarkComponent benchmarks parallel lookups on the component logger cache.
func BenchmarkComponent(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = Component("benchmark-component")
		}
	})
}
