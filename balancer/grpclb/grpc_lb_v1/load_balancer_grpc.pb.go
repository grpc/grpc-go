// Copyright 2015 The gRPC Authors
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

// This file defines the GRPCLB LoadBalancing protocol.
//
// The canonical version of this proto can be found at
// https://github.com/grpc/grpc-proto/blob/master/grpc/lb/v1/load_balancer.proto

// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.14.0
// source: grpc/lb/v1/load_balancer.proto

package grpc_lb_v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// LoadBalancerClient is the client API for LoadBalancer service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type LoadBalancerClient interface {
	// Bidirectional rpc to get a list of servers.
	BalanceLoad(ctx context.Context, opts ...grpc.CallOption) (LoadBalancer_BalanceLoadClient, error)
}

type loadBalancerClient struct {
	cc grpc.ClientConnInterface
}

func NewLoadBalancerClient(cc grpc.ClientConnInterface) LoadBalancerClient {
	return &loadBalancerClient{cc}
}

func (c *loadBalancerClient) BalanceLoad(ctx context.Context, opts ...grpc.CallOption) (LoadBalancer_BalanceLoadClient, error) {
	stream, err := c.cc.NewStream(ctx, &LoadBalancer_ServiceDesc.Streams[0], LoadBalancer_BalanceLoad_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &loadBalancerBalanceLoadClient{stream}
	return x, nil
}

type LoadBalancer_BalanceLoadClient interface {
	Send(*LoadBalanceRequest) error
	Recv() (*LoadBalanceResponse, error)
	grpc.ClientStream
}

type loadBalancerBalanceLoadClient struct {
	grpc.ClientStream
}

func (x *loadBalancerBalanceLoadClient) Send(m *LoadBalanceRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *loadBalancerBalanceLoadClient) Recv() (*LoadBalanceResponse, error) {
	m := new(LoadBalanceResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadBalancerServer is the server API for LoadBalancer service.
// All implementations should embed UnimplementedLoadBalancerServer
// for forward compatibility
type LoadBalancerServer interface {
	// Bidirectional rpc to get a list of servers.
	BalanceLoad(LoadBalancer_BalanceLoadServer) error
}

// UnimplementedLoadBalancerServer should be embedded to have forward compatible implementations.
type UnimplementedLoadBalancerServer struct {
}

func (UnimplementedLoadBalancerServer) BalanceLoad(LoadBalancer_BalanceLoadServer) error {
	return status.Errorf(codes.Unimplemented, "method BalanceLoad not implemented")
}

// UnsafeLoadBalancerServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to LoadBalancerServer will
// result in compilation errors.
type UnsafeLoadBalancerServer interface {
	mustEmbedUnimplementedLoadBalancerServer()
}

func RegisterLoadBalancerServer(s grpc.ServiceRegistrar, srv LoadBalancerServer) {
	s.RegisterService(&LoadBalancer_ServiceDesc, srv)
}

func _LoadBalancer_BalanceLoad_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(LoadBalancerServer).BalanceLoad(&loadBalancerBalanceLoadServer{stream})
}

type LoadBalancer_BalanceLoadServer interface {
	Send(*LoadBalanceResponse) error
	Recv() (*LoadBalanceRequest, error)
	grpc.ServerStream
}

type loadBalancerBalanceLoadServer struct {
	grpc.ServerStream
}

func (x *loadBalancerBalanceLoadServer) Send(m *LoadBalanceResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *loadBalancerBalanceLoadServer) Recv() (*LoadBalanceRequest, error) {
	m := new(LoadBalanceRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadBalancer_ServiceDesc is the grpc.ServiceDesc for LoadBalancer service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var LoadBalancer_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.lb.v1.LoadBalancer",
	HandlerType: (*LoadBalancerServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "BalanceLoad",
			Handler:       _LoadBalancer_BalanceLoad_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "grpc/lb/v1/load_balancer.proto",
}

const (
	LoadBalancer_BalanceLoad_FullMethodName = "/grpc.lb.v1.LoadBalancer/BalanceLoad"
)
