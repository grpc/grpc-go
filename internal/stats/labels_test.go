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
	tracker := map[string]string{}

	tests := map[string]struct {
		callbacks        []func(map[string]string)
		additionalLabels map[string]string
		wantLabels       map[string]string
	}{
		"NilCallback": {
			callbacks:  []func(map[string]string){nil},
			wantLabels: map[string]string{},
		},
		"NoOPCallback": {
			callbacks: []func(map[string]string){
				func(map[string]string) {},
			},
			wantLabels: map[string]string{},
		},
		"PanicCallback": {
			callbacks: []func(map[string]string){
				func(map[string]string) { panic("intentional panic") },
			},
			wantLabels: map[string]string{},
		},
		"MutatingCallback": {
			callbacks: []func(u map[string]string){
				func(u map[string]string) {
					for key, value := range u {
						tracker[key] = value
					}
				},
			},
			wantLabels: map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_val", "grpc.lb.locality": "grpc.lb.locality_val"},
		},
		"PanicAndMutateCallback": {
			callbacks: []func(map[string]string){
				func(map[string]string) { panic("intentional panic") },
				func(u map[string]string) {
					for key, value := range u {
						tracker[key] = value
					}
				},
			},
			wantLabels: map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_val", "grpc.lb.locality": "grpc.lb.locality_val"},
		},
		"OverrideLabelsWithCallback": {
			callbacks: []func(u map[string]string){
				func(u map[string]string) {
					for key, value := range u {
						tracker[key] = value
					}
				},
			},
			additionalLabels: map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_other_val"},
			wantLabels:       map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_other_val", "grpc.lb.locality": "grpc.lb.locality_val"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// resest the tracker at the end of every test
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			t.Cleanup(func() {
				tracker = map[string]string{}
				cancel()
			})

			for _, c := range test.callbacks {
				ctx = stats.RegisterTelemetryLabelCallback(ctx, c)
			}

			stats.UpdateLabels(ctx, commonLabelValues)
			stats.UpdateLabels(ctx, test.additionalLabels)

			if diff := cmp.Diff(tracker, test.wantLabels); diff != "" {
				t.Fatalf("tracked labels did not match expcted values (-got, +want): %v", diff)
			}
		})
	}
}
