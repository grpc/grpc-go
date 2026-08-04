/*
 *
 * Copyright 2023 gRPC authors.
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

package grpc_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestStream_Header_TrailersOnly(t *testing.T) {
	ss := stubserver.StubServer{
		FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
			return status.Errorf(codes.NotFound, "a test error")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	s, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatal("Error staring call", err)
	}
	if md, err := s.Header(); md != nil || err != nil {
		t.Fatalf("s.Header() = %v, %v; want nil, nil", md, err)
	}
	if _, err := s.Recv(); status.Code(err) != codes.NotFound {
		t.Fatalf("s.Recv() = _, %v; want _, err.Code()=codes.NotFound", err)
	}
}

// TestUnaryClient_ServerStreamingMismatch ensures that the client's
// non-streaming RecvMsg() logic correctly handles various error scenarios
// from the server.
//
// The Client initiates a Unary RPC (Invoke), forcing it to use the
// non-server-streaming `recvMsg` code path (where the bug was).
// The Server handles it as a Streaming RPC (FullDuplexCall), allowing us to
// send arbitrary sequences of messages and errors.
func (s) TestUnaryClient_ServerStreamingMismatch(t *testing.T) {
	tests := []struct {
		name              string
		fullDuplexCallF   func(testgrpc.TestService_FullDuplexCallServer) error
		wantErrorContains string
		wantCode          codes.Code
		clientCallOptions []grpc.CallOption
	}{
		{
			name: "server_sends_error_after_message",
			fullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				if err := stream.Send(&testpb.StreamingOutputCallResponse{}); err != nil {
					return err
				}
				return status.Error(codes.Internal, "server error after message")
			},
			wantErrorContains: "server error after message",
			wantCode:          codes.Internal,
		},
		{
			name: "server_sends_second_message_exceeding_limit",
			fullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: make([]byte, 1)},
				}); err != nil {
					return err
				}
				return stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: make([]byte, 10)},
				})
			},
			clientCallOptions: []grpc.CallOption{grpc.MaxCallRecvMsgSize(5)},
			wantErrorContains: "received message larger than max",
			wantCode:          codes.ResourceExhausted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ss := &stubserver.StubServer{
				FullDuplexCallF: test.fullDuplexCallF,
			}
			if err := ss.Start(nil); err != nil {
				t.Fatal("Error starting server:", err)
			}
			defer ss.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Invoke the streaming RPC method as a Unary RPC. This forces the client
			// to use the non-streaming RecvMsg path, while the server handles it as
			// a stream (allowing it to send messages and errors in ways a standard
			// Unary server cannot).
			err := ss.CC.Invoke(ctx, "/grpc.testing.TestService/FullDuplexCall", &testpb.StreamingOutputCallRequest{}, &testpb.StreamingOutputCallResponse{}, test.clientCallOptions...)
			if err == nil {
				t.Fatal("Client.Invoke returned nil, want error")
			}
			if status.Code(err) != test.wantCode {
				t.Errorf("Unexpected error code: got %v, want %v", status.Code(err), test.wantCode)
			}
			if !strings.Contains(err.Error(), test.wantErrorContains) {
				t.Errorf("Unexpected error message: got %v, want %v", err.Error(), test.wantErrorContains)
			}
		})
	}
}

// interceptorStream wraps a ClientStream to record invocations of RecvMsg and
// CloseSend hooks across downstream interceptors.
type interceptorStream struct {
	grpc.ClientStream
	recvMsgCount int
	closeSend    bool
}

func (s *interceptorStream) RecvMsg(m any) error {
	s.recvMsgCount++
	return s.ClientStream.RecvMsg(m)
}

func (s *interceptorStream) CloseSend() error {
	s.closeSend = true
	return s.ClientStream.CloseSend()
}

// TestDefaultStreamInterceptor verifies that defaultStreamInterceptor
// automatically triggers CloseSend on non-client-streaming RPCs right after
// SendMsg, and calls RecvMsg a second time on non-server-streaming RPCs to
// consume trailers and io.EOF.
func (s) TestDefaultStreamInterceptor(t *testing.T) {
	var iStream *interceptorStream
	// Define a client-side stream interceptor that wraps the ClientStream to
	// monitor hook invocations across RPC calls.
	clientInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		cs, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		iStream = &interceptorStream{ClientStream: cs}
		return iStream, nil
	}

	// Setup a service implementing both a client-streaming RPC and a
	// server-streaming RPC.
	ss := &stubserver.StubServer{
		StreamingOutputCallF: func(_ *testpb.StreamingOutputCallRequest, stream testgrpc.TestService_StreamingOutputCallServer) error {
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
		StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
			for {
				if _, err := stream.Recv(); err != nil {
					if err == io.EOF {
						return stream.SendAndClose(&testpb.StreamingInputCallResponse{})
					}
					return err
				}
			}
		},
	}
	if err := ss.Start(nil, grpc.WithStreamInterceptor(clientInt)); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make a client-streaming RPC. When CloseAndRecv invokes RecvMsg once to get
	// the single reply message on a non-server-streaming RPC,
	// defaultStreamInterceptor automatically calls a second RecvMsg on the
	// underlying client stream to consume io.EOF and receive trailers.
	stream, err := ss.Client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatal("Error calling StreamingInputCall:", err)
	}
	if err := stream.Send(&testpb.StreamingInputCallRequest{}); err != nil {
		t.Fatal("Error sending request:", err)
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatal("Error running CloseAndRecv:", err)
	}
	if iStream.recvMsgCount != 2 {
		t.Fatalf("StreamingInputCall RecvMsg was called %v times, want 2 times", iStream.recvMsgCount)
	}

	// Make a server-streaming RPC. Since StreamingOutputCall is not
	// client-streaming, defaultStreamInterceptor immediately invokes CloseSend
	// right after sending the request message to signal downstream interceptors.
	if _, err := ss.Client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatal("Error calling StreamingOutputCall:", err)
	}
	if !iStream.closeSend {
		t.Fatal("CloseSend not called after SendMsg on non-client-streaming RPC")
	}
}
