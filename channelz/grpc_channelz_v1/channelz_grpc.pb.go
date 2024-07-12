// Copyright 2018 The gRPC Authors
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

// This file defines an interface for exporting monitoring information
// out of gRPC servers.  See the full design at
// https://github.com/grpc/proposal/blob/master/A14-channelz.md
//
// The canonical version of this proto can be found at
// https://github.com/grpc/grpc-proto/blob/master/grpc/channelz/v1/channelz.proto

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.4.0
// - protoc             v5.27.1
// source: grpc/channelz/v1/channelz.proto

package grpc_channelz_v1

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
	Channelz_GetTopChannels_FullMethodName   = "/grpc.channelz.v1.Channelz/GetTopChannels"
	Channelz_GetServers_FullMethodName       = "/grpc.channelz.v1.Channelz/GetServers"
	Channelz_GetServer_FullMethodName        = "/grpc.channelz.v1.Channelz/GetServer"
	Channelz_GetServerSockets_FullMethodName = "/grpc.channelz.v1.Channelz/GetServerSockets"
	Channelz_GetChannel_FullMethodName       = "/grpc.channelz.v1.Channelz/GetChannel"
	Channelz_GetSubchannel_FullMethodName    = "/grpc.channelz.v1.Channelz/GetSubchannel"
	Channelz_GetSocket_FullMethodName        = "/grpc.channelz.v1.Channelz/GetSocket"
)

// ChannelzClient is the client API for Channelz service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
//
// Channelz is a service exposed by gRPC servers that provides detailed debug
// information.
type ChannelzClient interface {
	// Gets all root channels (i.e. channels the application has directly
	// created). This does not include subchannels nor non-top level channels.
	GetTopChannels(ctx context.Context, in *GetTopChannelsRequest, opts ...grpc.CallOption) (*GetTopChannelsResponse, error)
	// Gets all servers that exist in the process.
	GetServers(ctx context.Context, in *GetServersRequest, opts ...grpc.CallOption) (*GetServersResponse, error)
	// Returns a single Server, or else a NOT_FOUND code.
	GetServer(ctx context.Context, in *GetServerRequest, opts ...grpc.CallOption) (*GetServerResponse, error)
	// Gets all server sockets that exist in the process.
	GetServerSockets(ctx context.Context, in *GetServerSocketsRequest, opts ...grpc.CallOption) (*GetServerSocketsResponse, error)
	// Returns a single Channel, or else a NOT_FOUND code.
	GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*GetChannelResponse, error)
	// Returns a single Subchannel, or else a NOT_FOUND code.
	GetSubchannel(ctx context.Context, in *GetSubchannelRequest, opts ...grpc.CallOption) (*GetSubchannelResponse, error)
	// Returns a single Socket or else a NOT_FOUND code.
	GetSocket(ctx context.Context, in *GetSocketRequest, opts ...grpc.CallOption) (*GetSocketResponse, error)
}

type channelzClient struct {
	cc grpc.ClientConnInterface
}

func NewChannelzClient(cc grpc.ClientConnInterface) ChannelzClient {
	return &channelzClient{cc}
}

func (c *channelzClient) GetTopChannels(ctx context.Context, in *GetTopChannelsRequest, opts ...grpc.CallOption) (*GetTopChannelsResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetTopChannelsResponse)
	err := c.cc.Invoke(ctx, Channelz_GetTopChannels_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetServers(ctx context.Context, in *GetServersRequest, opts ...grpc.CallOption) (*GetServersResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetServersResponse)
	err := c.cc.Invoke(ctx, Channelz_GetServers_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetServer(ctx context.Context, in *GetServerRequest, opts ...grpc.CallOption) (*GetServerResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetServerResponse)
	err := c.cc.Invoke(ctx, Channelz_GetServer_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetServerSockets(ctx context.Context, in *GetServerSocketsRequest, opts ...grpc.CallOption) (*GetServerSocketsResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetServerSocketsResponse)
	err := c.cc.Invoke(ctx, Channelz_GetServerSockets_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*GetChannelResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetChannelResponse)
	err := c.cc.Invoke(ctx, Channelz_GetChannel_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetSubchannel(ctx context.Context, in *GetSubchannelRequest, opts ...grpc.CallOption) (*GetSubchannelResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetSubchannelResponse)
	err := c.cc.Invoke(ctx, Channelz_GetSubchannel_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *channelzClient) GetSocket(ctx context.Context, in *GetSocketRequest, opts ...grpc.CallOption) (*GetSocketResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(GetSocketResponse)
	err := c.cc.Invoke(ctx, Channelz_GetSocket_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ChannelzServer is the server API for Channelz service.
// All implementations should embed UnimplementedChannelzServer
// for forward compatibility
//
// Channelz is a service exposed by gRPC servers that provides detailed debug
// information.
type ChannelzServer interface {
	// Gets all root channels (i.e. channels the application has directly
	// created). This does not include subchannels nor non-top level channels.
	GetTopChannels(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error)
	// Gets all servers that exist in the process.
	GetServers(context.Context, *GetServersRequest) (*GetServersResponse, error)
	// Returns a single Server, or else a NOT_FOUND code.
	GetServer(context.Context, *GetServerRequest) (*GetServerResponse, error)
	// Gets all server sockets that exist in the process.
	GetServerSockets(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error)
	// Returns a single Channel, or else a NOT_FOUND code.
	GetChannel(context.Context, *GetChannelRequest) (*GetChannelResponse, error)
	// Returns a single Subchannel, or else a NOT_FOUND code.
	GetSubchannel(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error)
	// Returns a single Socket or else a NOT_FOUND code.
	GetSocket(context.Context, *GetSocketRequest) (*GetSocketResponse, error)
}

// UnimplementedChannelzServer should be embedded to have forward compatible implementations.
type UnimplementedChannelzServer struct {
}

func (UnimplementedChannelzServer) GetTopChannels(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTopChannels not implemented")
}
func (UnimplementedChannelzServer) GetServers(context.Context, *GetServersRequest) (*GetServersResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServers not implemented")
}
func (UnimplementedChannelzServer) GetServer(context.Context, *GetServerRequest) (*GetServerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServer not implemented")
}
func (UnimplementedChannelzServer) GetServerSockets(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServerSockets not implemented")
}
func (UnimplementedChannelzServer) GetChannel(context.Context, *GetChannelRequest) (*GetChannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetChannel not implemented")
}
func (UnimplementedChannelzServer) GetSubchannel(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSubchannel not implemented")
}
func (UnimplementedChannelzServer) GetSocket(context.Context, *GetSocketRequest) (*GetSocketResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSocket not implemented")
}

// UnsafeChannelzServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ChannelzServer will
// result in compilation errors.
type UnsafeChannelzServer interface {
	mustEmbedUnimplementedChannelzServer()
}

func RegisterChannelzServer(s grpc.ServiceRegistrar, srv ChannelzServer) {
	s.RegisterService(&Channelz_ServiceDesc, srv)
}

func _Channelz_GetTopChannels_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTopChannelsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetTopChannels(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetTopChannels_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetTopChannels(ctx, req.(*GetTopChannelsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetServers_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServersRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetServers(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetServers_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetServers(ctx, req.(*GetServersRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetServer_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServerRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetServer(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetServer_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetServer(ctx, req.(*GetServerRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetServerSockets_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServerSocketsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetServerSockets(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetServerSockets_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetServerSockets(ctx, req.(*GetServerSocketsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetChannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetChannel_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetChannel(ctx, req.(*GetChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetSubchannel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSubchannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetSubchannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetSubchannel_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetSubchannel(ctx, req.(*GetSubchannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Channelz_GetSocket_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSocketRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ChannelzServer).GetSocket(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Channelz_GetSocket_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ChannelzServer).GetSocket(ctx, req.(*GetSocketRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Channelz_ServiceDesc is the grpc.ServiceDesc for Channelz service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Channelz_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.channelz.v1.Channelz",
	HandlerType: (*ChannelzServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetTopChannels",
			Handler:    _Channelz_GetTopChannels_Handler,
		},
		{
			MethodName: "GetServers",
			Handler:    _Channelz_GetServers_Handler,
		},
		{
			MethodName: "GetServer",
			Handler:    _Channelz_GetServer_Handler,
		},
		{
			MethodName: "GetServerSockets",
			Handler:    _Channelz_GetServerSockets_Handler,
		},
		{
			MethodName: "GetChannel",
			Handler:    _Channelz_GetChannel_Handler,
		},
		{
			MethodName: "GetSubchannel",
			Handler:    _Channelz_GetSubchannel_Handler,
		},
		{
			MethodName: "GetSocket",
			Handler:    _Channelz_GetSocket_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "grpc/channelz/v1/channelz.proto",
}
