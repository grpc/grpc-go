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

package grpc_testing

import proto2 "proto/empty_proto"

import (
	"fmt"
	"github.com/google/arcwire-go/rpc"
	proto "code.google.com/p/goprotobuf/proto"
	context "golang.org/x/net/context"
	"io"
)

type TestServiceClient interface {
	EmptyCall(ctx context.Context, in *proto2.Empty, opts ...rpc.CallOption) (*proto2.Empty, error)
	UnaryCall(ctx context.Context, in *SimpleRequest, opts ...rpc.CallOption) (*SimpleResponse, error)
	StreamingOutputCall(ctx context.Context, m *StreamingOutputCallRequest, opts ...rpc.CallOption) (TestService_StreamingOutputCallClient, error)
	StreamingInputCall(ctx context.Context, opts ...rpc.CallOption) (TestService_StreamingInputCallClient, error)
	FullDuplexCall(ctx context.Context, opts ...rpc.CallOption) (TestService_FullDuplexCallClient, error)
	HalfDuplexCall(ctx context.Context, opts ...rpc.CallOption) (TestService_HalfDuplexCallClient, error)
}

type testServiceClient struct {
	cc *rpc.ClientConn
}

func NewTestServiceClient(cc *rpc.ClientConn) TestServiceClient {
	return &testServiceClient{cc}
}

func (c *testServiceClient) EmptyCall(ctx context.Context, in *proto2.Empty, opts ...rpc.CallOption) (*proto2.Empty, error) {
	out := new(proto2.Empty)
	err := rpc.Invoke(ctx, "/TestService/EmptyCall", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *testServiceClient) UnaryCall(ctx context.Context, in *SimpleRequest, opts ...rpc.CallOption) (*SimpleResponse, error) {
	out := new(SimpleResponse)
	err := rpc.Invoke(ctx, "/TestService/UnaryCall", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *testServiceClient) StreamingOutputCall(ctx context.Context, m *StreamingOutputCallRequest, opts ...rpc.CallOption) (TestService_StreamingOutputCallClient, error) {
	stream, err := rpc.NewClientStream(ctx, c.cc, "/TestService/StreamingOutputCall", opts...)
	if err != nil {
		return nil, err
	}
	x := &testServiceStreamingOutputCallClient{stream}
	if err := x.ClientStream.SendProto(m); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type TestService_StreamingOutputCallClient interface {
	Recv() (*StreamingOutputCallResponse, error)
	rpc.ClientStream
}

type testServiceStreamingOutputCallClient struct {
	rpc.ClientStream
}

func (x *testServiceStreamingOutputCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *testServiceClient) StreamingInputCall(ctx context.Context, opts ...rpc.CallOption) (TestService_StreamingInputCallClient, error) {
	stream, err := rpc.NewClientStream(ctx, c.cc, "/TestService/StreamingInputCall", opts...)
	if err != nil {
		return nil, err
	}
	return &testServiceStreamingInputCallClient{stream}, nil
}

type TestService_StreamingInputCallClient interface {
	Send(*StreamingInputCallRequest) error
	CloseAndRecv() (*StreamingInputCallResponse, error)
	rpc.ClientStream
}

type testServiceStreamingInputCallClient struct {
	rpc.ClientStream
}

func (x *testServiceStreamingInputCallClient) Send(m *StreamingInputCallRequest) error {
	return x.ClientStream.SendProto(m)
}

func (x *testServiceStreamingInputCallClient) CloseAndRecv() (*StreamingInputCallResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(StreamingInputCallResponse)
	if err := x.ClientStream.RecvProto(m); err != nil {
		return nil, err
	}
	// Read EOF.
	if err := x.ClientStream.RecvProto(m); err == io.EOF {
		return m, io.EOF
	}
	// gRPC protocol violation.
	return m, fmt.Errorf("Violate gRPC client streaming protocol: no EOF after the response.")
}

func (c *testServiceClient) FullDuplexCall(ctx context.Context, opts ...rpc.CallOption) (TestService_FullDuplexCallClient, error) {
	stream, err := rpc.NewClientStream(ctx, c.cc, "/TestService/FullDuplexCall", opts...)
	if err != nil {
		return nil, err
	}
	return &testServiceFullDuplexCallClient{stream}, nil
}

type TestService_FullDuplexCallClient interface {
	Send(*StreamingOutputCallRequest) error
	Recv() (*StreamingOutputCallResponse, error)
	rpc.ClientStream
}

type testServiceFullDuplexCallClient struct {
	rpc.ClientStream
}

func (x *testServiceFullDuplexCallClient) Send(m *StreamingOutputCallRequest) error {
	return x.ClientStream.SendProto(m)
}

func (x *testServiceFullDuplexCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *testServiceClient) HalfDuplexCall(ctx context.Context, opts ...rpc.CallOption) (TestService_HalfDuplexCallClient, error) {
	stream, err := rpc.NewClientStream(ctx, c.cc, "/TestService/HalfDuplexCall", opts...)
	if err != nil {
		return nil, err
	}
	return &testServiceHalfDuplexCallClient{stream}, nil
}

type TestService_HalfDuplexCallClient interface {
	Send(*StreamingOutputCallRequest) error
	Recv() (*StreamingOutputCallResponse, error)
	rpc.ClientStream
}

type testServiceHalfDuplexCallClient struct {
	rpc.ClientStream
}

func (x *testServiceHalfDuplexCallClient) Send(m *StreamingOutputCallRequest) error {
	return x.ClientStream.SendProto(m)
}

func (x *testServiceHalfDuplexCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

type TestServiceServer interface {
	EmptyCall(context.Context, *proto2.Empty) (*proto2.Empty, error)
	UnaryCall(context.Context, *SimpleRequest) (*SimpleResponse, error)
	StreamingOutputCall(*StreamingOutputCallRequest, TestService_StreamingOutputCallServer) error
	StreamingInputCall(TestService_StreamingInputCallServer) error
	FullDuplexCall(TestService_FullDuplexCallServer) error
	HalfDuplexCall(TestService_HalfDuplexCallServer) error
}

func RegisterService(s *rpc.Server, srv TestServiceServer) {
	s.RegisterService(&_TestService_serviceDesc, srv)
}

func _TestService_EmptyCall_Handler(srv interface{}, ctx context.Context, buf []byte) (proto.Message, error) {
	in := new(proto2.Empty)
	if err := proto.Unmarshal(buf, in); err != nil {
		return nil, err
	}
	out, err := srv.(TestServiceServer).EmptyCall(ctx, in)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func _TestService_UnaryCall_Handler(srv interface{}, ctx context.Context, buf []byte) (proto.Message, error) {
	in := new(SimpleRequest)
	if err := proto.Unmarshal(buf, in); err != nil {
		return nil, err
	}
	out, err := srv.(TestServiceServer).UnaryCall(ctx, in)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func _TestService_StreamingOutputCall_Handler(srv interface{}, stream rpc.Stream) error {
	m := new(StreamingOutputCallRequest)
	if err := stream.RecvProto(m); err != nil {
		return err
	}
	return srv.(TestServiceServer).StreamingOutputCall(m, &testServiceStreamingOutputCallServer{stream})
}

type TestService_StreamingOutputCallServer interface {
	Send(*StreamingOutputCallResponse) error
	rpc.Stream
}

type testServiceStreamingOutputCallServer struct {
	rpc.Stream
}

func (x *testServiceStreamingOutputCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.Stream.SendProto(m)
}

func _TestService_StreamingInputCall_Handler(srv interface{}, stream rpc.Stream) error {
	return srv.(TestServiceServer).StreamingInputCall(&testServiceStreamingInputCallServer{stream})
}

type TestService_StreamingInputCallServer interface {
	SendAndClose(*StreamingInputCallResponse) error
	Recv() (*StreamingInputCallRequest, error)
	rpc.Stream
}

type testServiceStreamingInputCallServer struct {
	rpc.Stream
}

func (x *testServiceStreamingInputCallServer) SendAndClose(m *StreamingInputCallResponse) error {
	if err := x.Stream.SendProto(m); err != nil {
		return err
	}
	return nil
}

func (x *testServiceStreamingInputCallServer) Recv() (*StreamingInputCallRequest, error) {
	m := new(StreamingInputCallRequest)
	if err := x.Stream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _TestService_FullDuplexCall_Handler(srv interface{}, stream rpc.Stream) error {
	return srv.(TestServiceServer).FullDuplexCall(&testServiceFullDuplexCallServer{stream})
}

type TestService_FullDuplexCallServer interface {
	Send(*StreamingOutputCallResponse) error
	Recv() (*StreamingOutputCallRequest, error)
	rpc.Stream
}

type testServiceFullDuplexCallServer struct {
	rpc.Stream
}

func (x *testServiceFullDuplexCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.Stream.SendProto(m)
}

func (x *testServiceFullDuplexCallServer) Recv() (*StreamingOutputCallRequest, error) {
	m := new(StreamingOutputCallRequest)
	if err := x.Stream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _TestService_HalfDuplexCall_Handler(srv interface{}, stream rpc.Stream) error {
	return srv.(TestServiceServer).HalfDuplexCall(&testServiceHalfDuplexCallServer{stream})
}

type TestService_HalfDuplexCallServer interface {
	Send(*StreamingOutputCallResponse) error
	Recv() (*StreamingOutputCallRequest, error)
	rpc.Stream
}

type testServiceHalfDuplexCallServer struct {
	rpc.Stream
}

func (x *testServiceHalfDuplexCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.Stream.SendProto(m)
}

func (x *testServiceHalfDuplexCallServer) Recv() (*StreamingOutputCallRequest, error) {
	m := new(StreamingOutputCallRequest)
	if err := x.Stream.RecvProto(m); err != nil {
		return nil, err
	}
	return m, nil
}

var _TestService_serviceDesc = rpc.ServiceDesc{
	ServiceName: "TestService",
	HandlerType: (*TestServiceServer)(nil),
	Methods: []rpc.MethodDesc{
		{
			MethodName: "EmptyCall",
			Handler:    _TestService_EmptyCall_Handler,
		},
		{
			MethodName: "UnaryCall",
			Handler:    _TestService_UnaryCall_Handler,
		},
	},
	Streams: []rpc.StreamDesc{
		{
			StreamName: "StreamingOutputCall",
			Handler:    _TestService_StreamingOutputCall_Handler,
		},
		{
			StreamName: "StreamingInputCall",
			Handler:    _TestService_StreamingInputCall_Handler,
		},
		{
			StreamName: "FullDuplexCall",
			Handler:    _TestService_FullDuplexCall_Handler,
		},
		{
			StreamName: "HalfDuplexCall",
			Handler:    _TestService_HalfDuplexCall_Handler,
		},
	},
}
