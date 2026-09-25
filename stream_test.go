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
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
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

// stubConfigSelector is a test implementation of a config selector that allows
// specifying a custom SelectConfig function.
type stubConfigSelector struct {
	selectConfigFunc func(iresolver.RPCInfo) (*iresolver.RPCConfig, error)
}

func (f stubConfigSelector) SelectConfig(i iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
	return f.selectConfigFunc(i)
}

// stubClientInterceptor is a test implementation of a client interceptor that allows
// specifying a custom NewStream function.
type stubClientInterceptor struct {
	newStreamFunc func(context.Context, iresolver.RPCInfo, func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), ...grpc.CallOption) (grpc.ClientStream, error)
}

func (i *stubClientInterceptor) NewStream(ctx context.Context, ri iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return i.newStreamFunc(ctx, ri, newStream, opts...)
}

func (i *stubClientInterceptor) Close() {}

// mutatingClientStream is a test implementation of a ClientStream that allows
// mutating headers, trailers, peer, and injecting errors for Header, SendMsg,
// CloseSend, and RecvMsg.
type mutatingClientStream struct {
	grpc.ClientStream
	ctx          context.Context
	extraHeader  metadata.MD
	extraTrailer metadata.MD
	overridePeer *peer.Peer
	headerErr    error
	sendMsgErr   error
	recvMsgErr   error
}

func (s *mutatingClientStream) Header() (metadata.MD, error) {
	if s.headerErr != nil {
		return nil, s.headerErr
	}
	if s.ClientStream == nil {
		return s.extraHeader, nil
	}
	md, err := s.ClientStream.Header()
	if err != nil {
		return md, err
	}
	return metadata.Join(md, s.extraHeader), nil
}

func (s *mutatingClientStream) Trailer() metadata.MD {
	if s.ClientStream == nil {
		return s.extraTrailer
	}
	md := s.ClientStream.Trailer()
	return metadata.Join(md, s.extraTrailer)
}

func (s *mutatingClientStream) Context() context.Context {
	ctx := s.ctx
	if s.ClientStream != nil {
		ctx = s.ClientStream.Context()
	}
	if s.overridePeer != nil {
		return peer.NewContext(ctx, s.overridePeer)
	}
	return ctx
}

func (s *mutatingClientStream) SendMsg(m any) error {
	if s.sendMsgErr != nil {
		return s.sendMsgErr
	}
	return s.ClientStream.SendMsg(m)
}

func (s *mutatingClientStream) RecvMsg(m any) error {
	if s.recvMsgErr != nil {
		return s.recvMsgErr
	}
	return s.ClientStream.RecvMsg(m)
}

// Test verifies that Header, Trailer, and Peer CallOptions observe values
// returned by the ClientStream wrapped by chained RPCConfig.Interceptors (e.g.,
// xDS HTTP filters), both on successful and failed RPCs.
func (s) TestClientStream_AfterCallOption_WithInterceptor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rpcErr   error
		wantCode codes.Code
	}{
		{
			name:     "success",
			rpcErr:   nil,
			wantCode: codes.OK,
		},
		{
			name:     "rpc_error",
			rpcErr:   status.Error(codes.PermissionDenied, "aborted"),
			wantCode: codes.PermissionDenied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Set up a stub server that sends headers and trailers.
			ss := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					grpc.SendHeader(ctx, metadata.Pairs("server-header", "server-header-val"))
					grpc.SetTrailer(ctx, metadata.Pairs("server-trailer", "server-trailer-val"))
					return &testpb.Empty{}, tc.rpcErr
				},
			}
			ss.R = manual.NewBuilderWithScheme("callopt-after")
			if err := ss.Start(nil); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			// Set up the expected peer information for the interceptor to override.
			ip, err := netip.ParseAddr("192.0.2.1")
			if err != nil {
				t.Fatalf("Failed to parse IP: %v", err)
			}
			ipv4 := ip.As4()
			wantPeer := &peer.Peer{
				Addr: &net.TCPAddr{IP: ipv4[:], Port: 12345},
			}

			// Create two chained interceptors that wrap the ClientStream and inject
			// headers, trailers, and peer information.
			interceptor1 := &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{
						ClientStream: cs,
						extraHeader:  metadata.Pairs("filter1-header", "filter1-header-val"),
						extraTrailer: metadata.Pairs("filter1-trailer", "filter1-trailer-val"),
						overridePeer: wantPeer,
					}, nil
				},
			}
			interceptor2 := &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{
						ClientStream: cs,
						extraHeader:  metadata.Pairs("filter2-header", "filter2-header-val"),
						extraTrailer: metadata.Pairs("filter2-trailer", "filter2-trailer-val"),
					}, nil
				},
			}

			// Set up the resolver state with the stub server address and the custom
			// config selector that returns the chained interceptors.
			state := iresolver.SetConfigSelector(resolver.State{
				Addresses:     []resolver.Address{{Addr: ss.Address}},
				ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
			}, stubConfigSelector{
				selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
					return &iresolver.RPCConfig{Interceptors: []any{interceptor1, interceptor2}}, nil
				},
			})
			ss.R.UpdateState(state)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			wantHeader := metadata.Pairs(
				"content-type", "application/grpc",
				"server-header", "server-header-val",
				"filter1-header", "filter1-header-val",
				"filter2-header", "filter2-header-val",
			)
			wantTrailer := metadata.Pairs(
				"server-trailer", "server-trailer-val",
				"filter1-trailer", "filter1-trailer-val",
				"filter2-trailer", "filter2-trailer-val",
			)

			var gotHeader, gotTrailer metadata.MD
			var gotPeer peer.Peer
			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.Header(&gotHeader), grpc.Trailer(&gotTrailer), grpc.Peer(&gotPeer)); status.Code(err) != tc.wantCode {
				t.Fatalf("EmptyCall() error = %v, want code %v", err, tc.wantCode)
			}
			if diff := cmp.Diff(wantHeader, gotHeader); diff != "" {
				t.Errorf("Unexpected header metadata (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantTrailer, gotTrailer); diff != "" {
				t.Errorf("Unexpected trailer metadata (-want +got):\n%s", diff)
			}
			if !cmp.Equal(gotPeer.Addr, wantPeer.Addr) {
				t.Errorf("gotPeer.Addr = %v, want %v", gotPeer.Addr, wantPeer.Addr)
			}
		})
	}
}

// Test verifies that when an RPCConfig.Interceptor aborts early (in NewStream,
// Header, SendMsg, CloseSend, or RecvMsg), the innermost stream is still
// finished and OnFinish callbacks are executed exactly once.
func (s) TestClientStream_OnFinishWhenInterceptorAborts(t *testing.T) {
	wantErr := status.Error(codes.PermissionDenied, "aborted by filter")

	tests := []struct {
		name                 string
		interceptor          *stubClientInterceptor
		makeRPC              func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error
		wantInnermostCreated bool
	}{
		{
			name: "abort_in_NewStream_before_creating_innermost",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(context.Context, iresolver.RPCInfo, func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), ...grpc.CallOption) (grpc.ClientStream, error) {
					return nil, wantErr
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, opt)
				return err
			},
			wantInnermostCreated: false,
		},
		{
			name: "abort_in_SendMsg_before_creating_innermost",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, _ func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), _ ...grpc.CallOption) (grpc.ClientStream, error) {
					// Don't delegate to newStream, abort in SendMsg.
					return &mutatingClientStream{ctx: ctx, sendMsgErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, opt)
				return err
			},
			wantInnermostCreated: false,
		},
		{
			name: "abort_in_NewStream_after_creating_innermost",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					if _, err := newStream(ctx, opts...); err != nil {
						return nil, err
					}
					return nil, wantErr
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, opt)
				return err
			},
			wantInnermostCreated: true,
		},
		{
			name: "abort_in_Header_streaming",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{ClientStream: cs, headerErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				stream, err := client.StreamingInputCall(ctx, opt)
				if err != nil {
					return err
				}
				_, err = stream.Header()
				return err
			},
			wantInnermostCreated: true,
		},
		{
			name: "abort_in_SendMsg_unary",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{ClientStream: cs, sendMsgErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, opt)
				return err
			},
			wantInnermostCreated: true,
		},
		{
			name: "abort_in_SendMsg_client_streaming",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{ClientStream: cs, sendMsgErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				stream, err := client.StreamingInputCall(ctx, opt)
				if err != nil {
					return err
				}
				return stream.Send(&testpb.StreamingInputCallRequest{})
			},
			wantInnermostCreated: true,
		},
		{
			name: "abort_in_RecvMsg_unary",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{ClientStream: cs, recvMsgErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				_, err := client.EmptyCall(ctx, &testpb.Empty{}, opt)
				return err
			},
			wantInnermostCreated: true,
		},
		{
			name: "abort_in_RecvMsg_server_streaming",
			interceptor: &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					return &mutatingClientStream{ClientStream: cs, recvMsgErr: wantErr}, nil
				},
			},
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient, opt grpc.CallOption) error {
				stream, err := client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{}, opt)
				if err != nil {
					return err
				}
				_, err = stream.Recv()
				return err
			},
			wantInnermostCreated: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set up a stub server with handlers for different RPC types as
			// different subtests test different RPC behaviors.
			ss := &stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
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
				StreamingOutputCallF: func(_ *testpb.StreamingOutputCallRequest, stream testgrpc.TestService_StreamingOutputCallServer) error {
					return stream.Send(&testpb.StreamingOutputCallResponse{})
				},
			}
			ss.R = manual.NewBuilderWithScheme("onfinish-abort")
			if err := ss.Start(nil); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			// Set up a client interceptor that adds an OnFinish callback that
			// is used to track if OnFinish is executed.
			var interceptorOnFinishCount int
			var interceptorOnFinishErr error
			addingInterceptor := &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					opts = append(opts, grpc.OnFinish(func(err error) {
						interceptorOnFinishCount++
						interceptorOnFinishErr = err
					}))
					return newStream(ctx, opts...)
				},
			}

			// Set up a client interceptor that captures the innermost stream.
			var innermostStream grpc.ClientStream
			innermostMonitor := &stubClientInterceptor{
				newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
					cs, err := newStream(ctx, opts...)
					if err != nil {
						return nil, err
					}
					innermostStream = cs
					return cs, nil
				},
			}

			// Set up the resolver state with the stub server address and a
			// config selector that returns all of the above interceptors.
			state := iresolver.SetConfigSelector(resolver.State{
				Addresses:     []resolver.Address{{Addr: ss.Address}},
				ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
			}, stubConfigSelector{
				selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
					return &iresolver.RPCConfig{Interceptors: []any{addingInterceptor, tc.interceptor, innermostMonitor}}, nil
				},
			})
			ss.R.UpdateState(state)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			var onFinishCount int
			var onFinishErr error
			err := tc.makeRPC(ctx, ss.Client, grpc.OnFinish(func(err error) {
				onFinishCount++
				onFinishErr = err
			}))
			if status.Code(err) != codes.PermissionDenied {
				t.Errorf("RPC failed with error %v, want code %v", err, codes.PermissionDenied)
			}
			if onFinishCount != 1 {
				t.Errorf("OnFinish called %d times, want 1", onFinishCount)
			}
			if status.Code(onFinishErr) != codes.PermissionDenied {
				t.Errorf("OnFinish received error %v, want code %v", onFinishErr, codes.PermissionDenied)
			}
			if interceptorOnFinishCount != 1 {
				t.Errorf("Interceptor OnFinish called %d times, want 1", interceptorOnFinishCount)
			}
			if status.Code(interceptorOnFinishErr) != codes.PermissionDenied {
				t.Errorf("Interceptor OnFinish received error %v, want code %v", interceptorOnFinishErr, codes.PermissionDenied)
			}
			if tc.wantInnermostCreated {
				if innermostStream == nil {
					t.Fatalf("Innermost stream was not created, want created")
				}
				if innermostStream.Context().Err() == nil {
					t.Errorf("Innermost stream context is not canceled after interceptor abort, want canceled")
				}
			} else if innermostStream != nil {
				t.Errorf("Innermost stream was unexpectedly created")
			}
		})
	}
}

// Test verifies that when a streaming RPC is canceled (via context cancellation
// or ClientConn closure) while an interceptor has deferred or skipped creating
// the innermost stream, clientStreamWrapper.registerCancellationHandlers runs
// OnFinish callbacks, and any asynchronous call to newStream after cancellation
// fails without creating a stream and runs newly added OnFinish callbacks.
func (s) TestClientStream_CancelOrCloseWithInterceptor(t *testing.T) {
	t.Run("cancel_before_creating_innermost", func(t *testing.T) {
		ss := &stubserver.StubServer{
			StreamingInputCallF: func(testgrpc.TestService_StreamingInputCallServer) error {
				return nil
			},
		}
		ss.R = manual.NewBuilderWithScheme("cancel-no-innermost")
		if err := ss.Start(nil); err != nil {
			t.Fatalf("Error starting stub server: %v", err)
		}
		defer ss.Stop()

		interceptorOnFinishCh := make(chan error, 1)
		interceptor := &stubClientInterceptor{
			newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, _ func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), _ ...grpc.CallOption) (grpc.ClientStream, error) {
				// Return a stream without calling newStream.
				return &mutatingClientStream{ctx: ctx}, nil
			},
		}
		addingInterceptor := &stubClientInterceptor{
			newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
				opts = append(opts, grpc.OnFinish(func(err error) {
					interceptorOnFinishCh <- err
				}))
				return newStream(ctx, opts...)
			},
		}

		state := iresolver.SetConfigSelector(resolver.State{
			Addresses:     []resolver.Address{{Addr: ss.Address}},
			ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
		}, stubConfigSelector{
			selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
				return &iresolver.RPCConfig{Interceptors: []any{addingInterceptor, interceptor}}, nil
			},
		})
		ss.R.UpdateState(state)

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		rpcCtx, rpcCancel := context.WithCancel(ctx)
		defer cancel()

		userOnFinishCh := make(chan error, 1)
		if _, err := ss.Client.StreamingInputCall(rpcCtx, grpc.OnFinish(func(err error) {
			userOnFinishCh <- err
		})); err != nil {
			t.Fatalf("StreamingInputCall() failed: %v", err)
		}

		// Cancel the RPC context without calling RecvMsg.
		rpcCancel()

		select {
		case err := <-userOnFinishCh:
			if status.Code(err) != codes.Canceled {
				t.Errorf("User OnFinish error = %v, want code %v", err, codes.Canceled)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for user OnFinish callback")
		}

		select {
		case err := <-interceptorOnFinishCh:
			if status.Code(err) != codes.Canceled {
				t.Errorf("Interceptor OnFinish error = %v, want code %v", err, codes.Canceled)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for interceptor OnFinish callback")
		}
	})

	t.Run("clientconn_close_before_creating_innermost", func(t *testing.T) {
		ss := &stubserver.StubServer{
			StreamingInputCallF: func(testgrpc.TestService_StreamingInputCallServer) error {
				return nil
			},
		}
		ss.R = manual.NewBuilderWithScheme("close-no-innermost")
		if err := ss.Start(nil); err != nil {
			t.Fatalf("Error starting stub server: %v", err)
		}
		defer ss.Stop()

		interceptor := &stubClientInterceptor{
			newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, _ func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mutatingClientStream{ctx: ctx}, nil
			},
		}

		state := iresolver.SetConfigSelector(resolver.State{
			Addresses:     []resolver.Address{{Addr: ss.Address}},
			ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
		}, stubConfigSelector{
			selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
				return &iresolver.RPCConfig{Interceptors: []any{interceptor}}, nil
			},
		})
		ss.R.UpdateState(state)

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		userOnFinishCh := make(chan error, 1)
		if _, err := ss.Client.StreamingInputCall(ctx, grpc.OnFinish(func(err error) {
			userOnFinishCh <- err
		})); err != nil {
			t.Fatalf("StreamingInputCall() failed: %v", err)
		}

		// Close the ClientConn without calling RecvMsg.
		ss.CC.Close()

		select {
		case err := <-userOnFinishCh:
			if !errors.Is(err, grpc.ErrClientConnClosing) {
				t.Errorf("User OnFinish error = %v, want %v", err, grpc.ErrClientConnClosing)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for user OnFinish callback")
		}
	})

	t.Run("async_newStream_after_context_canceled", func(t *testing.T) {
		ss := &stubserver.StubServer{
			StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
				<-stream.Context().Done()
				return stream.Context().Err()
			},
		}
		ss.R = manual.NewBuilderWithScheme("async-newstream")
		if err := ss.Start(nil); err != nil {
			t.Fatalf("Error starting stub server: %v", err)
		}
		defer ss.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		rpcCtx, rpcCancel := context.WithCancel(ctx)
		defer cancel()

		proceedCh := make(chan struct{})
		lateOnFinishCh := make(chan error, 1)
		lateNewStreamErrCh := make(chan error, 1)

		interceptor := &stubClientInterceptor{
			newStreamFunc: func(streamCtx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
				go func() {
					<-proceedCh
					// Pass the uncanceled parent test context, so that the
					// only reason for newStream to fail is that the RPC has
					// already finished.
					opts = append(opts, grpc.OnFinish(func(err error) {
						lateOnFinishCh <- err
					}))
					cs, err := newStream(ctx, opts...)
					if cs != nil {
						t.Errorf("newStream() returned a non-nil stream after the RPC finished")
					}
					lateNewStreamErrCh <- err
				}()
				return &mutatingClientStream{ctx: streamCtx}, nil
			},
		}
		// nextInterceptor follows the interceptor above in the chain. It must
		// not be invoked when newStream is called after the RPC has finished.
		nextInterceptorCalledCh := make(chan struct{}, 1)
		nextInterceptor := &stubClientInterceptor{
			newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
				nextInterceptorCalledCh <- struct{}{}
				return newStream(ctx, opts...)
			},
		}

		state := iresolver.SetConfigSelector(resolver.State{
			Addresses:     []resolver.Address{{Addr: ss.Address}},
			ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
		}, stubConfigSelector{
			selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
				return &iresolver.RPCConfig{Interceptors: []any{interceptor, nextInterceptor}}, nil
			},
		})
		ss.R.UpdateState(state)

		userOnFinishCh := make(chan error, 1)
		if _, err := ss.Client.StreamingInputCall(rpcCtx, grpc.OnFinish(func(err error) {
			userOnFinishCh <- err
		})); err != nil {
			t.Fatalf("StreamingInputCall() failed: %v", err)
		}

		// Cancel the RPC context and wait for the outer wrapper to finish.
		rpcCancel()
		select {
		case err := <-userOnFinishCh:
			if status.Code(err) != codes.Canceled {
				t.Fatalf("User OnFinish error = %v, want code %v", err, codes.Canceled)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for user OnFinish callback")
		}

		// Now let the background goroutine call newStream after the wrapper has
		// already finished.
		close(proceedCh)

		select {
		case err := <-lateOnFinishCh:
			if status.Code(err) != codes.Canceled {
				t.Errorf("Late OnFinish error = %v, want code %v", err, codes.Canceled)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for late OnFinish callback from setOpts")
		}

		select {
		case err := <-lateNewStreamErrCh:
			if status.Code(err) != codes.Canceled {
				t.Errorf("newStream() after the RPC finished returned error %v, want code %v", err, codes.Canceled)
			}
		case <-ctx.Done():
			t.Fatal("Timed out waiting for newStream() called after the RPC finished to return")
		}

		// newStream has returned, so the next interceptor would have been
		// invoked by now if newStream did not return early.
		select {
		case <-nextInterceptorCalledCh:
			t.Error("Next interceptor in the chain was invoked by newStream() after the RPC finished")
		default:
		}
	})
}

// blockingContextClientStream is a test implementation of a ClientStream whose
// Context method blocks until the delegate stream has been created by an
// asynchronous call to newStream. This mimics interceptors (like ext_proc)
// that defer creation of the delegate stream to a background goroutine. All
// other methods are provided by the embedded mutatingClientStream.
type blockingContextClientStream struct {
	*mutatingClientStream
	contextCalledOnce sync.Once
	contextCalled     chan struct{} // Closed when Context is first called.
	delegateCreated   chan struct{} // Closed once newStream has returned.
}

func (s *blockingContextClientStream) Context() context.Context {
	s.contextCalledOnce.Do(func() { close(s.contextCalled) })
	select {
	case <-s.delegateCreated:
	case <-s.mutatingClientStream.ctx.Done():
	}
	return s.mutatingClientStream.Context()
}

// Test verifies that an RPC completes when an interceptor creates the delegate
// stream asynchronously, and CallOption.after callbacks (grpc.Peer here) block
// on the interceptor's stream until that happens. It also verifies that
// newStream, when called after the RPC has completed, fails with the RPC's
// error without creating a stream.
func (s) TestClientStream_AfterCallOptionDoesNotBlockAsyncNewStream(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	ss.R = manual.NewBuilderWithScheme("after-async-newstream")
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	wantErr := status.Error(codes.Unavailable, "interceptor failure")
	lateNewStreamErrCh := make(chan error, 1)
	interceptor := &stubClientInterceptor{
		newStreamFunc: func(streamCtx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
			cs := &blockingContextClientStream{
				// The delegate is never set on the embedded stream. This is
				// safe because SendMsg fails before touching it, so CloseSend
				// and RecvMsg are never called, and grpc.Peer's after callback
				// only calls Context, which is overridden.
				mutatingClientStream: &mutatingClientStream{ctx: streamCtx, sendMsgErr: wantErr},
				contextCalled:        make(chan struct{}),
				delegateCreated:      make(chan struct{}),
			}
			go func() {
				defer close(cs.delegateCreated)
				// Wait until the after callback for grpc.Peer is blocked in
				// Context before creating the delegate stream. This
				// deterministically triggers the deadlock, if one exists.
				select {
				case <-cs.contextCalled:
				case <-ctx.Done():
					lateNewStreamErrCh <- ctx.Err()
					return
				}
				// The RPC has already finished with wantErr, so newStream
				// must fail without creating a stream.
				innermost, err := newStream(streamCtx, opts...)
				if innermost != nil {
					t.Errorf("newStream() returned a non-nil stream after the RPC finished")
				}
				lateNewStreamErrCh <- err
			}()
			return cs, nil
		},
	}

	state := iresolver.SetConfigSelector(resolver.State{
		Addresses:     []resolver.Address{{Addr: ss.Address}},
		ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
	}, stubConfigSelector{
		selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
			return &iresolver.RPCConfig{Interceptors: []any{interceptor}}, nil
		},
	})
	ss.R.UpdateState(state)

	rpcErrCh := make(chan error, 1)
	go func() {
		var p peer.Peer
		_, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&p))
		rpcErrCh <- err
	}()

	select {
	case err := <-rpcErrCh:
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("EmptyCall() error = %v, want code %v", err, codes.Unavailable)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for EmptyCall() to return; possible deadlock in clientStreamWrapper.finish")
	}

	select {
	case err := <-lateNewStreamErrCh:
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("newStream() after the RPC finished returned error %v, want code %v", err, codes.Unavailable)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for newStream() called after the RPC finished to return")
	}
}

// Test verifies that an OnFinish callback can invoke methods on the stream that
// may cause the stream to be finished again (e.g. Header returning an error),
// without deadlocking.
func (s) TestClientStream_OnFinishCanCallStreamMethods(t *testing.T) {
	ss := &stubserver.StubServer{
		StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}
	ss.R = manual.NewBuilderWithScheme("onfinish-reentrant")
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting stub server: %v", err)
	}
	defer ss.Stop()

	// Header is only called from the OnFinish callback below, so returning an
	// error unconditionally from it ensures that the call to Header from
	// OnFinish ends up calling finish on the stream again.
	wantErr := status.Error(codes.Unavailable, "header failure")
	interceptor := &stubClientInterceptor{
		newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
			cs, err := newStream(ctx, opts...)
			if err != nil {
				return nil, err
			}
			return &mutatingClientStream{ClientStream: cs, headerErr: wantErr}, nil
		},
	}
	state := iresolver.SetConfigSelector(resolver.State{
		Addresses:     []resolver.Address{{Addr: ss.Address}},
		ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
	}, stubConfigSelector{
		selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
			return &iresolver.RPCConfig{Interceptors: []any{interceptor}}, nil
		},
	})
	ss.R.UpdateState(state)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	rpcCtx, rpcCancel := context.WithCancel(ctx)
	defer rpcCancel()

	// The OnFinish callback needs access to the stream returned by the RPC,
	// which is only available after the RPC is created.
	streamCh := make(chan grpc.ClientStream, 1)
	headerErrCh := make(chan error, 1)
	stream, err := ss.Client.StreamingInputCall(rpcCtx, grpc.OnFinish(func(error) {
		select {
		case s := <-streamCh:
			_, err := s.Header()
			headerErrCh <- err
		case <-ctx.Done():
			headerErrCh <- ctx.Err()
		}
	}))
	if err != nil {
		t.Fatalf("StreamingInputCall() failed: %v", err)
	}
	streamCh <- stream

	// Cancel the RPC to trigger the cancellation handler, which finishes the
	// stream and runs the OnFinish callback.
	rpcCancel()

	select {
	case err := <-headerErrCh:
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("Header() from OnFinish returned error %v, want code %v", err, codes.Unavailable)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for OnFinish callback to call Header(); possible deadlock in clientStreamWrapper.finish")
	}
}

// Test verifies that when an interceptor's Header method returns io.EOF, the
// stream is not finished at that point, and that the RPC's status is instead
// determined by RecvMsg and reported to OnFinish callbacks.
func (s) TestClientStream_HeaderEOFDoesNotFinishStream(t *testing.T) {
	ss := &stubserver.StubServer{
		StreamingOutputCallF: func(*testpb.StreamingOutputCallRequest, testgrpc.TestService_StreamingOutputCallServer) error {
			return status.Error(codes.ResourceExhausted, "server failure")
		},
	}
	ss.R = manual.NewBuilderWithScheme("header-eof")
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting stub server: %v", err)
	}
	defer ss.Stop()

	interceptor := &stubClientInterceptor{
		newStreamFunc: func(ctx context.Context, _ iresolver.RPCInfo, newStream func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error), opts ...grpc.CallOption) (grpc.ClientStream, error) {
			cs, err := newStream(ctx, opts...)
			if err != nil {
				return nil, err
			}
			return &mutatingClientStream{ClientStream: cs, headerErr: io.EOF}, nil
		},
	}
	state := iresolver.SetConfigSelector(resolver.State{
		Addresses:     []resolver.Address{{Addr: ss.Address}},
		ServiceConfig: ss.R.CC().ParseServiceConfig("{}"),
	}, stubConfigSelector{
		selectConfigFunc: func(iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
			return &iresolver.RPCConfig{Interceptors: []any{interceptor}}, nil
		},
	})
	ss.R.UpdateState(state)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	onFinishCh := make(chan error, 1)
	stream, err := ss.Client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{}, grpc.OnFinish(func(err error) {
		onFinishCh <- err
	}))
	if err != nil {
		t.Fatalf("StreamingOutputCall() failed: %v", err)
	}

	if _, err := stream.Header(); err != io.EOF {
		t.Fatalf("stream.Header() returned error %v, want %v", err, io.EOF)
	}
	// If Header had finished the stream, OnFinish would have been invoked
	// synchronously from within Header.
	select {
	case err := <-onFinishCh:
		t.Fatalf("OnFinish called with error %v after Header() returned io.EOF, want no call until RecvMsg returns", err)
	default:
	}

	if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("stream.Recv() returned error %v, want code %v", err, codes.ResourceExhausted)
	}
	select {
	case err := <-onFinishCh:
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("OnFinish called with error %v, want code %v", err, codes.ResourceExhausted)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for OnFinish to be called")
	}
}
