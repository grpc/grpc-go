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
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type cancelBeforeHandlerStatsHandler struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	method string
}

func (h *cancelBeforeHandlerStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *cancelBeforeHandlerStatsHandler) HandleRPC(ctx context.Context, stat stats.RPCStats) {
	switch stat := stat.(type) {
	case *stats.InHeader:
		if stat.FullMethod != h.method {
			return
		}
		h.cancel()
		<-ctx.Done()
		// Stream cancellation marks the stream done immediately after canceling
		// the context. Wait for that state transition before allowing request
		// processing to continue.
		time.Sleep(10 * time.Millisecond)
	case *stats.End:
		h.err = stat.Error
		close(h.done)
	}
}

func (*cancelBeforeHandlerStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (*cancelBeforeHandlerStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

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

func (s) TestCancelBeforeHandlerWithCompression(t *testing.T) {
	for _, test := range []struct {
		name      string
		method    string
		streaming bool
	}{
		{
			name:   "unary",
			method: testgrpc.TestService_UnaryCall_FullMethodName,
		},
		{
			name:      "streaming",
			method:    testgrpc.TestService_FullDuplexCall_FullMethodName,
			streaming: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			testCtx, testCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer testCancel()
			callCtx, callCancel := context.WithCancel(testCtx)
			defer callCancel()

			statsHandler := &cancelBeforeHandlerStatsHandler{
				cancel: callCancel,
				done:   make(chan struct{}),
				method: test.method,
			}
			handlerCalled := false
			ss := &stubserver.StubServer{
				UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
					handlerCalled = true
					return &testpb.SimpleResponse{}, nil
				},
				FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
					handlerCalled = true
					return nil
				},
			}
			if err := ss.Start([]grpc.ServerOption{grpc.StatsHandler(statsHandler)}); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			var err error
			if test.streaming {
				stream, streamErr := ss.Client.FullDuplexCall(callCtx, grpc.UseCompressor(gzip.Name))
				if streamErr == nil {
					_, streamErr = stream.Recv()
				}
				err = streamErr
			} else {
				_, err = ss.Client.UnaryCall(callCtx, &testpb.SimpleRequest{
					Payload: &testpb.Payload{Body: []byte("payload")},
				}, grpc.UseCompressor(gzip.Name))
			}
			if got, want := status.Code(err), codes.Canceled; got != want {
				t.Fatalf("RPC returned code %v, want %v", got, want)
			}

			select {
			case <-statsHandler.done:
				if got, want := status.Code(statsHandler.err), codes.Canceled; got != want {
					t.Fatalf("stats.End error code = %v, want %v; error: %v", got, want, statsHandler.err)
				}
			case <-testCtx.Done():
				t.Fatal("Timeout waiting for server stats.End")
			}

			if handlerCalled {
				t.Error("RPC handler unexpectedly called")
			}
		})
	}
}
