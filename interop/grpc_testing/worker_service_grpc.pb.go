// Copyright 2015 gRPC authors.
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

// An integration test service that covers all the method signature permutations
// of unary/streaming requests/responses.

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.4.0
// - protoc             v5.27.1
// source: grpc/testing/worker_service.proto

package grpc_testing

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
	WorkerService_RunServer_FullMethodName  = "/grpc.testing.WorkerService/RunServer"
	WorkerService_RunClient_FullMethodName  = "/grpc.testing.WorkerService/RunClient"
	WorkerService_CoreCount_FullMethodName  = "/grpc.testing.WorkerService/CoreCount"
	WorkerService_QuitWorker_FullMethodName = "/grpc.testing.WorkerService/QuitWorker"
)

// WorkerServiceClient is the client API for WorkerService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type WorkerServiceClient interface {
	// Start server with specified workload.
	// First request sent specifies the ServerConfig followed by ServerStatus
	// response. After that, a "Mark" can be sent anytime to request the latest
	// stats. Closing the stream will initiate shutdown of the test server
	// and once the shutdown has finished, the OK status is sent to terminate
	// this RPC.
	RunServer(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[ServerArgs, ServerStatus], error)
	// Start client with specified workload.
	// First request sent specifies the ClientConfig followed by ClientStatus
	// response. After that, a "Mark" can be sent anytime to request the latest
	// stats. Closing the stream will initiate shutdown of the test client
	// and once the shutdown has finished, the OK status is sent to terminate
	// this RPC.
	RunClient(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[ClientArgs, ClientStatus], error)
	// Just return the core count - unary call
	CoreCount(ctx context.Context, in *CoreRequest, opts ...grpc.CallOption) (*CoreResponse, error)
	// Quit this worker
	QuitWorker(ctx context.Context, in *Void, opts ...grpc.CallOption) (*Void, error)
}

type workerServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewWorkerServiceClient(cc grpc.ClientConnInterface) WorkerServiceClient {
	return &workerServiceClient{cc}
}

func (c *workerServiceClient) RunServer(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[ServerArgs, ServerStatus], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &WorkerService_ServiceDesc.Streams[0], WorkerService_RunServer_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[ServerArgs, ServerStatus]{ClientStream: stream}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_RunServerClient = grpc.BidiStreamingClient[ServerArgs, ServerStatus]

func (c *workerServiceClient) RunClient(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[ClientArgs, ClientStatus], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &WorkerService_ServiceDesc.Streams[1], WorkerService_RunClient_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[ClientArgs, ClientStatus]{ClientStream: stream}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_RunClientClient = grpc.BidiStreamingClient[ClientArgs, ClientStatus]

func (c *workerServiceClient) CoreCount(ctx context.Context, in *CoreRequest, opts ...grpc.CallOption) (*CoreResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(CoreResponse)
	err := c.cc.Invoke(ctx, WorkerService_CoreCount_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *workerServiceClient) QuitWorker(ctx context.Context, in *Void, opts ...grpc.CallOption) (*Void, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(Void)
	err := c.cc.Invoke(ctx, WorkerService_QuitWorker_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WorkerServiceServer is the server API for WorkerService service.
// All implementations must embed UnimplementedWorkerServiceServer
// for forward compatibility.
type WorkerServiceServer interface {
	// Start server with specified workload.
	// First request sent specifies the ServerConfig followed by ServerStatus
	// response. After that, a "Mark" can be sent anytime to request the latest
	// stats. Closing the stream will initiate shutdown of the test server
	// and once the shutdown has finished, the OK status is sent to terminate
	// this RPC.
	RunServer(grpc.BidiStreamingServer[ServerArgs, ServerStatus]) error
	// Start client with specified workload.
	// First request sent specifies the ClientConfig followed by ClientStatus
	// response. After that, a "Mark" can be sent anytime to request the latest
	// stats. Closing the stream will initiate shutdown of the test client
	// and once the shutdown has finished, the OK status is sent to terminate
	// this RPC.
	RunClient(grpc.BidiStreamingServer[ClientArgs, ClientStatus]) error
	// Just return the core count - unary call
	CoreCount(context.Context, *CoreRequest) (*CoreResponse, error)
	// Quit this worker
	QuitWorker(context.Context, *Void) (*Void, error)
	mustEmbedUnimplementedWorkerServiceServer()
}

// UnimplementedWorkerServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedWorkerServiceServer struct{}

func (UnimplementedWorkerServiceServer) RunServer(grpc.BidiStreamingServer[ServerArgs, ServerStatus]) error {
	return status.Errorf(codes.Unimplemented, "method RunServer not implemented")
}
func (UnimplementedWorkerServiceServer) RunClient(grpc.BidiStreamingServer[ClientArgs, ClientStatus]) error {
	return status.Errorf(codes.Unimplemented, "method RunClient not implemented")
}
func (UnimplementedWorkerServiceServer) CoreCount(context.Context, *CoreRequest) (*CoreResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CoreCount not implemented")
}
func (UnimplementedWorkerServiceServer) QuitWorker(context.Context, *Void) (*Void, error) {
	return nil, status.Errorf(codes.Unimplemented, "method QuitWorker not implemented")
}
func (UnimplementedWorkerServiceServer) mustEmbedUnimplementedWorkerServiceServer() {}
func (UnimplementedWorkerServiceServer) testEmbeddedByValue()                       {}

// UnsafeWorkerServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to WorkerServiceServer will
// result in compilation errors.
type UnsafeWorkerServiceServer interface {
	mustEmbedUnimplementedWorkerServiceServer()
}

func RegisterWorkerServiceServer(s grpc.ServiceRegistrar, srv WorkerServiceServer) {
	// If the following call pancis, it indicates UnimplementedWorkerServiceServer was embedded by pointer
	// and is nil.  This will cause panics if an unimplemented method is ever invoked, so we test this
	// at initialization time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&WorkerService_ServiceDesc, srv)
}

func _WorkerService_RunServer_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(WorkerServiceServer).RunServer(&grpc.GenericServerStream[ServerArgs, ServerStatus]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_RunServerServer = grpc.BidiStreamingServer[ServerArgs, ServerStatus]

func _WorkerService_RunClient_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(WorkerServiceServer).RunClient(&grpc.GenericServerStream[ClientArgs, ClientStatus]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type WorkerService_RunClientServer = grpc.BidiStreamingServer[ClientArgs, ClientStatus]

func _WorkerService_CoreCount_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CoreRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).CoreCount(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerService_CoreCount_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).CoreCount(ctx, req.(*CoreRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _WorkerService_QuitWorker_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Void)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(WorkerServiceServer).QuitWorker(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: WorkerService_QuitWorker_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(WorkerServiceServer).QuitWorker(ctx, req.(*Void))
	}
	return interceptor(ctx, in, info, handler)
}

// WorkerService_ServiceDesc is the grpc.ServiceDesc for WorkerService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var WorkerService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.testing.WorkerService",
	HandlerType: (*WorkerServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CoreCount",
			Handler:    _WorkerService_CoreCount_Handler,
		},
		{
			MethodName: "QuitWorker",
			Handler:    _WorkerService_QuitWorker_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "RunServer",
			Handler:       _WorkerService_RunServer_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
		{
			StreamName:    "RunClient",
			Handler:       _WorkerService_RunClient_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "grpc/testing/worker_service.proto",
}
