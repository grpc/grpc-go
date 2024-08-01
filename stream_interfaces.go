/*
 *
 * Copyright 2024 gRPC authors.
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

package grpc

// ServerStreamingClient represents the client side of a server-streaming (one
// request, many responses) RPC. It is generic over the type of the response
// message. It is used in generated code.
type ServerStreamingClient[Res any] interface {
	// Recv receives a message from the server. The message type is determined
	// by the Res type parameter. This method blocks until a message is received
	// or an error occurs.
	Recv() (*Res, error)

	// ClientStream represents the basic client-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ClientStream
}

// ServerStreamingServer represents the server side of a server-streaming (one
// request, many responses) RPC. It is generic over the type of the response
// message. It is used in generated code.
type ServerStreamingServer[Res any] interface {
	// Send sends a message to the client. The message type is determined by
	// the Res type parameter. This method blocks until the message is sent
	// or an error occurs.
	Send(*Res) error

	// ServerStream represents the basic server-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ServerStream
}

// ClientStreamingClient represents the client side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message. It is used in
// generated code.
type ClientStreamingClient[Req any, Res any] interface {
	// Send sends a message to the server. The message type is determined by
	// the Req type parameter. This method blocks until the message is sent
	// or an error occurs.
	Send(*Req) error

	// CloseAndRecv closes the sending side of the stream and receives the
	// unary response from the server. The response message type is determined
	// by the Res type parameter.
	CloseAndRecv() (*Res, error)

	// ClientStream represents the basic client-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ClientStream
}

// ClientStreamingServer represents the server side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message. It is used in
// generated code.
type ClientStreamingServer[Req any, Res any] interface {
	// Recv receives a message from the client. The message type is determined
	// by the Req type parameter. This method blocks until a message is received
	// or an error occurs.
	Recv() (*Req, error)

	// SendAndClose sends the unary response to the client and closes the stream.
	// The response message type is determined by the Res type parameter.
	SendAndClose(*Res) error

	// ServerStream represents the basic server-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ServerStream
}

// BidiStreamingClient represents the client side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream. It is
// used in generated code.
type BidiStreamingClient[Req any, Res any] interface {
	// Send sends a message to the server. The message type is determined by
	// the Req type parameter. This method blocks until the message is sent
	// or an error occurs.
	Send(*Req) error

	// Recv receives a message from the server. The message type is determined
	// by the Res type parameter. This method blocks until a message is received
	// or an error occurs.
	Recv() (*Res, error)

	// ClientStream represents the basic client-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ClientStream
}

// BidiStreamingServer represents the server side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream. It is
// used in generated code.
type BidiStreamingServer[Req any, Res any] interface {
	// Recv receives a message from the client. The message type is determined
	// by the Req type parameter. This method blocks until a message is received
	// or an error occurs.
	Recv() (*Req, error)

	// Send sends a message to the client. The message type is determined by
	// the Res type parameter. This method blocks until the message is sent
	// or an error occurs.
	Send(*Res) error

	// ServerStream represents the basic server-side streaming operations.
	// It provides methods like SendMsg, RecvMsg, etc., for interacting with
	// the streaming RPC.
	ServerStream
}

// GenericClientStream implements the ServerStreamingClient, ClientStreamingClient,
// and BidiStreamingClient interfaces. It is used in generated code.
type GenericClientStream[Req any, Res any] struct {
	ClientStream
}

var _ ServerStreamingClient[string] = (*GenericClientStream[int, string])(nil)
var _ ClientStreamingClient[int, string] = (*GenericClientStream[int, string])(nil)
var _ BidiStreamingClient[int, string] = (*GenericClientStream[int, string])(nil)

// Send pushes one message into the stream of requests to be consumed by the
// server. The type of message which can be sent is determined by the Req type
// parameter of the GenericClientStream receiver.
func (x *GenericClientStream[Req, Res]) Send(m *Req) error {
	return x.ClientStream.SendMsg(m)
}

// Recv reads one message from the stream of responses generated by the server.
// The type of the message returned is determined by the Res type parameter
// of the GenericClientStream receiver.
func (x *GenericClientStream[Req, Res]) Recv() (*Res, error) {
	m := new(Res)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// CloseAndRecv closes the sending side of the stream, then receives the unary
// response from the server. The type of message which it returns is determined
// by the Res type parameter of the GenericClientStream receiver.
func (x *GenericClientStream[Req, Res]) CloseAndRecv() (*Res, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(Res)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// GenericServerStream implements the ServerStreamingServer, ClientStreamingServer,
// and BidiStreamingServer interfaces. It is used in generated code.
type GenericServerStream[Req any, Res any] struct {
	ServerStream
}

var _ ServerStreamingServer[string] = (*GenericServerStream[int, string])(nil)
var _ ClientStreamingServer[int, string] = (*GenericServerStream[int, string])(nil)
var _ BidiStreamingServer[int, string] = (*GenericServerStream[int, string])(nil)

// Send pushes one message into the stream of responses to be consumed by the
// client. The type of message which can be sent is determined by the Res
// type parameter of the serverStreamServer receiver.
func (x *GenericServerStream[Req, Res]) Send(m *Res) error {
	return x.ServerStream.SendMsg(m)
}

// SendAndClose pushes the unary response to the client. The type of message
// which can be sent is determined by the Res type parameter of the
// clientStreamServer receiver.
func (x *GenericServerStream[Req, Res]) SendAndClose(m *Res) error {
	return x.ServerStream.SendMsg(m)
}

// Recv reads one message from the stream of requests generated by the client.
// The type of the message returned is determined by the Req type parameter
// of the clientStreamServer receiver.
func (x *GenericServerStream[Req, Res]) Recv() (*Req, error) {
	m := new(Req)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}
