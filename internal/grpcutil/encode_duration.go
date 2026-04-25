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
	"time"
)

const maxTimeoutValue = 99_999_999

var units = []struct {
	symbol   byte
	duration time.Duration
}{
	{'n', time.Nanosecond},
	{'u', time.Microsecond},
	{'m', time.Millisecond},
	{'S', time.Second},
	{'M', time.Minute},
	{'H', time.Hour},
}

// div does integer division and round-up the result. Note that this is
// equivalent to (d+r-1)/r but has less chance to overflow.
func div(d, r time.Duration) int64 {
	q := (int64)(d / r)
	if d%r > 0 {
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
	for i = len(units) - 1; i > 0; i-- {
		if t%units[i].duration == 0 {
			break
		}
	}
	value := div(t, units[i].duration)
	// Round to larger unit as needed to satisfy value limit.
	// Note that the maximum value of Duration encodes properly in hours.
	for value > maxTimeoutValue && i+1 < len(units) {
		i++
		value = div(t, units[i].duration)
	}
	return fmt.Sprintf("%d%c", value, units[i].symbol)
}
