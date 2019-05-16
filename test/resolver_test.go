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
 */

package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const resolverErrorMsg = "resolver-failed-with-this-error"

var errResolver = errors.New(resolverErrorMsg)

func (s) TestResolverErrorNonWaitforready(t *testing.T) {
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	r.InitialState(resolver.State{
		Err: errResolver,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	cc, err := grpc.DialContext(ctx, r.Scheme()+":///not-important", grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Dial failed with %v", err)
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	cctx, ccancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer ccancel()
	// TODO: this should fail fast, but now it fails after 1 second (timeout).
	if _, err = tc.EmptyCall(cctx, &testpb.Empty{}); !strings.Contains(err.Error(), resolverErrorMsg) {
		t.Fatalf("got err: %v, want error containing %q", err, resolverErrorMsg)
	}
}

func (s) TestResolverErrorWaitforready(t *testing.T) {
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	r.InitialState(resolver.State{
		Err: errResolver,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	cc, err := grpc.DialContext(ctx, r.Scheme()+":///not-important", grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Dial failed with %v", err)
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	cctx, ccancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer ccancel()
	if _, err = tc.EmptyCall(cctx, &testpb.Empty{}, grpc.WaitForReady(true)); !strings.Contains(err.Error(), resolverErrorMsg) {
		t.Fatalf("got err: %v, want error containing %q", err, resolverErrorMsg)
	}
}
