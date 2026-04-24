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
	"testing"
	"time"
)

func TestEncodeDuration(t *testing.T) {
	for _, test := range []struct{ in, out string }{
		// Exact encoding
		{"0ns", "0S"},
		{"1ns", "1n"},
		{"99999999ns", "99999999n"},
		{"1us", "1u"},
		{"99999999us", "99999999u"},
		{"1ms", "1m"},
		{"99999999ms", "99999999m"},
		{"1s", "1S"},
		{"99999999s", "99999999S"},
		{"1m", "1M"},
		{"99999999m", "99999999M"},
		{"1h", "1H"},

		// Rounding
		{"100000001ns", "100001u"},
		{"100000001us", "100001m"},
		{"100000001ms", "100001S"},
		{"100000000s", "1666667M"},
		{"100000000m", "1666667H"},

		// Boundary conditions
		{"-1ns", "0S"},
		{"9223372036854775807ns", "2562048H"},
	} {
		d, err := time.ParseDuration(test.in)
		if err != nil {
			t.Fatalf("failed to parse duration string %s: %v", test.in, err)
		}
		out := EncodeDuration(d)
		if out != test.out {
			t.Fatalf("EncodeDuration(%s) = %s, want %s", test.in, out, test.out)
		}
	}
}
