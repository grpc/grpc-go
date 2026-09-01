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

package extproc

import (
	"math"
	"sync/atomic"
	"testing"

	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
)

// TestApplyServerWindowUpdate verifies that a flow control window increment
// from the external processor is applied only when it is a non-negative value
// that does not overflow the window. An out-of-range increment must be
// rejected without mutating the window, so the accounting cannot wrap to a
// value that permanently blocks acquireDownstreamToSidestreamWindow.
func TestApplyServerWindowUpdate(t *testing.T) {
	tests := []struct {
		name       string
		start      int64
		delta      int64
		wantErr    bool
		wantWindow int64
		wantSignal bool
	}{
		{
			name:       "zero increment is a no-op",
			start:      iextproc.DefaultFlowControlWindowSize,
			delta:      0,
			wantWindow: iextproc.DefaultFlowControlWindowSize,
		},
		{
			name:       "valid increment grows the window",
			start:      iextproc.DefaultFlowControlWindowSize,
			delta:      1024,
			wantWindow: iextproc.DefaultFlowControlWindowSize + 1024,
		},
		{
			name:       "increment that crosses zero signals the acquirer",
			start:      -1024,
			delta:      2048,
			wantWindow: 1024,
			wantSignal: true,
		},
		{
			name:       "overflowing increment is rejected and leaves the window unchanged",
			start:      iextproc.DefaultFlowControlWindowSize,
			delta:      math.MaxInt64,
			wantErr:    true,
			wantWindow: iextproc.DefaultFlowControlWindowSize,
		},
		{
			name:       "negative increment is rejected and leaves the window unchanged",
			start:      iextproc.DefaultFlowControlWindowSize,
			delta:      -1,
			wantErr:    true,
			wantWindow: iextproc.DefaultFlowControlWindowSize,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var window atomic.Int64
			window.Store(test.start)
			ch := make(chan struct{}, 1)

			err := applyServerWindowUpdate(&window, ch, test.delta)
			if (err != nil) != test.wantErr {
				t.Fatalf("applyServerWindowUpdate(%d, %d) error = %v, wantErr %v", test.start, test.delta, err, test.wantErr)
			}
			if got := window.Load(); got != test.wantWindow {
				t.Errorf("window = %d, want %d", got, test.wantWindow)
			}
			gotSignal := false
			select {
			case <-ch:
				gotSignal = true
			default:
			}
			if gotSignal != test.wantSignal {
				t.Errorf("positive-update signal = %v, want %v", gotSignal, test.wantSignal)
			}
		})
	}
}
