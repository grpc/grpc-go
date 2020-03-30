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

package rls

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/balancer/rls/internal/testutils/fakeserver"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultDialTarget  = "dummy"
	defaultRPCTimeout  = 5 * time.Second
	defaultTestTimeout = 1 * time.Second
)

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

// TestLookupFailure verifies the case where the RLS server returns an error.
func TestLookupFailure(t *testing.T) {
	server, cc, cleanup := setup(t)
	defer cleanup()

	// We setup the fake server to return an error.
	server.ResponseChan <- fakeserver.Response{Err: errors.New("rls failure")}

	rlsClient := newRLSClient(cc, defaultDialTarget, defaultRPCTimeout)

	errCh := make(chan error)
	rlsClient.lookup("", nil, func(target, headerData string, err error) {
		if err == nil {
			errCh <- errors.New("rlsClient.lookup() succeeded, should have failed")
			return
		}
		if target != "" || headerData != "" {
			errCh <- fmt.Errorf("rlsClient.lookup() = (%s, %s), should be empty strings", target, headerData)
			return
		}
		errCh <- nil
	})

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting a routeLookup callback")
	case err := <-errCh:
		timer.Stop()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestLookupDeadlineExceeded tests the case where the RPC deadline associated
// with the lookup expires.
func TestLookupDeadlineExceeded(t *testing.T) {
	_, cc, cleanup := setup(t)
	defer cleanup()

	// Give the Lookup RPC a small deadline, but don't setup the fake server to
	// return anything. So the Lookup call will block and eventuall expire.
	rlsClient := newRLSClient(cc, defaultDialTarget, 100*time.Millisecond)

	errCh := make(chan error)
	rlsClient.lookup("", nil, func(target, headerData string, err error) {
		if st, ok := status.FromError(err); !ok || st.Code() != codes.DeadlineExceeded {
			errCh <- fmt.Errorf("rlsClient.lookup() returned error: %v, want %v", err, codes.DeadlineExceeded)
			return
		}
		errCh <- nil
	})

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting a routeLookup callback")
	case err := <-errCh:
		timer.Stop()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestLookupSuccess verifies the successful Lookup API case.
func TestLookupSuccess(t *testing.T) {
	server, cc, cleanup := setup(t)
	defer cleanup()

	const (
		rlsReqPath    = "/service/method"
		rlsRespTarget = "us_east_1.firestore.googleapis.com"
		rlsHeaderData = "headerData"
	)

	rlsReqKeyMap := map[string]string{
		"k1": "v1",
		"k2": "v2",
	}
	wantLookupRequest := &rlspb.RouteLookupRequest{
		Server:     defaultDialTarget,
		Path:       rlsReqPath,
		TargetType: "grpc",
		KeyMap:     rlsReqKeyMap,
	}

	rlsClient := newRLSClient(cc, defaultDialTarget, defaultRPCTimeout)

	errCh := make(chan error)
	rlsClient.lookup(rlsReqPath, rlsReqKeyMap, func(t, hd string, err error) {
		if err != nil {
			errCh <- fmt.Errorf("rlsClient.Lookup() failed: %v", err)
			return
		}
		if t != rlsRespTarget || hd != rlsHeaderData {
			errCh <- fmt.Errorf("rlsClient.lookup() = (%s, %s), want (%s, %s)", t, hd, rlsRespTarget, rlsHeaderData)
			return
		}
		errCh <- nil
	})

	// Make sure that the fake server received the expected RouteLookupRequest
	// proto.
	timer := time.NewTimer(defaultTestTimeout)
	select {
	case gotLookupRequest := <-server.RequestChan:
		if !timer.Stop() {
			<-timer.C
		}
		if diff := cmp.Diff(wantLookupRequest, gotLookupRequest, cmp.Comparer(proto.Equal)); diff != "" {
			t.Fatalf("RouteLookupRequest diff (-want, +got):\n%s", diff)
		}
	case <-timer.C:
		t.Fatalf("Timed out wile waiting for a RouteLookupRequest")
	}

	// We setup the fake server to return this response when it receives a
	// request.
	server.ResponseChan <- fakeserver.Response{
		Resp: &rlspb.RouteLookupResponse{
			Target:     rlsRespTarget,
			HeaderData: rlsHeaderData,
		},
	}

	timer = time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting a routeLookup callback")
	case err := <-errCh:
		timer.Stop()
		if err != nil {
			t.Fatal(err)
		}
	}
}
