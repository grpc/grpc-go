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
	"fmt"

	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

// FractionalPercent is the internal representation of the xDS FractionalPercent
// proto.
type FractionalPercent struct {
	Numerator   uint32
	Denominator uint32
	PPM         uint32 // Pre-computed and capped at 1,000,000 (100%).
}

// NewFractionalPercent converts the given FractionalPercent proto to its
// internal representation. A nil proto is treated as 0 out of 100. An error is
// returned for an unrecognized denominator.
func NewFractionalPercent(fp *v3typepb.FractionalPercent) (FractionalPercent, error) {
	if fp == nil {
		return FractionalPercent{Numerator: 0, Denominator: 100, PPM: 0}, nil
	}

	var den uint32
	switch fp.GetDenominator() {
	case v3typepb.FractionalPercent_HUNDRED:
		den = 100
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		den = 10000
	case v3typepb.FractionalPercent_MILLION:
		den = 1000000
	default:
		return FractionalPercent{}, fmt.Errorf("unsupported denominator: %v", fp.GetDenominator())
	}

	num := fp.GetNumerator()
	// The numerator comes from the control plane, so perform the
	// multiplication in uint64 to prevent overflowing a uint32, and cap the
	// result at 100%.
	ppm := uint64(num) * 1000000 / uint64(den)
	if ppm > 1000000 {
		ppm = 1000000
	}

	return FractionalPercent{
		Numerator:   num,
		Denominator: den,
		PPM:         uint32(ppm),
	}, nil
}
