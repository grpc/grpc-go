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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func (s) TestContextCanceled(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			stream.SetTrailer(metadata.New(map[string]string{"a": "b"}))
			return status.Error(codes.PermissionDenied, "perm denied")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	// Runs 10 rounds of tests with the given delay and returns counts of status codes.
	// Fails in case of trailer/status code inconsistency.
	const cntRetry uint = 10
	runTest := func(delay time.Duration) (cntCanceled, cntPermDenied uint) {
		for i := uint(0); i < cntRetry; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), delay)
			defer cancel()

			str, err := ss.Client.FullDuplexCall(ctx)
			if err != nil {
				continue
			}

			_, err = str.Recv()
			if err == nil {
				t.Fatalf("non-nil error expected from Recv()")
			}

			_, trlOk := str.Trailer()["a"]
			switch status.Code(err) {
			case codes.PermissionDenied:
				if !trlOk {
					t.Fatalf(`status err: %v; wanted key "a" in trailer but didn't get it`, err)
				}
				cntPermDenied++
			case codes.DeadlineExceeded:
				if trlOk {
					t.Fatalf(`status err: %v; didn't want key "a" in trailer but got it`, err)
				}
				cntCanceled++
			default:
				t.Fatalf(`unexpected status err: %v`, err)
			}
		}
		return cntCanceled, cntPermDenied
	}

	// Tries to find the delay that causes canceled/perm denied race.
	canceledOk, permDeniedOk := false, false
	for lower, upper := time.Duration(0), 2*time.Millisecond; lower <= upper; {
		delay := lower + (upper-lower)/2
		cntCanceled, cntPermDenied := runTest(delay)
		if cntPermDenied > 0 && cntCanceled > 0 {
			// Delay that causes the race is found.
			return
		}

		// Set OK flags.
		if cntCanceled > 0 {
			canceledOk = true
		}
		if cntPermDenied > 0 {
			permDeniedOk = true
		}

		if cntPermDenied == 0 {
			// No perm denied, increase the delay.
			lower += (upper-lower)/10 + 1
		} else {
			// All perm denied, decrease the delay.
			upper -= (upper-lower)/10 + 1
		}
	}

	if !canceledOk || !permDeniedOk {
		t.Fatalf(`couldn't find the delay that causes canceled/perm denied race.`)
	}
}

// To make sure that canceling a stream with compression enabled won't result in
// internal error, compressed flag set with identity or empty encoding.
//
// The root cause is a select race on stream headerChan and ctx. Stream gets
// whether compression is enabled and the compression type from two separate
// functions, both include select with context. If the `case non-ctx:` wins the
// first one, but `case ctx.Done()` wins the second one, the compression info
// will be inconsistent, and it causes internal error.
func (s) TestCancelWhileRecvingWithCompression(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: nil,
				}); err != nil {
					return err
				}
			}
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		s, err := ss.Client.FullDuplexCall(ctx, grpc.UseCompressor(gzip.Name))
		if err != nil {
			t.Fatalf("failed to start bidi streaming RPC: %v", err)
		}
		// Cancel the stream while receiving to trigger the internal error.
		time.AfterFunc(time.Millisecond, cancel)
		for {
			_, err := s.Recv()
			if err != nil {
				if status.Code(err) != codes.Canceled {
					t.Fatalf("recv failed with %v, want Canceled", err)
				}
				break
			}
		}
	}
	if err := ss.CC.Close(); err != nil {
		t.Fatalf("Close failed with %v, want nil", err)
	}
}

// Test verifies that an in-flight Unary RPC fails promptly when the ClientConn
// is closed (canceling ClientConn context).
func (s) TestUnary_ClientConnContextExpires(t *testing.T) {
	rpcStarted := make(chan struct{})
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			close(rpcStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	errChan := make(chan error, 1)
	go func() {
		_, err := ss.Client.EmptyCall(ctx, &testpb.Empty{})
		errChan <- err
	}()

	select {
	case <-rpcStarted:
	case <-time.After(defaultTestTimeout):
		t.Fatal("timed out waiting for RPC to start on server")
	}

	// Close ClientConn while Unary RPC is in-flight.
	ss.CC.Close()

	select {
	case err := <-errChan:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("EmptyCall returned error: %v (code: %v), want Canceled", err, status.Code(err))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Unary RPC did not return promptly after ClientConn was closed")
	}
}

// Test verifies that an in-flight Streaming RPC fails promptly when the
// ClientConn is closed (canceling ClientConn context).
func (s) TestStreaming_ClientConnContextExpires(t *testing.T) {
	rpcStarted := make(chan struct{})
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			close(rpcStarted)
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}

	// Send an initial message so the stream is active on the server.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case <-rpcStarted:
	case <-time.After(defaultTestTimeout):
		t.Fatal("timed out waiting for stream to start on server")
	}

	errChan := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		errChan <- err
	}()

	// Close ClientConn while Streaming RPC is waiting in Recv.
	ss.CC.Close()

	select {
	case err := <-errChan:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("Recv returned error: %v (code: %v), want Canceled", err, status.Code(err))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Streaming RPC did not return promptly after ClientConn was closed")
	}
}
