// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// ProfilingClient is the client API for Profiling service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ProfilingClient interface {
	// Enable allows users to toggle profiling on and off remotely.
	Enable(ctx context.Context, in *EnableRequest, opts ...grpc.CallOption) (*EnableResponse, error)
	// GetStreamStats is used to retrieve an array of stream-level stats from a
	// gRPC client/server.
	GetStreamStats(ctx context.Context, in *GetStreamStatsRequest, opts ...grpc.CallOption) (*GetStreamStatsResponse, error)
}

type profilingClient struct {
	cc grpc.ClientConnInterface
}

func NewProfilingClient(cc grpc.ClientConnInterface) ProfilingClient {
	return &profilingClient{cc}
}

func (c *profilingClient) Enable(ctx context.Context, in *EnableRequest, opts ...grpc.CallOption) (*EnableResponse, error) {
	out := new(EnableResponse)
	err := c.cc.Invoke(ctx, "/grpc.go.profiling.v1alpha.Profiling/Enable", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *profilingClient) GetStreamStats(ctx context.Context, in *GetStreamStatsRequest, opts ...grpc.CallOption) (*GetStreamStatsResponse, error) {
	out := new(GetStreamStatsResponse)
	err := c.cc.Invoke(ctx, "/grpc.go.profiling.v1alpha.Profiling/GetStreamStats", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProfilingServer is the server API for Profiling service.
// All implementations must embed UnimplementedProfilingServer
// for forward compatibility
type ProfilingServer interface {
	// Enable allows users to toggle profiling on and off remotely.
	Enable(context.Context, *EnableRequest) (*EnableResponse, error)
	// GetStreamStats is used to retrieve an array of stream-level stats from a
	// gRPC client/server.
	GetStreamStats(context.Context, *GetStreamStatsRequest) (*GetStreamStatsResponse, error)
	mustEmbedUnimplementedProfilingServer()
}

// UnimplementedProfilingServer must be embedded to have forward compatible implementations.
type UnimplementedProfilingServer struct {
}

func (*UnimplementedProfilingServer) Enable(context.Context, *EnableRequest) (*EnableResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Enable not implemented")
}
func (*UnimplementedProfilingServer) GetStreamStats(context.Context, *GetStreamStatsRequest) (*GetStreamStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetStreamStats not implemented")
}
func (*UnimplementedProfilingServer) mustEmbedUnimplementedProfilingServer() {}

func RegisterProfilingServer(s *grpc.Server, srv ProfilingServer) {
	s.RegisterService(&_Profiling_serviceDesc, srv)
}

func _Profiling_Enable_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(EnableRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ProfilingServer).Enable(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/grpc.go.profiling.v1alpha.Profiling/Enable",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ProfilingServer).Enable(ctx, req.(*EnableRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Profiling_GetStreamStats_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetStreamStatsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ProfilingServer).GetStreamStats(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/grpc.go.profiling.v1alpha.Profiling/GetStreamStats",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ProfilingServer).GetStreamStats(ctx, req.(*GetStreamStatsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Profiling_serviceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.go.profiling.v1alpha.Profiling",
	HandlerType: (*ProfilingServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Enable",
			Handler:    _Profiling_Enable_Handler,
		},
		{
			MethodName: "GetStreamStats",
			Handler:    _Profiling_GetStreamStats_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "profiling/proto/service.proto",
}
