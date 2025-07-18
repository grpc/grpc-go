/*
 *
 * Copyright 2022 gRPC authors.
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

// Package roundrobin contains helper functions to check for roundrobin and
// weighted-roundrobin load balancing of RPCs in tests.
package roundrobin

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"gonum.org/v1/gonum/stat/distuv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var logger = grpclog.Component("testutils-roundrobin")

// waitForTrafficToReachBackends repeatedly makes RPCs using the provided
// TestServiceClient until RPCs reach all backends specified in addrs, or the
// context expires, in which case a non-nil error is returned.
func waitForTrafficToReachBackends(ctx context.Context, client testgrpc.TestServiceClient, addrs []resolver.Address) error {
	// Make sure connections to all backends are up. We need to do this two
	// times (to be sure that round_robin has kicked in) because the channel
	// could have been configured with a different LB policy before the switch
	// to round_robin. And the previous LB policy could be sharing backends with
	// round_robin, and therefore in the first iteration of this loop, RPCs
	// could land on backends owned by the previous LB policy.
	for j := 0; j < 2; j++ {
		for i := 0; i < len(addrs); i++ {
			for {
				time.Sleep(time.Millisecond)
				if ctx.Err() != nil {
					return fmt.Errorf("timeout waiting for connection to %q to be up", addrs[i].Addr)
				}
				var peer peer.Peer
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
					// Some tests remove backends and check if round robin is
					// happening across the remaining backends. In such cases,
					// RPCs can initially fail on the connection using the
					// removed backend. Just keep retrying and eventually the
					// connection using the removed backend will shutdown and
					// will be removed.
					continue
				}
				if peer.Addr.String() == addrs[i].Addr {
					break
				}
			}
		}
	}
	return nil
}

// CheckRoundRobinRPCs verifies that EmptyCall RPCs on the given ClientConn,
// connected to a server exposing the test.grpc_testing.TestService, are
// roundrobined across the given backend addresses.
//
// Returns a non-nil error if context deadline expires before RPCs start to get
// roundrobined across the given backends.
func CheckRoundRobinRPCs(ctx context.Context, client testgrpc.TestServiceClient, addrs []resolver.Address) error {
	if err := waitForTrafficToReachBackends(ctx, client, addrs); err != nil {
		return err
	}

	// At this point, RPCs are getting successfully executed at the backends
	// that we care about. To support duplicate addresses (in addrs) and
	// backends being removed from the list of addresses passed to the
	// roundrobin LB, we do the following:
	// 1. Determine the count of RPCs that we expect each of our backends to
	//    receive per iteration.
	// 2. Wait until the same pattern repeats a few times, or the context
	//    deadline expires.
	wantAddrCount := make(map[string]int)
	for _, addr := range addrs {
		wantAddrCount[addr.Addr]++
	}
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		// Perform 3 more iterations.
		var iterations [][]string
		for i := 0; i < 3; i++ {
			iteration := make([]string, len(addrs))
			for c := 0; c < len(addrs); c++ {
				var peer peer.Peer
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
					return fmt.Errorf("EmptyCall() = %v, want <nil>", err)
				}
				iteration[c] = peer.Addr.String()
			}
			iterations = append(iterations, iteration)
		}
		// Ensure the first iteration contains all addresses in addrs.
		gotAddrCount := make(map[string]int)
		for _, addr := range iterations[0] {
			gotAddrCount[addr]++
		}
		if diff := cmp.Diff(gotAddrCount, wantAddrCount); diff != "" {
			logger.Infof("non-roundrobin, got address count in one iteration: %v, want: %v, Diff: %s", gotAddrCount, wantAddrCount, diff)
			continue
		}
		// Ensure all three iterations contain the same addresses.
		if !cmp.Equal(iterations[0], iterations[1]) || !cmp.Equal(iterations[0], iterations[2]) {
			logger.Infof("non-roundrobin, first iter: %v, second iter: %v, third iter: %v", iterations[0], iterations[1], iterations[2])
			continue
		}
		return nil
	}
	return fmt.Errorf("timeout when waiting for roundrobin distribution of RPCs across addresses: %v", addrs)
}

// CheckWeightedRoundRobinRPCs verifies that EmptyCall RPCs on the given
// ClientConn, connected to a server exposing the test.grpc_testing.TestService,
// are weighted roundrobined (with randomness) across the given backend
// addresses.
//
// Returns a non-nil error if context deadline expires before RPCs start to get
// roundrobined across the given backends.
func CheckWeightedRoundRobinRPCs(ctx context.Context, t *testing.T, client testgrpc.TestServiceClient, addrs []resolver.Address) error {
	if err := waitForTrafficToReachBackends(ctx, client, addrs); err != nil {
		return err
	}

	// At this point, RPCs are getting successfully executed at the backends
	// that we care about. To take the randomness of the WRR into account, we
	// look for approximate distribution instead of exact.
	wantAddrCount := make(map[string]int)
	for _, addr := range addrs {
		wantAddrCount[addr.Addr]++
	}
	attemptCount := attemptCounts(wantAddrCount)

	expectedCount := make(map[string]float64)
	for addr, count := range wantAddrCount {
		expectedCount[addr] = float64(count) / float64(len(addrs)) * float64(attemptCount)
	}

	// There is a small possibility that RPCs are reaching backends that we
	// don't expect them to reach here. The can happen because:
	// - at time T0, the list of backends [A, B, C, D].
	// - at time T1, the test updates the list of backends to [A, B, C], and
	//   immediately starts attempting to check the distribution of RPCs to the
	//   new backends.
	// - there is no way for the test to wait for a new picker to be pushed on
	//   to the channel (which contains the updated list of backends) before
	//   starting to attempt the RPC distribution checks.
	// - This is usually a transitory state and will eventually fix itself when
	//   the new picker is pushed on the channel, and RPCs will start getting
	//   routed to only backends that we care about.
	//
	// We work around this situation by using two loops. The inner loop contains
	// the meat of the calculations, and includes the logic which factors out
	// the randomness in weighted roundrobin. If we ever see an RPCs getting
	// routed to a backend that we don't expect it to get routed to, we break
	// from the inner loop thereby resetting all state and start afresh.
	for {
		observedCount := make(map[string]float64)
	InnerLoop:
		for {
			if ctx.Err() != nil {
				return fmt.Errorf("timeout when waiting for roundrobin distribution of RPCs across addresses: %v", addrs)
			}
			for i := 0; i < attemptCount; i++ {
				var peer peer.Peer
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
					return fmt.Errorf("EmptyCall() = %v, want <nil>", err)
				}
				if addr := peer.Addr.String(); wantAddrCount[addr] == 0 {
					break InnerLoop
				}
				observedCount[peer.Addr.String()]++
			}

			return pearsonsChiSquareTest(t, observedCount, expectedCount)
		}
		<-time.After(time.Millisecond)
	}
}

// attemptCounts returns the number of attempts needed to verify the expected
// distribution of RPCs. Having more attempts per category will give the test
// a greater ability to detect a small but real deviation from the expected
// distribution.
func attemptCounts(wantAddrWeights map[string]int) int {
	if len(wantAddrWeights) == 0 {
		return 0
	}

	totalWeight := 0
	minWeight := -1

	for _, weight := range wantAddrWeights {
		totalWeight += weight
		if minWeight == -1 || weight < minWeight {
			minWeight = weight
		}
	}

	minRatio := float64(minWeight) / float64(totalWeight)

	// We want the expected count for the smallest category to be at least 500.
	// ExpectedCount = TotalAttempts * minRatio
	// So, 500 <= TotalAttempts * minRatio
	// which means TotalAttempts >= 500 / minRatio
	const minExpectedPerCategory = 500.0
	requiredAttempts := minExpectedPerCategory / minRatio

	return int(math.Ceil(requiredAttempts))
}

// pearsonsChiSquareTest checks if the observed counts match the expected
// counts.
// Pearson's Chi-Squared Test Formula:
//
//	χ² = ∑ (Oᵢ - Eᵢ)² / Eᵢ
//
// Where:
//   - χ² is the chi-square statistic
//   - Oᵢ is the observed count for category i
//   - Eᵢ is the expected count for category i
//   - The sum is over all categories (i = 1 to k)
//
// This tests how well the observed distribution matches the expected one.
// Larger χ² values indicate a greater difference between observed and expected
// counts. The p-value is computed as P(χ² ≥ computed value) under the
// chi-square distribution with degrees of freedom:
// df = number of categories - 1
// See https://en.wikipedia.org/wiki/Pearson%27s_chi-squared_test for more
// details.
func pearsonsChiSquareTest(t *testing.T, observedCounts, expectedCounts map[string]float64) error {
	chiSquaredStat := 0.0
	for addr, want := range expectedCounts {
		got := observedCounts[addr]
		chiSquaredStat += (got - want) * (got - want) / want
	}
	degreesOfFreedom := len(expectedCounts) - 1
	const alpha = 1e-6
	chiSquareDist := distuv.ChiSquared{K: float64(degreesOfFreedom)}
	pValue := chiSquareDist.Survival(chiSquaredStat)
	t.Logf("Observed ratio: %v", observedCounts)
	t.Logf("Expected ratio: %v", expectedCounts)
	t.Logf("χ² statistic: %.4f", chiSquaredStat)
	t.Logf("Degrees of freedom: %d\n", degreesOfFreedom)
	t.Logf("p-value: %.10f\n", pValue)
	// Alpha (α) is the threshold we use to decide if the test "fails".
	// It controls how sensitive the chi-square test is.
	//
	// A smaller alpha means we require stronger evidence to say the load
	// balancing is wrong. A larger alpha makes the test more likely to fail,
	// even for small random fluctuations.
	//
	// For CI, we set alpha = 1e-6 to avoid flaky test failures.
	// That means:
	//  - There's only a 1-in-a-million chance the test fails due to random
	//    variation, assuming the load balancer is working correctly.
	//  - If the test *does* fail, it's very likely there's a real bug.
	//
	// TL;DR: smaller alpha = stricter test, fewer false alarms.
	if pValue > alpha {
		return nil
	}
	return fmt.Errorf("observed distribution significantly differs from expectations, observeredCounts: %v, expectedCounts: %v", observedCounts, expectedCounts)
}
