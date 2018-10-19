/*
 *
 * Copyright 2018 gRPC authors.
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

package healthcheck

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc/internal/backoff"
)

func TestNewClientHealthCheckBackoff(t *testing.T) {
	retried := false
	var fstTry time.Time
	var sndTry time.Time
	newStream := func() (interface{}, error) {
		callTime := time.Now()
		if retried {
			sndTry = callTime
			return nil, nil
		}
		fstTry = callTime
		retried = true
		return nil, errors.New("Backoff")

	}
	newClientHealthCheck(context.Background(), newStream, func(_ bool) {}, "test")
	actualDelta := sndTry.Sub(fstTry)
	bo := backoff.Exponential{MaxDelay: maxDelay}
	expectedDelta := bo.Backoff(0)

	if float64(actualDelta.Nanoseconds()) <= float64(expectedDelta)*.8 {
		t.Fatalf("Duration between two calls of newStream is %v (expected: %v)\n", actualDelta, expectedDelta)
	}
}
