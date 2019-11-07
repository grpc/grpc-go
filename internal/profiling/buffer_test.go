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

package profiling

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCircularBufferSerial(t *testing.T) {
	var size, i uint32
	var result []interface{}

	size = 1 << 15
	cb, err := NewCircularBuffer(size)
	if err != nil {
		t.Errorf("error allocating CircularBuffer: %v", err)
		return
	}

	for i = 0; i < size/2; i++ {
		cb.Push(i)
	}

	result = cb.Drain()
	if uint32(len(result)) != size/2 {
		t.Errorf("expected result size %d, got %d", size/2, len(result))
		return
	}

	seen := make(map[uint32]bool)
	for _, r := range result {
		seen[r.(uint32)] = true
	}

	for i = 0; i < uint32(len(result)); i++ {
		if !seen[i] {
			t.Errorf("expected seen[%d] to be true", i)
			return
		}
	}

	for i = 0; i < size; i++ {
		cb.Push(i)
	}

	result = cb.Drain()
	if uint32(len(result)) != size {
		t.Errorf("expected second push set drain size to be %d, got %d", size/2, len(result))
		return
	}
}

func TestCircularBufferOverflow(t *testing.T) {
	var size, i uint32
	var result []interface{}

	size = 1 << 10
	cb, err := NewCircularBuffer(size)
	if err != nil {
		t.Errorf("error allocating CircularBuffer: %v", err)
		return
	}

	for i = 0; i < size+size/2; i++ {
		cb.Push(i)
	}

	result = cb.Drain()

	if uint32(len(result)) != size {
		t.Errorf("expected drain size to be a full %d, got %d", size, len(result))
		return
	}
}

func TestCircularBufferConcurrent(t *testing.T) {
	for tn := 0; tn < 2; tn++ {
		var size uint32
		var result []interface{}

		size = 1 << 6
		cb, err := NewCircularBuffer(size)
		if err != nil {
			t.Errorf("error allocating CircularBuffer: %v", err)
			return
		}

		type item struct {
			R uint32
			N uint32
			T time.Time
		}

		var wg sync.WaitGroup
		for r := uint32(0); r < 1024; r++ {
			wg.Add(1)
			go func(r uint32) {
				for n := uint32(0); n < size; n++ {
					cb.Push(item{R: r, N: n, T: time.Now()})
				}
				wg.Done()
			}(r)
		}

		// Wait for all goroutines to finish only in one test. Draining
		// concurrently while pushes are still happening will test for races in the
		// draining lock.
		if tn == 0 {
			wg.Wait()
		}

		result = cb.Drain()

		// Can't expect the buffer to be full if the pushes aren't necessarily done.
		if tn == 0 {
			if uint32(len(result)) != size {
				t.Errorf("expected drain size to be a full %d, got %d", size, len(result))
				return
			}
		}

		// There can be absolutely no expectation on the order of the data returned
		// by Drain because: (a) everything is happening concurrently (b) a
		// round-robin is used to write to different queues (and therefore
		// different cachelines) for less write contention.

		// Wait for all goroutines to complete before moving on to other tests. If
		// the benchmarks run after this, it might affect performance unfairly.
		wg.Wait()
	}
}

func BenchmarkCircularBuffer(b *testing.B) {
	for size := 1 << 16; size <= 1<<20; size <<= 1 {
		for routines := 1; routines <= 1<<8; routines <<= 1 {
			b.Run(fmt.Sprintf("goroutines:%d/size:%d", routines, size), func(b *testing.B) {
				cb, err := NewCircularBuffer(uint32(size))
				if err != nil {
					b.Errorf("error allocating CircularBuffer: %v", err)
					return
				}

				perRoutine := b.N / routines
				var wg sync.WaitGroup
				for r := 0; r < routines; r++ {
					wg.Add(1)
					go func() {
						for i := 0; i < perRoutine; i++ {
							x := 1
							cb.Push(&x)
						}
						wg.Done()
					}()
				}
				wg.Wait()
			})
		}
	}
}
