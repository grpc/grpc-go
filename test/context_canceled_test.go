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
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
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

// TestSendCompressorSetupDuringStreamClose verifies the status reported when
// response compressor setup races with the end of a compressed RPC.
func (s) TestSendCompressorSetupDuringStreamClose(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(context.Context, testgrpc.TestServiceClient) error
	}{
		{
			name: "unary",
			call: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.UseCompressor(gzip.Name))
				return err
			},
		},
		{
			name: "stream",
			call: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				stream, err := client.FullDuplexCall(ctx, grpc.UseCompressor(gzip.Name))
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
					return err
				}
				_, err = stream.Recv()
				return err
			},
		},
	} {
		for _, setupTest := range []struct {
			name          string
			detachContext bool
		}{
			{
				name: "canceled_before_setup",
			},
			{
				name:          "canceled_before_setup_with_detached_context",
				detachContext: true,
			},
		} {
			t.Run(test.name+"/"+setupTest.name, func(t *testing.T) {
				headerSeen := make(chan struct{})
				serverDone := make(chan error, 1)
				handlerCalled := make(chan struct{}, 1)
				var transportCtx context.Context
				statsHandler := &testutils.StubStatsHandler{
					TagRPCF: func(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
						transportCtx = ctx
						if setupTest.detachContext {
							return context.WithoutCancel(ctx)
						}
						return ctx
					},
					HandleRPCF: func(_ context.Context, stat stats.RPCStats) {
						switch stat := stat.(type) {
						case *stats.InHeader:
							// Pause before send compressor setup until the stream is done.
							close(headerSeen)
							<-transportCtx.Done()
						case *stats.End:
							// Capture the server-side status observed by telemetry.
							serverDone <- stat.Error
						}
					},
				}

				ss := &stubserver.StubServer{
					EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
						handlerCalled <- struct{}{}
						return &testpb.Empty{}, nil
					},
					FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
						handlerCalled <- struct{}{}
						return nil
					},
				}
				if err := ss.Start([]grpc.ServerOption{grpc.StatsHandler(statsHandler)}); err != nil {
					t.Fatalf("Error starting endpoint server: %v", err)
				}
				defer ss.Stop()

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				callDone := make(chan error, 1)
				go func() {
					callDone <- test.call(ctx, ss.Client)
				}()

				select {
				case <-headerSeen:
				case <-time.After(defaultTestTimeout):
					t.Fatal("Timed out waiting for server to receive request headers")
				}
				cancel()

				select {
				case <-callDone:
				case <-time.After(defaultTestTimeout):
					t.Fatal("Timed out waiting for RPC to finish")
				}

				select {
				case err := <-serverDone:
					if got, want := status.Code(err), codes.Canceled; got != want {
						t.Fatalf("Server stats ended with code %v, want %v: %v", got, want, err)
					}
				case <-time.After(defaultTestTimeout):
					t.Fatal("Timed out waiting for server stats")
				}

				select {
				case <-handlerCalled:
					t.Fatal("RPC handler was called after the request was canceled")
				default:
				}
			})
		}
	}
}
