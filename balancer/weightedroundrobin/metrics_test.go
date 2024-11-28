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
	tests := []struct {
		name                           string
		weightExpirationPeriod         time.Duration
		blackoutPeriod                 time.Duration
		lastUpdated                    time.Time
		nonEmpty                       time.Time
		nowTime                        time.Time
		endpointWeightStaleWant        float64
		endpointWeightNotYetUsableWant float64
		endpointWeightWant             float64
	}{
		// The weighted SubConn's lastUpdated field hasn't been set, so this
		// SubConn's weight is not yet usable. Thus, should emit that endpoint
		// weight is not yet usable, and 0 for weight.
		{
			name:                           "no weight set",
			weightExpirationPeriod:         time.Second,
			blackoutPeriod:                 time.Second,
			nowTime:                        time.Now(),
			endpointWeightStaleWant:        0,
			endpointWeightNotYetUsableWant: 1,
			endpointWeightWant:             0,
		},
		{
			name:                           "weight expiration",
			lastUpdated:                    time.Now(),
			weightExpirationPeriod:         2 * time.Second,
			blackoutPeriod:                 time.Second,
			nowTime:                        time.Now().Add(100 * time.Second),
			endpointWeightStaleWant:        1,
			endpointWeightNotYetUsableWant: 0,
			endpointWeightWant:             0,
		},
		{
			name:                           "in blackout period",
			lastUpdated:                    time.Now(),
			weightExpirationPeriod:         time.Minute,
			blackoutPeriod:                 10 * time.Second,
			nowTime:                        time.Now(),
			endpointWeightStaleWant:        0,
			endpointWeightNotYetUsableWant: 1,
			endpointWeightWant:             0,
		},
		{
			name:                           "normal weight",
			lastUpdated:                    time.Now(),
			nonEmpty:                       time.Now(),
			weightExpirationPeriod:         time.Minute,
			blackoutPeriod:                 time.Second,
			nowTime:                        time.Now().Add(10 * time.Second),
			endpointWeightStaleWant:        0,
			endpointWeightNotYetUsableWant: 0,
			endpointWeightWant:             3,
		},
		{
			name:                           "weight expiration takes precdedence over blackout",
			lastUpdated:                    time.Now(),
			nonEmpty:                       time.Now(),
			weightExpirationPeriod:         time.Second,
			blackoutPeriod:                 time.Minute,
			nowTime:                        time.Now().Add(10 * time.Second),
			endpointWeightStaleWant:        1,
			endpointWeightNotYetUsableWant: 0,
			endpointWeightWant:             0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tmr := stats.NewTestMetricsRecorder()
			wsc := &endpointWeight{
				metricsRecorder: tmr,
				weightVal:       3,
				lastUpdated:     test.lastUpdated,
				nonEmptySince:   test.nonEmpty,
			}
			wsc.weight(test.nowTime, test.weightExpirationPeriod, test.blackoutPeriod, true)

			if got, _ := tmr.Metric("grpc.lb.wrr.endpoint_weight_stale"); got != test.endpointWeightStaleWant {
				t.Fatalf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.wrr.endpoint_weight_stale", got, test.endpointWeightStaleWant)
			}
			if got, _ := tmr.Metric("grpc.lb.wrr.endpoint_weight_not_yet_usable"); got != test.endpointWeightNotYetUsableWant {
				t.Fatalf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.wrr.endpoint_weight_not_yet_usable", got, test.endpointWeightNotYetUsableWant)
			}
			if got, _ := tmr.Metric("grpc.lb.wrr.endpoint_weight_stale"); got != test.endpointWeightStaleWant {
				t.Fatalf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.wrr.endpoint_weight_stale", got, test.endpointWeightStaleWant)
			}
		})
	}

}

// TestWRR_Metrics_Scheduler_RR_Fallback tests the round robin fallback metric
// for scheduler updates. It tests the case with one SubConn, and two SubConns
// with no weights. Both of these should emit a count metric for round robin
// fallback.
func (s) TestWRR_Metrics_Scheduler_RR_Fallback(t *testing.T) {
	tmr := stats.NewTestMetricsRecorder()
	ew := &endpointWeight{
		metricsRecorder: tmr,
		weightVal:       0,
	}

	p := &picker{
		cfg: &lbConfig{
			BlackoutPeriod:         iserviceconfig.Duration(10 * time.Second),
			WeightExpirationPeriod: iserviceconfig.Duration(3 * time.Minute),
		},
		weightedPickers: []pickerWeightedEndpoint{{weightedEndpoint: ew}},
		metricsRecorder: tmr,
	}
	// There is only one SubConn, so no matter if the SubConn has a weight or
	// not will fallback to round robin.
	p.regenerateScheduler()
	if got, _ := tmr.Metric("grpc.lb.wrr.rr_fallback"); got != 1 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.wrr.rr_fallback", got, 1)
	}
	tmr.ClearMetrics()

	// With two SubConns, if neither of them have weights, it will also fallback
	// to round robin.
	ew2 := &endpointWeight{
		target:          "target",
		metricsRecorder: tmr,
		weightVal:       0,
	}
	p.weightedPickers = append(p.weightedPickers, pickerWeightedEndpoint{weightedEndpoint: ew2})
	p.regenerateScheduler()
	if got, _ := tmr.Metric("grpc.lb.wrr.rr_fallback"); got != 1 {
		t.Fatalf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.wrr.rr_fallback", got, 1)
	}
}
