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

package autosharding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
)

const testHeaderName = "x-sharding-key"

// fakeChildPicker fakes the picker returned by a child pick_first balancer.
type fakeChildPicker struct {
	sc  balancer.SubConn
	err error
}

func (p *fakeChildPicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	if p.err != nil {
		return balancer.PickResult{}, p.err
	}
	return balancer.PickResult{SubConn: p.sc}, nil
}

// testEndpointSpec describes the state of an endpoint for picker unit tests.
type testEndpointSpec struct {
	hostname string             // unique endpoint identifier used as the key in endpointMap.
	state    connectivity.State // connectivity state reported by the endpoint's child policy.
	pickErr  error              // error returned by the endpoint's child picker when picked
}

// buildTestEndpoints constructs test state from the given endpoint specs and
// returns:
//   - an *endpointMap populated with one endpointState per spec (indexed 0..len(specs)-1)
//   - a slice of *testutils.TestSubConn, where element i is the SubConn returned
//     by endpoint i's child picker when pickErr is nil
//   - a slice of int counters, where element i records how many times ExitIdle
//     was called on endpoint i
func buildTestEndpoints(specs []testEndpointSpec) (*endpointMap, []*testutils.TestSubConn, []int) {
	em := &endpointMap{m: make(map[string]*endpointState, len(specs))}
	subConns := make([]*testutils.TestSubConn, len(specs))
	exitIdleCounts := make([]int, len(specs))

	for i, spec := range specs {
		sc := testutils.NewTestSubConn(fmt.Sprintf("sc-%d", i))
		subConns[i] = sc
		idx := i
		em.m[spec.hostname] = &endpointState{
			index: idx,
			childState: endpointsharding.ChildState{
				State: balancer.State{
					ConnectivityState: spec.state,
					Picker: &fakeChildPicker{
						sc:  sc,
						err: spec.pickErr,
					},
				},
				ExitIdle: func() {
					exitIdleCounts[idx]++
				},
			},
		}
	}
	return em, subConns, exitIdleCounts
}

// newContextWithShardingKey returns an outgoing context with the testHeaderName
// metadata header set to shardingKey.
func newContextWithShardingKey(ctx context.Context, shardingKey string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(testHeaderName, shardingKey))
}

// Tests that Pick fails with an appropriate error when the outgoing request
// context is missing metadata or does not contain the configured sharding key
// header.
func (s) TestPicker_MissingKeyHeader(t *testing.T) {
	em, _, _ := buildTestEndpoints([]testEndpointSpec{
		{hostname: "hostA", state: connectivity.Ready},
	})
	// Single slice starting at "" covers the complete key range ["", infinity).
	assign := &assignment{
		endpointNames: []string{"hostA"},
		slices:        []slice{{startKey: []byte(""), endpoints: []int{0}}},
		generation:    1,
	}
	sm := buildSliceMap(em, assign)
	p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "no-outgoing-metadata",
			ctx:  ctx,
		},
		{
			name: "missing-target-header",
			ctx:  metadata.NewOutgoingContext(ctx, metadata.Pairs("other-header", "val")),
		},
	}

	const wantErr = "not found in outgoing metadata"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.Pick(balancer.PickInfo{Ctx: tc.ctx}); err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("Pick() error = %v, want error containing %q", err, wantErr)
			}
		})
	}
}

// Tests the picker behavior when no assignment has been received from the
// sharding service and the initial assignment timeout has expired, verifying
// that RPCs fail when fallback is disabled and route to the fallback pool when
// fallback is enabled.
func (s) TestPicker_StartupNoAssignment(t *testing.T) {
	em, subConns, _ := buildTestEndpoints([]testEndpointSpec{
		{hostname: "hostA", state: connectivity.Ready},
	})
	// nil assignment simulates initial_assignment_timeout expiry before any
	// assignment is received from the sharding service.
	sm := buildSliceMap(em, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("fallback-disabled-fails-pick", func(t *testing.T) {
		const wantErr = "no assignment available and fallback is disabled"
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: false})
		if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "any-key")}); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("Pick() error = %v, want error containing %q", err, wantErr)
		}
	})

	t.Run("fallback-enabled-uses-fallback-pool", func(t *testing.T) {
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: true})
		res, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "any-key")})
		if err != nil {
			t.Fatalf("Pick() unexpected error: %v", err)
		}
		if res.SubConn != subConns[0] {
			t.Errorf("Pick() SubConn = %v, want %v", res.SubConn, subConns[0])
		}
	})
}

// Tests per-slice fallback behavior when the matching slice has either zero
// endpoints (a gap slice) or all assigned endpoints in TransientFailure,
// verifying routing with fallback both disabled and enabled.
func (s) TestPicker_PerSliceFallback(t *testing.T) {
	childPickerErr := errors.New("picker error")
	// hostA (index 0) is in TransientFailure; hostB (index 1) is Ready.
	em, subConns, _ := buildTestEndpoints([]testEndpointSpec{
		{hostname: "hostA", state: connectivity.TransientFailure, pickErr: childPickerErr},
		{hostname: "hostB", state: connectivity.Ready},
	})
	assign := &assignment{
		endpointNames: []string{"hostA", "hostB"},
		slices: []slice{
			// Slice 0 ["", "m"): gap slice (zero endpoints).
			{startKey: []byte(""), endpoints: nil},
			// Slice 1 ["m", "z"): assigned only to hostA (which is in TransientFailure).
			{startKey: []byte("m"), endpoints: []int{0}},
			// Slice 2 ["z", nil): assigned to hostB (Ready).
			{startKey: []byte("z"), endpoints: []int{1}},
		},
		generation: 1,
	}
	sm := buildSliceMap(em, assign)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// When the matching slice is a gap (zero endpoints) and fallback is
	// disabled, Pick should fail with an error indicating no available
	// endpoints.
	t.Run("gap-slice-fallback-disabled", func(t *testing.T) {
		const wantErr = "matching slice has no available endpoints"
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: false})
		if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "abc")}); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("Pick() on gap slice error = %v, want error containing %q", err, wantErr)
		}
	})

	// When the matching slice is a gap (zero endpoints) and fallback is
	// enabled, Pick should route across the fallback pool and select the
	// Ready endpoint (hostB).
	t.Run("gap-slice-fallback-enabled", func(t *testing.T) {
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: true})
		res, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "abc")})
		if err != nil {
			t.Fatalf("Pick() on gap slice with fallback enabled failed: %v", err)
		}
		if res.SubConn != subConns[1] {
			t.Errorf("Pick() SubConn = %v, want %v", res.SubConn, subConns[1])
		}
	})

	// When all endpoints in the matching slice are in TransientFailure and
	// fallback is disabled, Pick should delegate to the assigned endpoint's
	// child picker and return its error.
	t.Run("all-tf-slice-fallback-disabled", func(t *testing.T) {
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: false})
		_, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "nnn")})
		if !errors.Is(err, childPickerErr) {
			t.Errorf("Pick() error = %v, want child picker error %v", err, childPickerErr)
		}
	})

	// When all endpoints in the matching slice are in TransientFailure and
	// fallback is enabled, Pick should route across the fallback pool and
	// select the Ready endpoint (hostB).
	t.Run("all-tf-slice-fallback-enabled", func(t *testing.T) {
		p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: true})
		res, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "nnn")})
		if err != nil {
			t.Fatalf("Pick() with fallback enabled failed: %v", err)
		}
		if res.SubConn != subConns[1] {
			t.Errorf("Pick() SubConn = %v, want %v", res.SubConn, subConns[1])
		}
	})
}

// Tests circular scanning of candidate endpoints within a slice across
// combinations of Ready, Idle, Connecting, and TransientFailure states,
// verifying SubConn selection, ExitIdle triggering, and pick queuing/failure.
func (s) TestPicker_EndpointStateScanning(t *testing.T) {
	origRandIntN := randIntN
	defer func() { randIntN = origRandIntN }()

	childPickerErr0 := errors.New("picker error 0")
	childPickerErr1 := errors.New("picker error 1")

	tests := []struct {
		name               string
		specs              []testEndpointSpec
		startIndex         int
		wantSCIdx          int // -1 if expecting error
		wantErr            error
		wantExitIdleCounts []int
	}{
		{
			name: "first-picked-is-ready-does-not-wake-idle",
			specs: []testEndpointSpec{
				{hostname: "host0", state: connectivity.Ready},
				{hostname: "host1", state: connectivity.Idle},
			},
			startIndex:         0,
			wantSCIdx:          0,
			wantExitIdleCounts: []int{0, 0},
		},
		{
			name: "first-picked-is-idle-wakes-idle-and-returns-subsequent-ready",
			specs: []testEndpointSpec{
				{hostname: "host0", state: connectivity.Idle},
				{hostname: "host1", state: connectivity.Idle},
				{hostname: "host2", state: connectivity.Ready},
			},
			startIndex:         0,
			wantSCIdx:          2,
			wantExitIdleCounts: []int{1, 0, 0},
		},
		{
			name: "multiple-idle-no-ready-wakes-at-most-one-idle-and-queues",
			specs: []testEndpointSpec{
				{hostname: "host0", state: connectivity.Idle},
				{hostname: "host1", state: connectivity.Idle},
				{hostname: "host2", state: connectivity.TransientFailure, pickErr: childPickerErr0},
			},
			startIndex:         1,
			wantSCIdx:          -1,
			wantErr:            balancer.ErrNoSubConnAvailable,
			wantExitIdleCounts: []int{0, 1, 0},
		},
		{
			name: "connecting-and-tf-no-idle-queues-pick",
			specs: []testEndpointSpec{
				{hostname: "host0", state: connectivity.TransientFailure, pickErr: childPickerErr0},
				{hostname: "host1", state: connectivity.Connecting},
			},
			startIndex:         0,
			wantSCIdx:          -1,
			wantErr:            balancer.ErrNoSubConnAvailable,
			wantExitIdleCounts: []int{0, 0},
		},
		{
			name: "all-transient-failure-delegates-to-first-selected-index",
			specs: []testEndpointSpec{
				{hostname: "host0", state: connectivity.TransientFailure, pickErr: childPickerErr0},
				{hostname: "host1", state: connectivity.TransientFailure, pickErr: childPickerErr1},
			},
			startIndex:         1,
			wantSCIdx:          -1,
			wantErr:            childPickerErr1,
			wantExitIdleCounts: []int{0, 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			randIntN = func(int) int { return tc.startIndex }

			em, subConns, exitIdleCounts := buildTestEndpoints(tc.specs)
			names := make([]string, len(tc.specs))
			indices := make([]int, len(tc.specs))
			for i, spec := range tc.specs {
				names[i] = spec.hostname
				indices[i] = i
			}
			// Single slice starting at "" covers the complete key range ["", infinity).
			assign := &assignment{
				endpointNames: names,
				slices:        []slice{{startKey: []byte(""), endpoints: indices}},
				generation:    1,
			}
			sm := buildSliceMap(em, assign)
			p := newPicker(em, sm, &lbConfig{KeyHeaderName: testHeaderName, EnableFallback: false})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			res, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "key")})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Pick() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantSCIdx >= 0 && res.SubConn != subConns[tc.wantSCIdx] {
				t.Errorf("Pick() SubConn = %v, want %v", res.SubConn, subConns[tc.wantSCIdx])
			}
			if diff := cmp.Diff(tc.wantExitIdleCounts, exitIdleCounts); diff != "" {
				t.Errorf("ExitIdle call counts diff (-want +got):\n%s", diff)
			}
		})
	}
}
