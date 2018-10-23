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

func TestClientHealthCheckBackoff(t *testing.T) {
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
	clientHealthCheck(context.Background(), newStream, func(_ bool) {}, "test")
	actualDelta := sndTry.Sub(fstTry)
	bo := backoff.Exponential{MaxDelay: maxDelay}
	expectedDelta := bo.Backoff(0)

	if float64(actualDelta.Nanoseconds()) <= float64(expectedDelta)*.8 {
		t.Fatalf("Duration between two calls of newStream is %v (expected: %v)\n", actualDelta, expectedDelta)
	}
}

func TestClientHealthCheckMultipleBackoffs(t *testing.T) {
	var backoffDurations []time.Duration
	newStream := func() (interface{}, error) {
		if len(backoffDurations) < 5 {
			return nil, errors.New("backoff")
		}
		return nil, nil
	}

	oldBackoffFunc := backoffFunc
	backoffFunc = func(ctx context.Context, d time.Duration) bool {
		backoffDurations = append(backoffDurations, d)
		return true
	}
	clientHealthCheck(context.Background(), newStream, func(_ bool) {}, "test")
	backoffFunc = oldBackoffFunc

	bo := backoff.Exponential{MaxDelay: maxDelay}
	for i, d := range backoffDurations {
		actual := float64(d.Nanoseconds())
		expected := float64(bo.Backoff(i).Nanoseconds())
		lower := .5 * expected
		upper := 1.5 * expected
		if lower > actual || actual > upper {
			t.Fatalf("Duration for retry #%v is %v (expected: %v)\n", i, actual, expected)
		}
	}
}
