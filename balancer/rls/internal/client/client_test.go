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

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/rls/internal/testutils/fakeserver"
	"google.golang.org/grpc/codes"

	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

type fakeThrottler struct {
	shouldThrottle              bool
	gotBackendResponseThrottled bool
}

func (f *fakeThrottler) ShouldThrottle() bool {
	return f.shouldThrottle
}

func (f *fakeThrottler) RegisterBackendResponse(throttled bool) {
	f.gotBackendResponseThrottled = throttled
}

func setup(t *testing.T) (*fakeserver.Server, *grpc.ClientConn, func()) {
	t.Helper()

	server, sCleanup, err := fakeserver.Start()
	if err != nil {
		t.Fatalf("Failed to start fake RLS server: %v", err)
	}

	cc, cCleanup, err := server.ClientConn()
	if err != nil {
		t.Fatalf("Failed to get a ClientConn to the RLS server: %v", err)
	}

	return server, cc, func() {
		sCleanup()
		cCleanup()
	}
}

// TestLookupThrottled verifies that the Lookup API fails when the throttler
// asks the request to be throttled.
func TestLookupThrottled(t *testing.T) {
	_, cc, cleanup := setup(t)
	defer cleanup()

	rlsClient := New(cc, &fakeThrottler{shouldThrottle: true})
	_, err := rlsClient.Lookup(context.Background(), nil)
	if err != ErrRequestThrottled {
		t.Errorf("rlsClient.Lookup() = %v, want %v", err, ErrRequestThrottled)
	}
}

// TestLookupFailure verifies the case where the RLS server returns an error.
func TestLookupFailure(t *testing.T) {
	server, cc, cleanup := setup(t)
	defer cleanup()

	throttler := &fakeThrottler{shouldThrottle: false}
	rlsClient := New(cc, throttler)

	// We setup the fake server to return an error.
	server.ResponseChan <- fakeserver.Response{Err: errors.New("rls failure")}
	gotResult, err := rlsClient.Lookup(context.Background(), &LookupArgs{})
	if err == nil || gotResult != nil {
		t.Fatalf("rlsClient.Lookup() = (%v, %v) succeeded, should have failed", gotResult, err)
	}
	if !throttler.gotBackendResponseThrottled {
		t.Fatal("Lookup reported successful backend response to throttler, after request failed")
	}
}

// TestLookupCanceled tests the case where the Lookup API is cancelled, and
// verifies that the appropriate error is returned.
func TestLookupCanceled(t *testing.T) {
	_, cc, cleanup := setup(t)
	defer cleanup()

	rlsClient := New(cc, &fakeThrottler{shouldThrottle: false})
	// Give the Lookup RPC a big deadline, but don't setup the fake server to
	// return anything. So the Lookup call will block and we will cancel the
	// context in parallel, causing the Lookup call to return with a status
	// code of Canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	enterCh := make(chan struct{})
	errCh := make(chan error)
	go func() {
		close(enterCh)
		_, err := rlsClient.Lookup(ctx, &LookupArgs{})
		errCh <- err
	}()

	<-enterCh
	// Canceling the context here will unblock the Lookup call.
	cancel()

	if err := <-errCh; grpc.Code(err) != codes.Canceled {
		t.Fatalf("rlsClient.Lookup() returned error: %v, want %v", err, codes.Canceled)
	}
}

// TestLookupDeadlineExceeded tests the case where the RPC deadline associated
// with the Lookup API expires.
func TestLookupDeadlineExceeded(t *testing.T) {
	_, cc, cleanup := setup(t)
	defer cleanup()

	rlsClient := New(cc, &fakeThrottler{shouldThrottle: false})
	// Give the Lookup RPC a small deadline, but don't setup the fake server to
	// return anything. So the Lookup call will block and eventuall expire.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := rlsClient.Lookup(ctx, &LookupArgs{}); grpc.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("rlsClient.Lookup() returned error: %v, want %v", err, codes.DeadlineExceeded)
	}
}

// TestLookupSuccess verifies the successful Lookup API case.
func TestLookupSuccess(t *testing.T) {
	server, cc, cleanup := setup(t)
	defer cleanup()

	throttler := &fakeThrottler{shouldThrottle: false}
	rlsClient := New(cc, throttler)

	const (
		defaultTestTimeout = 1 * time.Second
		rlsReqTarget       = "firestore.googleapis.com"
		rlsReqPath         = "/service/method"
		rlsRespTarget      = "us_east_1.firestore.googleapis.com"
		rlsHeaderData      = "headerData"
	)

	// We setup the fake server to return this response when it receives a
	// request.
	server.ResponseChan <- fakeserver.Response{
		Resp: &rlspb.RouteLookupResponse{
			Target:     rlsRespTarget,
			HeaderData: rlsHeaderData,
		},
	}
	rlsReqKeyMap := map[string]string{
		"k1": "v1",
		"k2": "v2",
	}
	wantLookupRequest := &rlspb.RouteLookupRequest{
		Server:     rlsReqTarget,
		Path:       rlsReqPath,
		TargetType: "grpc",
		KeyMap:     rlsReqKeyMap,
	}
	wantResp := &LookupResult{
		Target:     rlsRespTarget,
		HeaderData: rlsHeaderData,
	}

	// Make the actual Lookup API call, and make sure that the fake server
	// received the expected RouteLookupRequest proto.
	gotResult, err := rlsClient.Lookup(context.Background(), &LookupArgs{
		Target: rlsReqTarget,
		Path:   rlsReqPath,
		KeyMap: rlsReqKeyMap,
	})
	if err != nil {
		t.Fatalf("rlsClient.Lookup() failed: %v", err)
	}

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case gotLookupRequest := <-server.RequestChan:
		timer.Stop()
		if diff := cmp.Diff(wantLookupRequest, gotLookupRequest, cmp.Comparer(proto.Equal)); diff != "" {
			t.Fatalf("RouteLookupRequest diff (-want, +got):\n%s", diff)
		}
	case <-timer.C:
		t.Fatalf("Timed out wile waiting for a RouteLookupRequest")
	}

	if !cmp.Equal(gotResult, wantResp) {
		t.Fatalf("rlsClient.Lookup() = %v, want %v", gotResult, wantResp)
	}
	if throttler.gotBackendResponseThrottled {
		t.Fatal("Lookup reported failed backend response to throttler, after request succeeded")
	}
}
