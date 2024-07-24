//
//
// Copyright 2018 gRPC authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.4.0
// - protoc             v5.27.1
// source: examples/features/proto/echo/echo.proto

package echo

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	Echo_UnaryEcho_FullMethodName                  = "/grpc.examples.echo.Echo/UnaryEcho"
	Echo_ServerStreamingEcho_FullMethodName        = "/grpc.examples.echo.Echo/ServerStreamingEcho"
	Echo_ClientStreamingEcho_FullMethodName        = "/grpc.examples.echo.Echo/ClientStreamingEcho"
	Echo_BidirectionalStreamingEcho_FullMethodName = "/grpc.examples.echo.Echo/BidirectionalStreamingEcho"
)

// EchoClient is the client API for Echo service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
//
// Echo is the echo service.
type EchoClient interface {
	// UnaryEcho is unary echo.
	UnaryEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (*EchoResponse, error)
	// ServerStreamingEcho is server side streaming.
	ServerStreamingEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[EchoResponse], error)
	// ClientStreamingEcho is client side streaming.
	ClientStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[EchoRequest, EchoResponse], error)
	// BidirectionalStreamingEcho is bidi streaming.
	BidirectionalStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[EchoRequest, EchoResponse], error)
}

type echoClient struct {
	cc grpc.ClientConnInterface
}

func NewEchoClient(cc grpc.ClientConnInterface) EchoClient {
	return &echoClient{cc}
}

func (c *echoClient) UnaryEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (*EchoResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(EchoResponse)
	err := c.cc.Invoke(ctx, Echo_UnaryEcho_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *echoClient) ServerStreamingEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[EchoResponse], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Echo_ServiceDesc.Streams[0], Echo_ServerStreamingEcho_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[EchoRequest, EchoResponse]{ClientStream: stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_ServerStreamingEchoClient = grpc.ServerStreamingClient[EchoResponse]

func (c *echoClient) ClientStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[EchoRequest, EchoResponse], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Echo_ServiceDesc.Streams[1], Echo_ClientStreamingEcho_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[EchoRequest, EchoResponse]{ClientStream: stream}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_ClientStreamingEchoClient = grpc.ClientStreamingClient[EchoRequest, EchoResponse]

func (c *echoClient) BidirectionalStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[EchoRequest, EchoResponse], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Echo_ServiceDesc.Streams[2], Echo_BidirectionalStreamingEcho_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[EchoRequest, EchoResponse]{ClientStream: stream}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_BidirectionalStreamingEchoClient = grpc.BidiStreamingClient[EchoRequest, EchoResponse]

// EchoServer is the server API for Echo service.
// All implementations must embed UnimplementedEchoServer
// for forward compatibility.
//
// Echo is the echo service.
type EchoServer interface {
	// UnaryEcho is unary echo.
	UnaryEcho(context.Context, *EchoRequest) (*EchoResponse, error)
	// ServerStreamingEcho is server side streaming.
	ServerStreamingEcho(*EchoRequest, grpc.ServerStreamingServer[EchoResponse]) error
	// ClientStreamingEcho is client side streaming.
	ClientStreamingEcho(grpc.ClientStreamingServer[EchoRequest, EchoResponse]) error
	// BidirectionalStreamingEcho is bidi streaming.
	BidirectionalStreamingEcho(grpc.BidiStreamingServer[EchoRequest, EchoResponse]) error
	mustEmbedUnimplementedEchoServer()
}

// UnimplementedEchoServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedEchoServer struct{}

func (UnimplementedEchoServer) UnaryEcho(context.Context, *EchoRequest) (*EchoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnaryEcho not implemented")
}
func (UnimplementedEchoServer) ServerStreamingEcho(*EchoRequest, grpc.ServerStreamingServer[EchoResponse]) error {
	return status.Errorf(codes.Unimplemented, "method ServerStreamingEcho not implemented")
}
func (UnimplementedEchoServer) ClientStreamingEcho(grpc.ClientStreamingServer[EchoRequest, EchoResponse]) error {
	return status.Errorf(codes.Unimplemented, "method ClientStreamingEcho not implemented")
}
func (UnimplementedEchoServer) BidirectionalStreamingEcho(grpc.BidiStreamingServer[EchoRequest, EchoResponse]) error {
	return status.Errorf(codes.Unimplemented, "method BidirectionalStreamingEcho not implemented")
}
func (UnimplementedEchoServer) mustEmbedUnimplementedEchoServer() {}
func (UnimplementedEchoServer) testEmbeddedByValue()              {}

// UnsafeEchoServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to EchoServer will
// result in compilation errors.
type UnsafeEchoServer interface {
	mustEmbedUnimplementedEchoServer()
}

func RegisterEchoServer(s grpc.ServiceRegistrar, srv EchoServer) {
	// If the following call pancis, it indicates UnimplementedEchoServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&Echo_ServiceDesc, srv)
}

func _Echo_UnaryEcho_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EchoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(EchoServer).UnaryEcho(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Echo_UnaryEcho_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(EchoServer).UnaryEcho(ctx, req.(*EchoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Echo_ServerStreamingEcho_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(EchoRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(EchoServer).ServerStreamingEcho(m, &grpc.GenericServerStream[EchoRequest, EchoResponse]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_ServerStreamingEchoServer = grpc.ServerStreamingServer[EchoResponse]

func _Echo_ClientStreamingEcho_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(EchoServer).ClientStreamingEcho(&grpc.GenericServerStream[EchoRequest, EchoResponse]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_ClientStreamingEchoServer = grpc.ClientStreamingServer[EchoRequest, EchoResponse]

func _Echo_BidirectionalStreamingEcho_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(EchoServer).BidirectionalStreamingEcho(&grpc.GenericServerStream[EchoRequest, EchoResponse]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Echo_BidirectionalStreamingEchoServer = grpc.BidiStreamingServer[EchoRequest, EchoResponse]

// Echo_ServiceDesc is the grpc.ServiceDesc for Echo service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Echo_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.examples.echo.Echo",
	HandlerType: (*EchoServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "UnaryEcho",
			Handler:    _Echo_UnaryEcho_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "ServerStreamingEcho",
			Handler:       _Echo_ServerStreamingEcho_Handler,
			ServerStreams: true,
		},
		{
			StreamName:    "ClientStreamingEcho",
			Handler:       _Echo_ClientStreamingEcho_Handler,
			ClientStreams: true,
		},
		{
			StreamName:    "BidirectionalStreamingEcho",
			Handler:       _Echo_BidirectionalStreamingEcho_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "examples/features/proto/echo/echo.proto",
}
