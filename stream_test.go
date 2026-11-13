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
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
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
	recvMsgCount   int
	closeSendCount int
}

func (s *interceptorStream) RecvMsg(m any) error {
	s.recvMsgCount++
	return s.ClientStream.RecvMsg(m)
}

func (s *interceptorStream) CloseSend() error {
	s.closeSendCount++
	return s.ClientStream.CloseSend()
}

// TestDefaultStreamInterceptor verifies that defaultStreamInterceptor's
// behavior of automatically triggering CloseSend on non-client-streaming RPCs
// right after SendMsg, and calling RecvMsg a second time on
// non-server-streaming RPCs to consume trailers and io.EOF, are not visible to
// user-defined interceptors.
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

	// Make a client-streaming RPC. When CloseAndRecv invokes RecvMsg once to
	// get the single reply message on a non-server-streaming RPC,
	// defaultStreamInterceptor automatically calls a second RecvMsg on the
	// underlying client stream to consume io.EOF and receive trailers. But this
	// should not be visible to user interceptors.
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
	if iStream.recvMsgCount != 1 {
		t.Fatalf("RecvMsg was called %v times on user interceptor stream, want 1 time", iStream.recvMsgCount)
	}

	// Make a server-streaming RPC. Since StreamingOutputCall is not
	// client-streaming, the proto generated code invokes CloseSend after
	// sending the single message. defaultStreamInterceptor also invokes
	// CloseSend right after sending the request message (to handle cases where
	// the user is using the ClientStream API instead of the proto generated
	// code) to signal downstream interceptors, which in this case are xDS
	// filters. The user interceptor should not see this CloseSend call.
	if _, err := ss.Client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatal("Error calling StreamingOutputCall:", err)
	}
	if iStream.closeSendCount != 1 {
		t.Fatalf("CloseSend called %v times on user interceptor stream, want 1 times", iStream.closeSendCount)
	}
}

// TestBidiStreaming_SendAfterCloseSend verifies that calling SendMsg after
// CloseSend on a bidirectional stream returns an Internal error, and repeated
// CloseSend calls succeed.
func (s) TestBidiStreaming_SendAfterCloseSend(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
					return err
				}
			}
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatal("Error starting FullDuplexCall:", err)
	}

	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("Stream.Send failed: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("Stream.CloseSend failed: %v", err)
	}

	// Sending another message after CloseSend should return an Internal error.
	err = stream.Send(&testpb.StreamingOutputCallRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("Stream.Send after CloseSend got error code %v (err: %v), want %v", status.Code(err), err, codes.Internal)
	}
	if !strings.Contains(err.Error(), "SendMsg called after CloseSend") {
		t.Fatalf("Stream.Send after CloseSend got error %v, want substring %q", err, "SendMsg called after CloseSend")
	}

	// Repeated CloseSend should succeed with nil error.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("Repeated stream.CloseSend failed: %v", err)
	}
}

type subConnProducerBuilder struct{}

type subConnProducer struct {
	cci grpc.ClientConnInterface
}

func (*subConnProducerBuilder) Build(cci any) (balancer.Producer, func()) {
	return &subConnProducer{cci: cci.(grpc.ClientConnInterface)}, func() {}
}

var subConnProducerBuilderSingleton = &subConnProducerBuilder{}

// startSubConnClient starts a server and a ClientConn configured with a custom
// LB policy that connects a single SubConn and returns its ClientConnInterface
// once Ready. Streams opened on this interface are backed by addrConnStream.
func startSubConnClient(ctx context.Context, t *testing.T, ss *stubserver.StubServer) testgrpc.TestServiceClient {
	t.Helper()
	if err := ss.StartServer(); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	t.Cleanup(ss.Stop)

	cciCh := make(chan grpc.ClientConnInterface, 1)
	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	bf := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			var sc balancer.SubConn
			sc, err := bd.ClientConn.NewSubConn(ccs.ResolverState.Addresses, balancer.NewSubConnOptions{
				StateListener: func(scs balancer.SubConnState) {
					if scs.ConnectivityState == connectivity.Ready {
						p, _ := sc.GetOrBuildProducer(subConnProducerBuilderSingleton)
						select {
						case cciCh <- p.(*subConnProducer).cci:
						default:
						}
					}
					bd.ClientConn.UpdateState(balancer.State{
						ConnectivityState: scs.ConnectivityState,
					})
				},
			})
			if err != nil {
				return err
			}
			sc.Connect()
			return nil
		},
	}
	stub.Register(name, bf)

	cc, err := grpc.NewClient("dns:///"+ss.Address,
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"`+name+`":{}}]}`),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	select {
	case cci := <-cciCh:
		return testgrpc.NewTestServiceClient(cci)
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for SubConn ClientConnInterface")
		return nil
	}
}

// TestAddrConnStream_Streaming opens a bidirectional stream directly on a
// SubConn (exercising addrConnStream) and verifies Header, Trailer, Context,
// compressed SendMsg/RecvMsg, CloseSend idempotency, and SendMsg after
// CloseSend.
func (s) TestAddrConnStream_Streaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			if err := stream.SendHeader(metadata.Pairs("test-header", "header-val")); err != nil {
				return err
			}
			stream.SetTrailer(metadata.Pairs("test-trailer", "trailer-val"))
			for {
				req, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
					return err
				}
			}
		},
	}

	client := startSubConnClient(ctx, t, ss)

	stream, err := client.FullDuplexCall(ctx, grpc.UseCompressor(gzip.Name))
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if stream.Context() == nil {
		t.Fatal("stream.Context() = nil, want non-nil")
	}

	hdr, err := stream.Header()
	if err != nil {
		t.Fatalf("stream.Header() failed: %v", err)
	}
	if got := hdr.Get("test-header"); len(got) != 1 || got[0] != "header-val" {
		t.Fatalf("stream.Header()[\"test-header\"] = %v, want [\"header-val\"]", got)
	}

	for i := 0; i < 3; i++ {
		wantBody := []byte{byte(i), byte(i + 1), byte(i + 2)}
		if err := stream.Send(&testpb.StreamingOutputCallRequest{
			Payload: &testpb.Payload{Body: wantBody},
		}); err != nil {
			t.Fatalf("stream.Send(%d) failed: %v", i, err)
		}
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv(%d) failed: %v", i, err)
		}
		if gotBody := resp.GetPayload().GetBody(); !bytes.Equal(gotBody, wantBody) {
			t.Fatalf("stream.Recv(%d) body = %v, want %v", i, gotBody, wantBody)
		}
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v", err)
	}
	// Repeated CloseSend should return nil.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("Repeated stream.CloseSend() failed: %v", err)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() after CloseSend = %v, want io.EOF", err)
	}
	if got := stream.Trailer().Get("test-trailer"); len(got) != 1 || got[0] != "trailer-val" {
		t.Fatalf("stream.Trailer()[\"test-trailer\"] = %v, want [\"trailer-val\"]", got)
	}

	// SendMsg after CloseSend should fail with Internal error.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("stream.Send after CloseSend = %v, want codes.Internal", err)
	}

	// Canceling the stream context triggers registerCancellationHandlers ->
	// addrConnStream.finish and terminates the stream with codes.Canceled.
	cancelCtx, cancelStream := context.WithCancel(ctx)
	stream2, err := client.FullDuplexCall(cancelCtx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	cancelStream()
	if _, err := stream2.Recv(); status.Code(err) != codes.Canceled {
		t.Fatalf("stream2.Recv() after context cancel = %v, want codes.Canceled", err)
	}
}

// TestAddrConnStream_Errors opens streams directly on a SubConn (exercising
// addrConnStream) and verifies unary invocation, cardinality violations,
// message size limits, unknown compressor errors, and Header errors.
func (s) TestAddrConnStream_Errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		StreamingInputCallF: func(testgrpc.TestService_StreamingInputCallServer) error {
			// Return nil without sending a response to trigger cardinality violation.
			return nil
		},
		StreamingOutputCallF: func(_ *testpb.StreamingOutputCallRequest, stream testgrpc.TestService_StreamingOutputCallServer) error {
			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: make([]byte, 20)},
			})
		},
		FullDuplexCallF: func(testgrpc.TestService_FullDuplexCallServer) error {
			return status.Error(codes.PermissionDenied, "denied before headers")
		},
	}

	client := startSubConnClient(ctx, t, ss)

	// 1. Unary RPC on SubConn succeeds.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall on SubConn failed: %v", err)
	}

	// 2. Unknown compressor fails stream creation with codes.Internal.
	if _, err := client.FullDuplexCall(ctx, grpc.UseCompressor("unknown-compressor")); status.Code(err) != codes.Internal {
		t.Fatalf("FullDuplexCall with unknown compressor = %v, want codes.Internal", err)
	}

	// 3. Cardinality violation when non-server-streaming RPC returns OK with no message.
	inStream, err := client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("StreamingInputCall failed: %v", err)
	}
	if _, err := inStream.CloseAndRecv(); status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "cardinality violation") {
		t.Fatalf("CloseAndRecv() = %v, want codes.Internal with 'cardinality violation'", err)
	}

	// 4. MaxCallSendMsgSize exceeded on addrConnStream.
	inStreamMaxSend, err := client.StreamingInputCall(ctx, grpc.MaxCallSendMsgSize(5))
	if err != nil {
		t.Fatalf("StreamingInputCall failed: %v", err)
	}
	err = inStreamMaxSend.Send(&testpb.StreamingInputCallRequest{
		Payload: &testpb.Payload{Body: make([]byte, 20)},
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Send exceeding MaxCallSendMsgSize = %v, want codes.ResourceExhausted", err)
	}

	// 5. MaxCallRecvMsgSize exceeded on addrConnStream.
	outStream, err := client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{}, grpc.MaxCallRecvMsgSize(5))
	if err != nil {
		t.Fatalf("StreamingOutputCall failed: %v", err)
	}
	if _, err := outStream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Recv exceeding MaxCallRecvMsgSize = %v, want codes.ResourceExhausted", err)
	}

	// 6. Header error when server fails before sending headers.
	biStream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if _, err := biStream.Header(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("biStream.Header() = _, %v; want codes.PermissionDenied", err)
	}
	if _, err := biStream.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("biStream.Recv() = %v, want codes.PermissionDenied", err)
	}
}
