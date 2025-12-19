/*
 *
 * Copyright 2025 gRPC authors.
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

package randomsubsetting

import (
	"fmt"
	"testing"

	xxhash "github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/resolver"

	_ "google.golang.org/grpc/balancer/roundrobin" // For round_robin LB policy in tests
)

func (s) TestSubsettingEndpointsDomain(t *testing.T) {

	testCases := []struct {
		endpoints  []resolver.Endpoint
		subsetSize uint32
		want       uint32
	}{
		{
			endpoints:  makeEndpoints(4),
			subsetSize: 0,
			want:       0,
		},
		{
			endpoints:  makeEndpoints(3),
			subsetSize: 4,
			want:       3,
		},
		{
			endpoints:  makeEndpoints(5),
			subsetSize: 3,
			want:       3,
		},
		{
			endpoints:  []resolver.Endpoint{},
			subsetSize: 1,
			want:       0,
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprint(tc.subsetSize), func(t *testing.T) {
			b := &subsettingBalancer{
				cfg:        &lbConfig{SubsetSize: tc.subsetSize},
				hashSeed:   0,
				hashDigest: xxhash.New(),
			}
			subsetOfEndpoints := b.calculateSubset(tc.endpoints)
			got := len(subsetOfEndpoints)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("subset size=%v; endpoints(%v) = %v; want %v", tc.want, tc.endpoints, got, tc.want)
			}
		})
	}
}

func (s) TestUniformDistributionOfEndpoints(t *testing.T) {

	var (
		subsetSize = 4
		iteration  = 1600
		diff       int
	)

	endpoints := makeEndpoints(16)
	expected := iteration / len(endpoints) * subsetSize
	diff = expected / 7 // allow ~14% difference
	EndpointCount := make(map[string]int, len(endpoints))

	for i := 0; i < iteration; i++ {
		t.Run(fmt.Sprint(subsetSize), func(t *testing.T) {
			t.Helper()
			lb := &subsettingBalancer{
				cfg:        &lbConfig{SubsetSize: uint32(subsetSize)},
				hashSeed:   uint64(i ^ 3 + iteration*i + subsetSize),
				hashDigest: xxhash.New(),
			}
			subsetOfEndpoints := lb.calculateSubset(endpoints)

			for _, ep := range subsetOfEndpoints {
				EndpointCount[ep.Addresses[0].Addr]++
			}
		})
	}
	// Verify the distribution is uniform within a small diff range.
	// The expected count for each endpoint is: iteration / total_endpoints * subset_size
	// e.g. 1600 / 16 * 4 = 400 +/- diff
	for epAddr, count := range EndpointCount {
		if count < expected-diff || count > expected+diff {
			t.Fatalf("endpoint %v selected %v times; expected <=> %v", epAddr, count, expected)
		}
	}
}
