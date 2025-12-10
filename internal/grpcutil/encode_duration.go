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
	"strconv"
	"time"
)

const maxTimeoutValue int64 = 99999999

type unit struct {
	duration time.Duration
	symbol   string
}

var units = []unit{
	{time.Hour, "H"},
	{time.Minute, "M"},
	{time.Second, "S"},
	{time.Millisecond, "m"},
	{time.Microsecond, "u"},
	{time.Nanosecond, "n"},
}

// div does integer division and round-up the result. Note that this is
// equivalent to (n+d-1)/d but has less chance to overflow.
func div(n, d time.Duration) int64 {
	q := int64(n / d)
	if n%d > 0 {
		q++
	}
	return q
}

// EncodeDuration encodes the duration to the format grpc-timeout header
// accepts.
//
// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md#requests
func EncodeDuration(t time.Duration) string {
	if t <= 0 {
		return "0S"
	}

	// Find the largest dividing unit.
	var i int
	for i = range units {
		if t%units[i].duration == 0 {
			break
		}
	}
	// Encode to maximum 8 digits.
	var value int64
	for {
		value = div(t, units[i].duration)
		if value <= maxTimeoutValue || i == 0 {
			break
		}
		i--
	}
	return strconv.FormatInt(value, 10) + units[i].symbol
}
