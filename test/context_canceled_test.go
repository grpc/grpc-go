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

// Binary wait_for_ready is an example for "wait for ready".
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

//TestRace tests.
func (s) TestContextCanceled(t *testing.T) {
	wait := make(chan struct{})
	ss := &stubServer{
		fullDuplexCall: func(stream testpb.TestService_FullDuplexCallServer) error {
			stream.SetTrailer(metadata.New(map[string]string{"a": "b"}))
			close(wait)
			return status.Error(codes.PermissionDenied, "perm denied")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	str, err := ss.client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", ss.client, err)
	}
	<-wait
	time.Sleep(time.Millisecond)
	cancel()
	_, err = str.Recv()
	if err == nil {
		t.Fatalf("non-nil error expected from Recv()")
	}
	trl := str.Trailer()
	if status.Code(err) == codes.PermissionDenied && trl["a"] == nil {
		t.Fatalf("<<a>> not in trailer")
	} else if status.Code(err) == codes.Canceled && trl["a"] != nil {
		t.Fatalf("<<a>> in trailer")
	}
}
