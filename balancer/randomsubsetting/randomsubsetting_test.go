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
	"math"
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

func (s) TestSubsettingEndpointsSimply(t *testing.T) {

	testCases := []struct {
		endpoints  []resolver.Endpoint
		subsetSize uint32
		want       uint32
	}{
		{
			endpoints:  makeEndpoints(100),
			subsetSize: 10,
			want:       10,
		},
		{
			endpoints:  makeEndpoints(10),
			subsetSize: 2,
			want:       2,
		},
		{
			endpoints:  makeEndpoints(150),
			subsetSize: 50,
			want:       50,
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

type DistributionData struct {
	cardinality   int
	mean          float64
	sigma         float64
	expectSuccess bool
}

func (s) TestUniformDistributionOfEndpoints(t *testing.T) {

	testCases := []struct {
		eps        int
		subsetSize uint32
		iteration  uint32
		positive   bool
	}{
		{
			eps:        16,
			subsetSize: 4,
			iteration:  10,
			positive:   true,
		},
		{
			eps:        40,
			subsetSize: 20,
			iteration:  100,
			positive:   true,
		},
		{
			eps:        10,
			subsetSize: 2,
			iteration:  1000,
			positive:   true,
		},
		{
			eps:        16,
			subsetSize: 4,
			iteration:  1600,
			positive:   true,
		},
		{
			eps:        8,
			subsetSize: 4,
			iteration:  3200,
			positive:   true,
		},
		{
			eps:        4,
			subsetSize: 4,
			iteration:  6400,
			positive:   true,
		},
		{
			eps:        6,
			subsetSize: 1,
			iteration:  30,
			positive:   false,
		},
		{
			eps:        17,
			subsetSize: 2,
			iteration:  100,
			positive:   false,
		},
	}

	for _, tc := range testCases {
		endpoints := makeEndpoints(tc.eps)
		// From a set of N numbers, we randomly select K-times a subset of L numbers,
		// where L < N. Calculate how many times each number belonging to set N appears.
		// Calculate the variance and standard deviation.
		// Check whether the distribution is uniform.

		// Theoretical Calculations
		N := len(endpoints)
		L := int(tc.subsetSize)
		K := int(tc.iteration)

		p := float64(L) / float64(N)         // Probability of x ∈ N being drawn p(x) = L / N
		E := float64(K) * p                  // Expected Value (Mean) E(N) = K * p
		variance := float64(K) * p * (1 - p) // Variance σ²(N) = K * p * (1 - p)
		sigma := math.Sqrt(variance)         // Standard Deviation σ(N) = sqrt(σ²(N))

		EndpointCount := make(map[string]int, N)
		initEndpointCount(&EndpointCount, endpoints)

		for i := 0; i < K; i++ {
			t.Run(fmt.Sprint(L), func(t *testing.T) {
				t.Helper()
				lb := &subsettingBalancer{
					cfg:        &lbConfig{SubsetSize: uint32(L)},
					hashSeed:   uint64(i ^ 3 + K*i + L),
					hashDigest: xxhash.New(),
				}
				subsetOfEndpoints := lb.calculateSubset(endpoints)

				for _, ep := range subsetOfEndpoints {
					EndpointCount[ep.Addresses[0].Addr]++
				}
			})
		}
		t.Logf("Test Case: Endpoints=%d, SubsetSize=%d, Iterations=%d", N, L, K)
		result, msgs := verifyUniformDistribution(EndpointCount, DistributionData{N, E, sigma, tc.positive})
		if !result {
			t.Errorf("Distribution check failed:\n%s", msgs.String())
		} else {
			t.Logf("Distribution check passed:\n%s", msgs.String())
		}
	}
}

func verifyUniformDistribution(eps map[string]int, dd DistributionData) (bool, strings.Builder) {
	// Verify the distribution is uniform within a small diff range.
	var (
		chi2          float64
		reportBuilder strings.Builder
		testPassed    bool = true
	)

	reportBuilder.WriteString(fmt.Sprintf("%-12s | %-15s | %-15s | %s\n", "Endpoint", "ExpValue", "Diff from E", "Status"))
	reportBuilder.WriteString(strings.Repeat("-", 75) + "\n")

	for epAddr, count := range eps {
		diff := float64(count) - dd.mean
		absDiff := math.Abs(diff)

		// Chi-Square Statistic Calculation (χ²)
		chi2 += (diff * diff) / dd.mean

		// Determine Sigma Range Status
		status := "      E ± σ  (Normal)"
		if absDiff > 3*dd.sigma {
			status = "!!! > E ± 3σ (Extreme)"
		} else if absDiff > 2*dd.sigma {
			status = "!!  > E ± 2σ (Rare)"
		} else if absDiff > dd.sigma {
			status = "!   > E ± σ  (Noticeable)"
		}

		reportBuilder.WriteString(fmt.Sprintf("%-12s | %-15.2f | %-15.2f | %s\n", epAddr, dd.mean, absDiff, status))
	}
	reportBuilder.WriteString(strings.Repeat("-", 75) + "\n")

	// Degrees of Freedom
	df := float64(dd.cardinality - 1)

	// Critical Value for α = 0.05 and df
	criticalValue := chiSquareCriticalValue(0.05, df)

	if chi2 > criticalValue {
		if dd.expectSuccess {
			testPassed = false
		} else {
			testPassed = true
		}
		reportBuilder.WriteString(fmt.Sprintf("Distribution is not uniform (χ²=%.2f > critical value=%.2f)", chi2, criticalValue))
	} else {
		if !dd.expectSuccess {
			testPassed = false
		}
		reportBuilder.WriteString(fmt.Sprintf("Distribution is uniform (χ²=%.2f <= critical value=%.2f)", chi2, criticalValue))
	}
	return testPassed, reportBuilder
}

// Initialize EndpointCount map with endpoint addresses as keys and zero counts.
func initEndpointCount(epCount *map[string]int, eps []resolver.Endpoint) {
	for _, ep := range eps {
		(*epCount)[ep.Addresses[0].Addr] = 0
	}
}

// ChiSquareCriticalValue calculates the critical value for alpha (e.g., 0.05)
// and degrees of freedom (df).
func chiSquareCriticalValue(alpha float64, df float64) float64 {
	// 1. Find the Z-score for the given alpha.
	// For alpha = 0.05 (95% confidence), Z is approx 1.64485
	z := getZScore(1 - alpha)

	// 2. Wilson-Hilferty transformation
	dfFloat := float64(df)
	fraction := 2.0 / (9.0 * dfFloat)

	// Formula: df * (1 - 2/(9df) + z * sqrt(2/(9df)))^3
	inner := 1.0 - fraction + z*math.Sqrt(fraction)
	return dfFloat * math.Pow(inner, 3)
}

// Helper to get Z-score for common alpha levels
func getZScore(probability float64) float64 {
	// Simplified mapping for common statistical confidence levels
	switch {
	case probability >= 0.99:
		return 2.326
	case probability >= 0.975:
		return 1.960
	case probability >= 0.95:
		return 1.645
	case probability >= 0.90:
		return 1.282
	default:
		return 1.645 // Default to 95%
	}
}
