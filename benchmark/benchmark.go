/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

/*
Package benchmark implements the building blocks to setup end-to-end gRPC benchmarks.
*/
package benchmark

import (
	"io"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/grpclog"
)

func newPayload(t testpb.PayloadType, size int) *testpb.Payload {
	if size < 0 {
		grpclog.Fatalf("Requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	case testpb.PayloadType_UNCOMPRESSABLE:
		grpclog.Fatalf("PayloadType UNCOMPRESSABLE is not supported")
	default:
		grpclog.Fatalf("Unsupported payload type: %d", t)
	}
	return &testpb.Payload{
		Type: t,
		Body: body,
	}
}

type testServer struct {
}

func (s *testServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{
		Payload: newPayload(in.ResponseType, int(in.ResponseSize)),
	}, nil
}

func (s *testServer) StreamingCall(stream testpb.BenchmarkService_StreamingCallServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(&testpb.SimpleResponse{
			Payload: newPayload(in.ResponseType, int(in.ResponseSize)),
		}); err != nil {
			return err
		}
	}
}

// StartServer starts a gRPC server serving a benchmark service on the given
// address, which may be something like "localhost:0". It returns its listen
// address and a function to stop the server.
func StartServer(addr string, opts ...grpc.ServerOption) (string, func()) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		grpclog.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer(opts...)
	testpb.RegisterBenchmarkServiceServer(s, &testServer{})
	go s.Serve(lis)
	return lis.Addr().String(), func() {
		s.Stop()
	}
}

type byteBufServer struct {
	respSize int32
}

func (s *byteBufServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{}, nil
}

func (s *byteBufServer) StreamingCall(stream testpb.BenchmarkService_StreamingCallServer) error {
	for {
		var in []byte
		err := stream.(grpc.ServerStream).RecvMsg(&in)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		out := make([]byte, s.respSize)
		if err := stream.(grpc.ServerStream).SendMsg(&out); err != nil {
			return err
		}
	}
}

// StartbyteBufServer starts a benchmark service server that supports custom codec.
// It returns its listen address and a function to stop the server.
func StartByteBufServer(addr string, respSize int32, opts ...grpc.ServerOption) (string, func()) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		grpclog.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer(opts...)
	testpb.RegisterBenchmarkServiceServer(s, &byteBufServer{respSize: respSize})
	go s.Serve(lis)
	return lis.Addr().String(), func() {
		s.Stop()
	}
}

// DoUnaryCall performs an unary RPC with given stub and request and response sizes.
func DoUnaryCall(tc testpb.BenchmarkServiceClient, reqSize, respSize int) error {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, reqSize)
	req := &testpb.SimpleRequest{
		ResponseType: pl.Type,
		ResponseSize: int32(respSize),
		Payload:      pl,
	}
	if _, err := tc.UnaryCall(context.Background(), req); err != nil {
		return grpc.Errorf(grpc.Code(err), "/BenchmarkService/UnaryCall RPC failed: %v", grpc.ErrorDesc(err))
	}
	return nil
}

// DoStreamingRoundTrip performs a round trip for a single streaming rpc.
func DoStreamingRoundTrip(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) error {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, reqSize)
	req := &testpb.SimpleRequest{
		ResponseType: pl.Type,
		ResponseSize: int32(respSize),
		Payload:      pl,
	}
	if err := stream.Send(req); err != nil {
		return grpc.Errorf(grpc.Code(err), "StreamingCall(_).Send: %v", grpc.ErrorDesc(err))
	}
	if _, err := stream.Recv(); err != nil {
		return grpc.Errorf(grpc.Code(err), "StreamingCall(_).Recv: %v", grpc.ErrorDesc(err))
	}
	return nil
}

// DoByteBufStreamingRoundTrip performs a round trip for a single streaming rpc, using custom codec.
func DoByteBufStreamingRoundTrip(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) error {
	out := make([]byte, reqSize)
	if err := stream.(grpc.ClientStream).SendMsg(&out); err != nil {
		return grpc.Errorf(grpc.Code(err), "StreamingCall(_).(ClientStream).SendMsg: %v", grpc.ErrorDesc(err))
	}
	var in []byte
	if err := stream.(grpc.ClientStream).RecvMsg(&in); err != nil {
		return grpc.Errorf(grpc.Code(err), "StreamingCall(_).(ClientStream).RecvMsg: %v", grpc.ErrorDesc(err))
	}
	return nil
}

// NewClientConn creates a gRPC client connection to addr.
func NewClientConn(addr string, opts ...grpc.DialOption) *grpc.ClientConn {
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		grpclog.Fatalf("NewClientConn(%q) failed to create a ClientConn %v", addr, err)
	}
	return conn
}
