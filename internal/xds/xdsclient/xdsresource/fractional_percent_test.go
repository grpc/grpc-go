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
 */

package xdsresource

import (
	"testing"

	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func (s) TestNewFractionalPercent(t *testing.T) {
	tests := []struct {
		name    string
		fp      *v3typepb.FractionalPercent
		want    FractionalPercent
		wantErr bool
	}{
		{
			name: "NilIsZeroOutOfHundred",
			fp:   nil,
			want: FractionalPercent{Numerator: 0, Denominator: 100, PPM: 0},
		},
		{
			name: "FivePercentHundred",
			fp:   &v3typepb.FractionalPercent{Numerator: 5, Denominator: v3typepb.FractionalPercent_HUNDRED},
			want: FractionalPercent{Numerator: 5, Denominator: 100, PPM: 50000},
		},
		{
			name: "HalfPercentTenThousand",
			fp:   &v3typepb.FractionalPercent{Numerator: 50, Denominator: v3typepb.FractionalPercent_TEN_THOUSAND},
			want: FractionalPercent{Numerator: 50, Denominator: 10000, PPM: 5000},
		},
		{
			name: "FiftyPercentMillion",
			fp:   &v3typepb.FractionalPercent{Numerator: 500000, Denominator: v3typepb.FractionalPercent_MILLION},
			want: FractionalPercent{Numerator: 500000, Denominator: 1000000, PPM: 500000},
		},
		{
			// numerator*million overflows a uint32 once the numerator reaches
			// 4295 with the million denominator, which is only a 0.43%
			// fraction.
			name: "PointFourThreePercentMillion",
			fp:   &v3typepb.FractionalPercent{Numerator: 4295, Denominator: v3typepb.FractionalPercent_MILLION},
			want: FractionalPercent{Numerator: 4295, Denominator: 1000000, PPM: 4295},
		},
		{
			// 429497*10000 wraps a uint32 to 2704, so a fraction meant to be
			// above 100% would become ~0.27% if computed in uint32.
			name: "OverflowingNumeratorHundredCapped",
			fp:   &v3typepb.FractionalPercent{Numerator: 429497, Denominator: v3typepb.FractionalPercent_HUNDRED},
			want: FractionalPercent{Numerator: 429497, Denominator: 100, PPM: 1000000},
		},
		{
			name: "OverHundredPercentCapped",
			fp:   &v3typepb.FractionalPercent{Numerator: 150, Denominator: v3typepb.FractionalPercent_HUNDRED},
			want: FractionalPercent{Numerator: 150, Denominator: 100, PPM: 1000000},
		},
		{
			name:    "UnsupportedDenominator",
			fp:      &v3typepb.FractionalPercent{Numerator: 1, Denominator: v3typepb.FractionalPercent_DenominatorType(7)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFractionalPercent(tt.fp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewFractionalPercent(%v) returned err %v, wantErr %v", tt.fp, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("NewFractionalPercent(%v) = %+v, want %+v", tt.fp, got, tt.want)
			}
		})
	}
}
