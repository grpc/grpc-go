/*
 *
 * Copyright 2024 gRPC authors.
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

package weightedroundrobin

import (
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils/stats"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestWRR_Metrics_SubConnWeight tests different scenarios for the weight call
// on a weighted SubConn, and expects certain metrics for each of these
// scenarios.
func (s) TestWRR_Metrics_SubConnWeight(t *testing.T) {
	tmr := stats.NewTestMetricsRecorder(t, []string{"grpc.lb.wrr.rr_fallback", "grpc.lb.wrr.endpoint_weight_not_yet_usable", "grpc.lb.wrr.endpoint_weight_stale", "grpc.lb.wrr.endpoint_weights"})

	wsc := &weightedSubConn{
		metricsRecorder: tmr,
		weightVal:       3,
	}

	// The weighted SubConn's lastUpdated field hasn't been set, so this
	// SubConn's weight is not yet usable. Thus, should emit that endpoint
	// weight is not yet usable, and 0 for weight.
	wsc.weight(time.Now(), time.Second, time.Second, true)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_stale", 0) // The endpoint weight has not expired so this is 0.
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_not_yet_usable", 1)
	// Unusable, so no endpoint weight (i.e. 0).
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weights", 0)
	tmr.ClearMetrics()

	// Setup a scenario where the SubConn's weight expires. Thus, should emit
	// that endpoint weight is stale, and 0 for weight.
	wsc.lastUpdated = time.Now()
	wsc.weight(time.Now().Add(100*time.Second), 2*time.Second, time.Second, true)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_stale", 1)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_not_yet_usable", 0)
	// Unusable, so no endpoint weight (i.e. 0).
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weights", 0)
	tmr.ClearMetrics()

	// Setup a scenario where the SubConn's weight is in the blackout period.
	// Thus, should emit that endpoint weight is not yet usable, and 0 for
	// weight.
	wsc.weight(time.Now(), time.Minute, 10*time.Second, true)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_stale", 0)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_not_yet_usable", 1)
	// Unusable, so no endpoint weight (i.e. 0).
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weights", 0)
	tmr.ClearMetrics()

	// Setup a scenario where SubConn's weight is what is persists in weight
	// field. This is triggered by last update being past blackout period and
	// before weight update period. Should thus emit that endpoint weight is 3,
	// and no other metrics.
	wsc.nonEmptySince = time.Now()
	wsc.weight(time.Now().Add(10*time.Second), time.Minute, time.Second, true)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_stale", 0)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_not_yet_usable", 0)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weights", 3)
	tmr.ClearMetrics()

	// Setup a scenario where a SubConn's weight both expires and is within the
	// blackout period. In this case, weight expiry should take precedence with
	// respect to metrics emitted. Thus, should emit that endpoint weight is not
	// yet usable, and 0 for weight.
	wsc.weight(time.Now().Add(10*time.Second), time.Second, time.Minute, true)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_stale", 1)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weight_not_yet_usable", 0)
	tmr.AssertDataForMetric("grpc.lb.wrr.endpoint_weights", 0)
	tmr.ClearMetrics()
}

// TestWRR_Metrics_Scheduler_RR_Fallback tests the round robin fallback metric
// for scheduler updates. It tests the case with one SubConn, and two SubConns
// with no weights. Both of these should emit a count metric for round robin
// fallback.
func (s) TestWRR_Metrics_Scheduler_RR_Fallback(t *testing.T) {
	tmr := stats.NewTestMetricsRecorder(t, []string{"grpc.lb.wrr.rr_fallback", "grpc.lb.wrr.endpoint_weight_not_yet_usable", "grpc.lb.wrr.endpoint_weight_stale", "grpc.lb.wrr.endpoint_weights"})
	wsc := &weightedSubConn{
		metricsRecorder: tmr,
		weightVal:       0,
	}

	p := &picker{
		cfg: &lbConfig{
			BlackoutPeriod:         iserviceconfig.Duration(10 * time.Second),
			WeightExpirationPeriod: iserviceconfig.Duration(3 * time.Minute),
		},
		subConns:        []*weightedSubConn{wsc},
		metricsRecorder: tmr,
	}
	// There is only one SubConn, so no matter if the SubConn has a weight or
	// not will fallback to round robin.
	p.regenerateScheduler()
	tmr.AssertDataForMetric("grpc.lb.wrr.rr_fallback", 1)
	tmr.ClearMetrics()

	// With two SubConns, if neither of them have weights, it will also fallback
	// to round robin.
	wsc2 := &weightedSubConn{
		target:          "target",
		metricsRecorder: tmr,
		weightVal:       0,
	}
	p.subConns = append(p.subConns, wsc2)
	p.regenerateScheduler()
	tmr.AssertDataForMetric("grpc.lb.wrr.rr_fallback", 1)
}
