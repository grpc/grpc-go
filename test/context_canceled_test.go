/*
 *
 * Copyright 2019 gRPC authors.
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

package test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func (s) TestContextCanceled(t *testing.T) {
	ss := &stubServer{
		fullDuplexCall: func(stream testpb.TestService_FullDuplexCallServer) error {
			stream.SetTrailer(metadata.New(map[string]string{"a": "b"}))
			return status.Error(codes.PermissionDenied, "perm denied")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	// Runs 5 rounds of tests with the given delay and returns counts of status codes.
	// Fails in case of trailer/status code inconsistency.
	runTest := func(delay uint) (cntCanceled, cntPermDenied uint) {
		for i := 0; i < 5; i++ {
			ctx, cancel := context.WithCancel(context.Background())

			str, err := ss.client.FullDuplexCall(ctx)
			if err != nil {
				t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", ss.client, err)
			}
			// As this duration goes up chances of Recv returning Cancelled will decrease.
			time.Sleep(time.Duration(delay) * time.Microsecond)
			cancel()
			_, err = str.Recv()
			if err == nil {
				t.Fatalf("non-nil error expected from Recv()")
			}
			code := status.Code(err)
			_, ok := str.Trailer()["a"]
			if code == codes.PermissionDenied {
				if !ok {
					t.Fatalf(`status err: %v; wanted key "a" in trailer but didn't get it`, err)
				}
				cntPermDenied++
			} else if code == codes.Canceled {
				if ok {
					t.Fatalf(`status err: %v; didn't want key "a" in trailer but got it`, err)
				}
				cntCanceled++
			}
		}
		return cntCanceled, cntPermDenied
	}

	const maxDelay uint = 250000
	var delay uint = 1
	// Doubles the delay until the upper bound is found.
	for delay < maxDelay {
		cntCanceled, cntPermDenied := runTest(delay)
		if cntCanceled == 5 {
			delay *= 2
		} else if cntPermDenied == 5 {
			break
		} else {
			return
		}
	}

	// Fails if the upper bound is greater than maxDelay
	if delay >= maxDelay {
		t.Fatalf(`couldn't find the delay that causes canceled/perm denied race.`)
	}

	lower, upper := delay/2, delay
	// Binary search for the delay that causes canceled/perm denied race.
	for lower <= upper {
		delay = lower + (upper-lower)/2
		cntCanceled, cntPermDenied := runTest(delay)
		if cntCanceled == 5 {
			lower = delay + 1
		} else if cntPermDenied == 5 {
			upper = delay - 1
		} else {
			return
		}
	}

	// Fails if the delay that causes canceled/perm denied race not found.
	t.Fatalf(`couldn't find the delay that causes canceled/perm denied race.`)
}
