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

	var i, cntCanceled uint
	cntPermDenied := func() uint {
		return i - cntCanceled
	}
	for i, cntCanceled = 0, 0; i < 500 && (cntCanceled < 5 || cntPermDenied() < 5); i++ {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		str, err := ss.client.FullDuplexCall(ctx)
		if err != nil {
			t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", ss.client, err)
		}
		// As this duration goes up chances of Recv returning Cancelled will decrease.
		time.Sleep(time.Duration(i) * time.Microsecond)
		cancel()
		_, err = str.Recv()
		if err == nil {
			t.Fatalf("non-nil error expected from Recv()")
		}
		code := status.Code(err)
		if code == codes.Canceled {
			cntCanceled++
		}
		_, ok := str.Trailer()["a"]
		if code == codes.PermissionDenied && !ok {
			t.Fatalf(`status err: %v; wanted key "a" in trailer but didn't get it`, err)
		}
		if code == codes.Canceled && ok {
			t.Fatalf(`status err: %v; didn't want key "a" in trailer but got it`, err)
		}
	}
	if cntCanceled < 5 || cntPermDenied() < 5 {
		t.Fatalf("got Canceled status %v times and PermissionDenied status %v times but wanted both of them at least 5 times", cntCanceled, cntPermDenied())
	}
}
