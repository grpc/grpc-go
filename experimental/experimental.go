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

// Package experimental is a collection of experimental features that might
// have some rough edges to them. Housing experimental features in this package
// results in a user accessing these APIs as `experimental.Foo`, thereby making
// it explicit that the feature is experimental and using them in production
// code is at their own risk.
//
// All APIs in this package are experimental.
package experimental

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
)

// WithRecvBufferPool returns a grpc.DialOption that configures the use of
// bufferPool for parsing incoming messages on a grpc.ClientConn. Depending on
// the application's workload, this could result in reduced memory allocation.
//
// If you are unsure about how to implement a memory pool but want to utilize
// one, begin with grpc.NewSharedBufferPool.
//
// Note: The shared buffer pool feature will not be active if any of the
// following options are used: WithStatsHandler, EnableTracing, or binary
// logging. In such cases, the shared buffer pool will be ignored.
//
// Note: It is not recommended to use the shared buffer pool when compression is
// enabled.
func WithRecvBufferPool(bufferPool grpc.SharedBufferPool) grpc.DialOption {
	return internal.WithRecvBufferPool.(func(grpc.SharedBufferPool) grpc.DialOption)(bufferPool)
}

// RecvBufferPool returns a grpc.ServerOption that configures the server to use
// the provided shared buffer pool for parsing incoming messages. Depending on
// the application's workload, this could result in reduced memory allocation.
//
// If you are unsure about how to implement a memory pool but want to utilize
// one, begin with grpc.NewSharedBufferPool.
//
// Note: The shared buffer pool feature will not be active if any of the
// following options are used: StatsHandler, EnableTracing, or binary logging.
// In such cases, the shared buffer pool will be ignored.
//
// Note: It is not recommended to use the shared buffer pool when compression is
// enabled.
func RecvBufferPool(bufferPool grpc.SharedBufferPool) grpc.ServerOption {
	return internal.RecvBufferPool.(func(grpc.SharedBufferPool) grpc.ServerOption)(bufferPool)
}

// ServerStreamClient represents the client side of a server-streaming (one
// request, many responses) RPC. It is generic over the type of the response
// message.
type ServerStreamClient[T any] interface {
	Recv() (*T, error)
	grpc.ClientStream
}

// ServerStreamServer represents the server side of a server-streaming (one
// request, many responses) RPC. It is generic over the type of the response
// message.
type ServerStreamServer[T any] interface {
	Send(*T) error
	grpc.ServerStream
}

// ClientStreamClient represents the client side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message.
type ClientStreamClient[T any, U any] interface {
	Send(*T) error
	CloseAndRecv() (*U, error)
	grpc.ClientStream
}

// ClientStreamServer represents the server side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message.
type ClientStreamServer[T any, U any] interface {
	Recv() (*T, error)
	SendAndClose(*U) error
	grpc.ServerStream
}

// BidiStreamClient represents the client side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream.
type BidiStreamClient[T any, U any] interface {
	Send(*T) error
	Recv() (*U, error)
	grpc.ClientStream
}

// BidiStreamServer represents the server side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream.
type BidiStreamServer[T any, U any] interface {
	Recv() (*T, error)
	Send(*U) error
	grpc.ServerStream
}

// StreamClientImpl implements the ServerStreamClient, ClientStreamClient, and
// BidiStreamClient interfaces.
type StreamClientImpl[T any, U any] struct {
	grpc.ClientStream
}

var _ ServerStreamClient[string] = (*StreamClientImpl[int, string])(nil)
var _ ClientStreamClient[int, string] = (*StreamClientImpl[int, string])(nil)
var _ BidiStreamClient[int, string] = (*StreamClientImpl[int, string])(nil)

// Send pushes one message into the stream of requests to be consumed by the
// server. The type of message which can be sent is determined by the first type
// parameter of the StreamClientImpl receiver.
func (x *StreamClientImpl[T, U]) Send(m *T) error {
	return x.ClientStream.SendMsg(m)
}

// Recv reads one message from the stream of responses generated by the server.
// The type of the message returned is determined by the second type parameter
// of the StreamClientImpl receiver.
func (x *StreamClientImpl[T, U]) Recv() (*U, error) {
	m := new(U)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// CloseAndRecv closes the sending side of the stream, then receives the unary
// response from the server. The type of message which it returns is determined
// by the second type parameter of the StreamClientImpl receiver.
func (x *StreamClientImpl[T, U]) CloseAndRecv() (*U, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(U)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// StreamServerImpl implements the ServerStreamServer, ClientStreamServer, and
// BidiStreamServer interfaces.
type StreamServerImpl[T any, U any] struct {
	grpc.ServerStream
}

var _ ServerStreamServer[string] = (*StreamServerImpl[int, string])(nil)
var _ ClientStreamServer[int, string] = (*StreamServerImpl[int, string])(nil)
var _ BidiStreamServer[int, string] = (*StreamServerImpl[int, string])(nil)

// Send pushes one message into the stream of responses to be consumed by the
// client. The type of message which can be sent is determined by the second type
// parameter of the serverStreamServer receiver.
func (x *StreamServerImpl[T, U]) Send(m *U) error {
	return x.ServerStream.SendMsg(m)
}

// SendAndClose pushes the unary response to the client. The type of message
// which can be sent is determined by the second type parameter of the
// clientStreamServer receiver.
func (x *StreamServerImpl[T, U]) SendAndClose(m *U) error {
	return x.ServerStream.SendMsg(m)
}

// Recv reads one message from the stream of requests generated by the client.
// The type of the message returned is determined by the first type parameter
// of the clientStreamServer receiver.
func (x *StreamServerImpl[T, U]) Recv() (*T, error) {
	m := new(T)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
