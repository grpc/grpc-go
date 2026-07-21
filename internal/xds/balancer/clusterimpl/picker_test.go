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

package clusterimpl

import "testing"

func (s) TestDropRequestsPerMillion(t *testing.T) {
	tests := []struct {
		name        string
		numerator   uint32
		denominator uint32
		want        uint32
	}{
		{name: "zero", numerator: 0, denominator: million, want: 0},
		{name: "five percent hundred", numerator: 5, denominator: 100, want: 50000},
		// numerator*million overflows a uint32 once numerator reaches 4295 with
		// the million denominator, which is only a 0.43% drop.
		{name: "point four three percent million", numerator: 4295, denominator: million, want: 4295},
		{name: "one percent million", numerator: 10000, denominator: million, want: 10000},
		{name: "fifty percent million", numerator: 500000, denominator: million, want: 500000},
		{name: "hundred percent million", numerator: million, denominator: million, want: million},
		// A fraction above 100% is capped at a million.
		{name: "over hundred percent", numerator: 150, denominator: 100, want: million},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dropRequestsPerMillion(tt.numerator, tt.denominator); got != tt.want {
				t.Errorf("dropRequestsPerMillion(%d, %d) = %d, want %d", tt.numerator, tt.denominator, got, tt.want)
			}
		})
	}
}
