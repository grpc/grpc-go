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

package ringhash

import (
	"fmt"
	"math"
	"testing"

	xxhash "github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/resolver"
)

func testAddr(addr string, weight uint32) resolver.Address {
	return resolver.Address{Addr: addr, Metadata: weight}
}

func TestRingNew(t *testing.T) {
	testAddrs := []resolver.Address{
		testAddr("a", 3),
		testAddr("b", 3),
		testAddr("c", 4),
	}
	var totalWeight float64 = 10
	testSubConnMap := map[resolver.Address]*subConn{
		testAddr("a", 3): {addr: "a"},
		testAddr("b", 3): {addr: "b"},
		testAddr("c", 4): {addr: "c"},
	}
	for _, min := range []uint64{3, 4, 6, 8} {
		for _, max := range []uint64{20, 8} {
			t.Run(fmt.Sprintf("size-min-%v-max-%v", min, max), func(t *testing.T) {
				r, _ := newRing(testSubConnMap, min, max)
				totalCount := len(r.items)
				if totalCount < int(min) || totalCount > int(max) {
					t.Fatalf("unexpect size %v, want min %v, max %v", totalCount, min, max)
				}
				for _, a := range testAddrs {
					var count int
					for _, ii := range r.items {
						if ii.sc.addr == a.Addr {
							count++
						}
					}
					got := float64(count) / float64(totalCount)
					want := float64(a.Metadata.(uint32)) / totalWeight
					if !equalApproximately(got, want) {
						t.Fatalf("unexpected item weight in ring: %v != %v", got, want)
					}
				}
			})
		}
	}
}

func equalApproximately(x, y float64) bool {
	delta := math.Abs(x - y)
	mean := math.Abs(x+y) / 2.0
	return delta/mean < 0.25
}

func TestRingPick(t *testing.T) {
	r, _ := newRing(map[resolver.Address]*subConn{
		{Addr: "a", Metadata: uint32(3)}: {addr: "a"},
		{Addr: "b", Metadata: uint32(3)}: {addr: "b"},
		{Addr: "c", Metadata: uint32(4)}: {addr: "c"},
	}, 10, 20)
	for _, h := range []uint64{xxhash.Sum64String("1"), xxhash.Sum64String("2"), xxhash.Sum64String("3"), xxhash.Sum64String("4")} {
		t.Run(fmt.Sprintf("picking-hash-%v", h), func(t *testing.T) {
			e := r.pick(h)
			var low uint64
			if e.idx > 0 {
				low = r.items[e.idx-1].hash
			}
			high := e.hash
			// h should be in [low, high).
			if h < low || h >= high {
				t.Fatalf("unexpected item picked, hash: %v, low: %v, high: %v", h, low, high)
			}
		})
	}
}

func TestRingNext(t *testing.T) {
	r, _ := newRing(map[resolver.Address]*subConn{
		{Addr: "a", Metadata: uint32(3)}: {addr: "a"},
		{Addr: "b", Metadata: uint32(3)}: {addr: "b"},
		{Addr: "c", Metadata: uint32(4)}: {addr: "c"},
	}, 10, 20)

	for _, e := range r.items {
		ne := r.next(e)
		if ne.idx != (e.idx+1)%len(r.items) {
			t.Fatalf("next(%+v) returned unexpected %+v", e, ne)
		}
	}
}
