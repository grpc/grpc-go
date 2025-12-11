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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	_ "google.golang.org/grpc/balancer/roundrobin" // For round_robin LB policy in tests
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestParseConfig(t *testing.T) {
	parser := bb{}
	tests := []struct {
		name    string
		input   string
		wantCfg serviceconfig.LoadBalancingConfig
		wantErr string
	}{
		{
			name:    "invalid_json",
			input:   "{{invalidjson{{",
			wantErr: "json.Unmarshal failed for configuration",
		},
		{
			name:    "empty_config",
			input:   `{}`,
			wantErr: "SubsetSize must be greater than 0",
		},
		{
			name:    "subset_size_zero",
			input:   `{ "subsetSize": 0 }`,
			wantErr: "SubsetSize must be greater than 0",
		},
		{
			name:    "child_policy_missing",
			input:   `{ "subsetSize": 1 }`,
			wantErr: "ChildPolicy must be specified",
		},
		{
			name:    "child_policy_not_registered",
			input:   `{ "subsetSize": 1 , "childPolicy": [{"unregistered_lb": {}}] }`,
			wantErr: "no supported policies found",
		},
		{
			name:  "success",
			input: `{ "subsetSize": 3, "childPolicy": [{"round_robin": {}}]}`,
			wantCfg: &lbConfig{
				SubsetSize:  3,
				ChildPolicy: &iserviceconfig.BalancerConfig{Name: "round_robin"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCfg, gotErr := parser.ParseConfig(json.RawMessage(test.input))
			// Substring match makes this very tightly coupled to the
			// internalserviceconfig.BalancerConfig error strings. However, it
			// is important to distinguish the different types of error messages
			// possible as the parser has a few defined buckets of ways it can
			// error out.
			if (gotErr != nil) != (test.wantErr != "") {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if gotErr != nil && !strings.Contains(gotErr.Error(), test.wantErr) {
				t.Fatalf("ParseConfig(%v) = %v, wantErr %v", test.input, gotErr, test.wantErr)
			}
			if test.wantErr != "" {
				return
			}
			if diff := cmp.Diff(test.wantCfg, gotCfg); diff != "" {
				t.Fatalf("ParseConfig(%s) got unexpected output, diff (-want +got):\n%s", test.input, diff)
			}
		})
	}
}

func makeEndpoints(n int) []resolver.Endpoint {
	endpoints := make([]resolver.Endpoint, n)
	for i := 0; i < n; i++ {
		endpoints[i] = resolver.Endpoint{
			Addresses: []resolver.Address{{Addr: fmt.Sprintf("endpoint-%d", i)}},
		}
	}
	return endpoints
}

func (s) TestCalculateSubset_Simple(t *testing.T) {
	tests := []struct {
		name       string
		endpoints  []resolver.Endpoint
		subsetSize uint32
		want       []resolver.Endpoint
	}{
		{
			name:       "NoEndpoints",
			endpoints:  []resolver.Endpoint{},
			subsetSize: 3,
			want:       []resolver.Endpoint{},
		},
		{
			name:       "SubsetSizeLargerThanNumberOfEndpoints",
			endpoints:  makeEndpoints(5),
			subsetSize: 10,
			want:       makeEndpoints(5),
		},
		{
			name:       "SubsetSizeEqualToNumberOfEndpoints",
			endpoints:  makeEndpoints(5),
			subsetSize: 5,
			want:       makeEndpoints(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &subsettingBalancer{
				cfg:        &lbConfig{SubsetSize: tt.subsetSize},
				hashSeed:   0,
				hashDigest: xxhash.New(),
			}
			got := b.calculateSubset(tt.endpoints)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("calculateSubset() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestCalculateSubset_EndpointsRetainHashValues(t *testing.T) {
	endpoints := makeEndpoints(10)
	const subsetSize = 5
	// The subset is deterministic based on the hash, so we can hardcode
	// the expected output.
	want := []resolver.Endpoint{
		{Addresses: []resolver.Address{{Addr: "endpoint-6"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-0"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-1"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-7"}}},
		{Addresses: []resolver.Address{{Addr: "endpoint-3"}}},
	}

	b := &subsettingBalancer{
		cfg:        &lbConfig{SubsetSize: subsetSize},
		hashSeed:   0,
		hashDigest: xxhash.New(),
	}
	for range 10 {
		got := b.calculateSubset(endpoints)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("calculateSubset() returned diff (-want +got):\n%s", diff)
		}
	}
}

func (s) TestSubsettingBalancer_DeterministicSubset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Register the stub balancer builder, which will be used as the child
	// policy in the random_subsetting balancer.
	updateCh := make(chan balancer.ClientConnState, 1)
	stub.Register("stub-child-balancer", stub.BalancerFuncs{
		UpdateClientConnState: func(_ *stub.BalancerData, s balancer.ClientConnState) error {
			select {
			case <-ctx.Done():
			case updateCh <- s:
			}
			return nil
		},
	})

	// Create a random_subsetting balancer.
	tcc := testutils.NewBalancerClientConn(t)
	rsb := balancer.Get(Name).Build(tcc, balancer.BuildOptions{})
	defer rsb.Close()

	// Prepare the configuration and resolver state to be passed to the
	// random_subsetting balancer.
	endpoints := makeEndpoints(10)
	state := balancer.ClientConnState{
		ResolverState: resolver.State{Endpoints: endpoints},
		BalancerConfig: &lbConfig{
			SubsetSize:  5,
			ChildPolicy: &iserviceconfig.BalancerConfig{Name: "stub-child-balancer"},
		},
	}

	// Send the resolver state to the random_subsetting balancer and verify that
	// the child policy receives the expected number of endpoints.
	if err := rsb.UpdateClientConnState(state); err != nil {
		t.Fatalf("UpdateClientConnState failed: %v", err)
	}

	var wantEndpoints []resolver.Endpoint
	select {
	case s := <-updateCh:
		if len(s.ResolverState.Endpoints) != 5 {
			t.Fatalf("Child policy received %d endpoints, want 5", len(s.ResolverState.Endpoints))
		}
		// Store the subset for the next comparison.
		wantEndpoints = s.ResolverState.Endpoints
	case <-ctx.Done():
		t.Fatal("Timed out waiting for child policy to receive an update")
	}

	// Call UpdateClientConnState again with the same configuration.
	if err := rsb.UpdateClientConnState(state); err != nil {
		t.Fatalf("Second UpdateClientConnState failed: %v", err)
	}

	// Verify that the child policy receives the same subset of endpoints.
	select {
	case s := <-updateCh:
		if diff := cmp.Diff(wantEndpoints, s.ResolverState.Endpoints); diff != "" {
			t.Fatalf("Child policy received a different subset of endpoints on second update, diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for second child policy update")
	}
}
