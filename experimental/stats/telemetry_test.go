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

package stats

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestTelemetryLabels tests registering a callback function with the context and
// the effects of executing the callback on a local label state tracker. Each test
// case constructs a new context with the provided callback registered.
func (s) TestTelemetryLabels(t *testing.T) {
	commonLabelValues := map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_val", "grpc.lb.locality": "grpc.lb.locality_val"}
	tracker := map[string]string{}

	tests := map[string]struct {
		callback         func(map[string]string)
		additionalLabels map[string]string
		wantLabels       map[string]string
	}{
		"NilCallback": {
			callback:   nil,
			wantLabels: map[string]string{},
		},
		"NoOPCallback": {
			callback:   func(map[string]string) {},
			wantLabels: map[string]string{},
		},
		"PanicCallback": {
			callback:   func(map[string]string) { panic("intentional panic") },
			wantLabels: map[string]string{},
		},
		"MutatingCallback": {
			callback: func(u map[string]string) {
				for key, value := range u {
					tracker[key] = value
				}
			},
			wantLabels: map[string]string{"grpc.lb.backend_service": "grpc.lb.backend_service_val", "grpc.lb.locality": "grpc.lb.locality_val"},
		},
		"OverrideLabelsWithCallback": {
			callback: func(u map[string]string) {
				for key, value := range u {
					tracker[key] = value
				}
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
			ctx = WithTelemetryLabelCallback(ctx, test.callback)
			ExecuteTelemetryLabelCallback(ctx, commonLabelValues)
			ExecuteTelemetryLabelCallback(ctx, test.additionalLabels)

			if diff := cmp.Diff(tracker, test.wantLabels); diff != "" {
				t.Fatalf("tracked labels did not match expcted values (-got, +want): %v", diff)
			}
		})
	}
}
