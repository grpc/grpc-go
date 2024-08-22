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
// If an error occurs on the stream, it will be compatible with the status
// package.
type ServerStreamingClient[Res any] interface {
	// Recv receives the next message from the server. The client can repeatedly
	// call Recv to read messages from the server-to-client response stream.
	// Recv returns (nil, io.EOF) once the server-to-client stream is completely
	// read.
	Recv() (*Res, error)
	ClientStream
}

// ServerStreamingServer represents the server side of a server-streaming (one
// request, many responses) RPC. It is generic over the type of the response
// message. It is used in generated code.
// If an error occurs on the stream, it will be compatible with the status
// package. End-of-stream for the server-to-client stream is indicated by the
// return of the handler method.
type ServerStreamingServer[Res any] interface {
	// Send can send a stream of messages to the client. Server handler may call
	// Send multiple times to send multiple messages to the client.
	Send(*Res) error
	ServerStream
}

// ClientStreamingClient represents the client side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message. It is used in
// generated code.
// If an error occurs on the stream, it will be compatible with the status
// package.
type ClientStreamingClient[Req any, Res any] interface {
	// Send sends a request message to the server. The client can repeatedly
	// call Send to send messages as part of the client-to-server request stream.
	Send(*Req) error

	// CloseAndRecv closes the client-to-server request stream and waits for the
	// server's unary response. This method must be called once and only once
	// after sending all request messages to close the stream and receive the
	// final response from the server.
	CloseAndRecv() (*Res, error)
	ClientStream
}

// ClientStreamingServer represents the server side of a client-streaming (many
// requests, one response) RPC. It is generic over both the type of the request
// message stream and the type of the unary response message. It is used in
// generated code.
// If an error occurs on the stream, it will be compatible with the status
// package.
type ClientStreamingServer[Req any, Res any] interface {
	// Recv reads a request message from the client. This method can be called
	// repeatedly to receive the full stream of messages from the client. Recv
	// returns (nil, io.EOF) once the end of the stream is reached.
	Recv() (*Req, error)

	// SendAndClose sends a single response message to the client and closes the
	// stream. This method must be called once and only once after all request
	// messages have been processed. No further methods should be called after
	// SendAndClose.
	SendAndClose(*Res) error
	ServerStream
}

// BidiStreamingClient represents the client side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream. It is
// used in generated code.
// If an error occurs on the stream, it will be compatible with the status
// package. End-of-stream for the client-to-server stream can be indicated by
// calling the CloseSend method.
type BidiStreamingClient[Req any, Res any] interface {
	// Send sends a message to the server. This method can be called repeatedly
	// to send messages as part of the client-to-server request stream.
	Send(*Req) error

	// Recv receives the next message from the server's response stream. This
	// method can be called repeatedly to receive all messages sent by the server.
	// Recv returns (nil, io.EOF) once the server-to-client stream is completely
	// read.
	Recv() (*Res, error)
	ClientStream
}

// BidiStreamingServer represents the server side of a bidirectional-streaming
// (many requests, many responses) RPC. It is generic over both the type of the
// request message stream and the type of the response message stream. It is
// used in generated code.
// If an error occurs on the stream, it will be compatible with the status
// package. The end of the response stream is indicated by the return of the
// bidi method handler.
type BidiStreamingServer[Req any, Res any] interface {
	// Recv receives a request message from the client. The server-side handler
	// can repeatedly call Recv to read the request message stream. Recv returns
	// (nil, io.EOF) once the end of the client-to-server stream is reached.
	Recv() (*Req, error)

	// Send sends a response message to the client. The server-side handler can
	// repeatedly call Send to write to the server-to-client message stream.
	Send(*Res) error
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
