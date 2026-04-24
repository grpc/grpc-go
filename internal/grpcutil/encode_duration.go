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
	duration time.Duration
	symbol   byte
}{
	{time.Nanosecond, 'n'},
	{time.Microsecond, 'u'},
	{time.Millisecond, 'm'},
	{time.Second, 'S'},
	{time.Minute, 'M'},
	{time.Hour, 'H'},
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
	value := (int64)(t / units[i].duration)
	// Round to larger unit as needed to satisfy value limit.
	// Note that the maximum value of Duration encodes properly in hours.
	for value > maxTimeoutValue && i+1 < len(units) {
		i++
		value = (int64)(t/units[i].duration + 1)
	}
	return fmt.Sprintf("%d%c", value, units[i].symbol)
}
