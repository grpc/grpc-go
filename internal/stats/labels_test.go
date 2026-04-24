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

package stats_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/internal/stats"
)

// TestTelemetryLabelCallbacks tests registering a callback function with the context and
// the effects of executing the callback on a local label state tracker. Each test
// case constructs a new context with the provided callback registered.
func (s) TestTelemetryLabels(t *testing.T) {
	commonLabelValues := map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_val", "grpc.lb.locality": "grpc.lb.locality_val"}

	tests := map[string]struct {
		callbacks        func(tracker map[string]string) []func(map[string]string)
		additionalLabels map[string]string
		wantLabels       map[string]string
	}{
		"NilCallback": {
			callbacks: func(map[string]string) []func(map[string]string) {
				return []func(map[string]string){nil}
			},
			wantLabels: map[string]string{},
		},
		"NoOPCallback": {
			callbacks: func(map[string]string) []func(map[string]string) {
				return []func(map[string]string){
					func(map[string]string) {},
				}
			},
			wantLabels: map[string]string{},
		},
		"TrackingCallback": {
			callbacks: func(tracker map[string]string) []func(map[string]string) {
				return []func(map[string]string){
					func(u map[string]string) {
						for key, value := range u {
							tracker[key] = value
						}
					},
				}
			},
			wantLabels: map[string]string{
				"grpc.lb.backend_service": "grpc.lb.backend_service_val",
				"grpc.lb.locality":        "grpc.lb.locality_val",
			},
		},
		"MultipleInvocations": {
			callbacks: func(tracker map[string]string) []func(map[string]string) {
				return []func(map[string]string){
					func(u map[string]string) {
						for key, value := range u {
							tracker[key] = value
						}
					},
				}
			},
			additionalLabels: map[string]string{"grpc.lb.other_label": "grpc.lb.other_val"},
			wantLabels: map[string]string{
				"grpc.lb.backend_service": "grpc.lb.backend_service_val",
				"grpc.lb.locality":        "grpc.lb.locality_val",
				"grpc.lb.other_label":     "grpc.lb.other_val",
			},
		},
		"MultipleInvocationsWithSameLabel": {
			callbacks: func(tracker map[string]string) []func(map[string]string) {
				return []func(map[string]string){
					func(u map[string]string) {
						for key, value := range u {
							tracker[key] = value
						}
					},
				}
			},
			additionalLabels: map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_other_val"},
			wantLabels: map[string]string{
				"grpc.lb.backend_service": "grpc.lb.backend_service_other_val",
				"grpc.lb.locality":        "grpc.lb.locality_val",
			},
		},
		"MultipleCallbacks": {
			callbacks: func(tracker map[string]string) []func(map[string]string) {
				return []func(map[string]string){
					func(u map[string]string) {
						for key, value := range u {
							k, v := key+"_first", value+"_first"
							tracker[k] = v
						}
					},
					func(u map[string]string) {
						for key, value := range u {
							k, v := key+"_second", value+"_second"
							tracker[k] = v
						}
					},
				}
			},
			wantLabels: map[string]string{
				"grpc.lb.backend_service_first":  "grpc.lb.backend_service_val_first",
				"grpc.lb.locality_first":         "grpc.lb.locality_val_first",
				"grpc.lb.backend_service_second": "grpc.lb.backend_service_val_second",
				"grpc.lb.locality_second":        "grpc.lb.locality_val_second",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tracker := map[string]string{}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			for _, c := range test.callbacks(tracker) {
				ctx = stats.RegisterTelemetryLabelCallback(ctx, c)
			}

			stats.UpdateLabels(ctx, commonLabelValues)
			stats.UpdateLabels(ctx, test.additionalLabels)

			if diff := cmp.Diff(tracker, test.wantLabels); diff != "" {
				t.Fatalf("tracked labels did not match expected values (-got, +want): %v", diff)
			}
		})
	}
}

// TestTelemetryLabelsMutation verifies that callbacks mutating labels do not
// affect the original map passed to UpdateLabels.
func (s) TestTelemetryLabelsMutation(t *testing.T) {
	originalLabels := map[string]string{"key1": "val1", "key2": "val2"}
	// Create a copy to compare against after calling UpdateLabels.
	wantLabels := map[string]string{"key1": "val1", "key2": "val2"}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	ctx = stats.RegisterTelemetryLabelCallback(ctx, func(u map[string]string) {
		u["key1"] = "mutated1"
		u["newkey"] = "newval"
	})

	stats.UpdateLabels(ctx, originalLabels)

	if diff := cmp.Diff(originalLabels, wantLabels); diff != "" {
		t.Fatalf("original labels were mutated by callback (-got, +want): %v", diff)
	}
}

// TestTelemetryLabelsDerivedContexts verifies that creating derived contexts
// with additional callbacks does not override or affect sibling contexts.
func (s) TestTelemetryLabelsDerivedContexts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	var baseExecuted, derived1Executed, derived2Executed uint

	ctxBase := stats.RegisterTelemetryLabelCallback(ctx, func(map[string]string) {
		baseExecuted++
	})

	ctxDerived1 := stats.RegisterTelemetryLabelCallback(ctxBase, func(map[string]string) {
		derived1Executed++
	})

	ctxDerived2 := stats.RegisterTelemetryLabelCallback(ctxBase, func(map[string]string) {
		derived2Executed++
	})

	labels := map[string]string{"k": "v"}

	// Run UpdateLabels to execute the telemetry callbacks on ctxDerived1 which should only increment
	// the base and first derived context counters
	stats.UpdateLabels(ctxDerived1, labels)

	if baseExecuted != 1 || derived1Executed != 1 || derived2Executed != 0 {
		t.Errorf("UpdateLabels(ctxDerived1) executed: base=%v, derived1=%v, derived2=%v; want 1, 1, 0", baseExecuted, derived1Executed, derived2Executed)
	}

	// Run UpdateLabels to execute the telemetry callbacks on ctxDerived2 which should only increment
	// the base and second derived context counters
	stats.UpdateLabels(ctxDerived2, labels)

	if baseExecuted != 2 || derived1Executed != 1 || derived2Executed != 1 {
		t.Errorf("UpdateLabels(ctxDerived2) executed: base=%v, derived1=%v, derived2=%v; want 2, 1, 1", baseExecuted, derived1Executed, derived2Executed)
	}

	// Run UpdateLabels to execute the telemetry callbacks on ctxBase which should only increment
	// the base context counters
	stats.UpdateLabels(ctxBase, labels)

	if baseExecuted != 3 || derived1Executed != 1 || derived2Executed != 1 {
		t.Errorf("UpdateLabels(ctxDerived2) executed: base=%v, derived1=%v, derived2=%v; want 3, 1, 1", baseExecuted, derived1Executed, derived2Executed)
	}
}
