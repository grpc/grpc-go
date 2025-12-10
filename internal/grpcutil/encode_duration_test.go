/*
 *
 * Copyright 2020 gRPC authors.
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

package grpcutil

import (
	"fmt"
	"math"
	"testing"
	"time"
)

type testCase struct{ in, out string }

func test(t *testing.T, tc *testCase) {
	in, err := time.ParseDuration(tc.in)
	if err != nil {
		t.Fatalf("failed to parse duration string %s: %v", tc.in, err)
	}
	out := EncodeDuration(in)
	if out != tc.out {
		t.Fatalf("timeoutEncode(%s) = %s, want %s", tc.in, out, tc.out)
	}
}

func TestZero(t *testing.T) {
	for _, tc := range []testCase{
		{"-1ns", "0S"},
		{"0s", "0S"},
	} {
		test(t, &tc)
	}
}

func TestConciseness(t *testing.T) {
	for _, tc := range []testCase{
		{"1ns", "1n"},
		{"1us", "1u"},
		{"1ms", "1m"},
		{"1s", "1S"},
		{"1m", "1M"},
		{"1h", "1H"},
	} {
		test(t, &tc)
	}
}

// Encode up to 8 digits of precision.
func TestPrecision(t *testing.T) {
	for _, tc := range []testCase{
		{"10000001ns", "10000001n"},
		{"10000001us", "10000001u"},
		{"10000001ms", "10000001m"},
		{"10000001s", "10000001S"},
		{"10000001m", "10000001M"},
	} {
		test(t, &tc)
	}
}

// Round up to larger unit if precision exceeds 8 digits.
func TestRounding(t *testing.T) {
	for _, tc := range []testCase{
		{"100000001ns", "100001u"},
		{"100000001us", "100001m"},
		{"100000001ms", "100001S"},
		{"100000001s", "1666667M"},
	} {
		test(t, &tc)
	}
}

func TestOverflow(t *testing.T) {
	tc := testCase{fmt.Sprintf("%dns", math.MaxInt64), "2562048H"}
	test(t, &tc)
}
