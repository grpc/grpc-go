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

package clients

import (
	"context"
	"errors"
)

// TransportBuilder provides the functionality to create a communication
// channel to an xDS or LRS server.
//
// Implementations must embed UnimplementedTransportBuilder.
type TransportBuilder interface {
	// Build creates a new Transport instance to the server based on the
	// provided ServerIdentifier.
	Build(serverIdentifier ServerIdentifier) (Transport, error)

	mustEmbedUnimplementedTransportBuilder()
}

// UnimplementedTransportBuilder must be embedded to have forward compatible
// implementations of TransportBuilder.
type UnimplementedTransportBuilder struct{}

// Build returns an unimplemented error.
func (UnimplementedTransportBuilder) Build(ServerIdentifier) (Transport, error) {
	return nil, errors.New("TransportBuilder.Build is not implemented")
}

func (UnimplementedTransportBuilder) mustEmbedUnimplementedTransportBuilder() {}

// Transport provides the functionality to communicate with an xDS or LRS
// server using streaming calls.
//
// Implementations must embed UnimplementedTransport.
type Transport interface {
	// NewStream creates a new streaming call to the server for the specific
	// RPC method name. The returned Stream interface can be used to send and
	// receive messages on the stream.
	NewStream(context.Context, string) (Stream, error)

	// Close closes the Transport.
	Close()

	mustEmbedUnimplementedTransport()
}

// UnimplementedTransport must be embedded to have forward compatible
// implementations of Transport.
type UnimplementedTransport struct{}

// NewStream returns an unimplemented error.
func (UnimplementedTransport) NewStream(context.Context, string) (Stream, error) {
	return nil, errors.New("Transport.NewStream is not implemented")
}

// Close is a no-op implementation of Transport.Close.
func (UnimplementedTransport) Close() {}

func (UnimplementedTransport) mustEmbedUnimplementedTransport() {}

// Stream provides methods to send and receive messages on a stream. Messages
// are represented as a byte slice.
//
// Implementations must embed UnimplementedStream.
type Stream interface {
	// Send sends the provided message on the stream.
	Send([]byte) error

	// Recv blocks until the next message is received on the stream.
	Recv() ([]byte, error)

	mustEmbedUnimplementedStream()
}

// UnimplementedStream must be embedded to have forward compatible
// implementations of Stream.
type UnimplementedStream struct{}

// Send returns an unimplemented error.
func (UnimplementedStream) Send([]byte) error {
	return errors.New("Stream.Send is not implemented")
}

// Recv returns an unimplemented error.
func (UnimplementedStream) Recv() ([]byte, error) {
	return nil, errors.New("Stream.Recv is not implemented")
}

func (UnimplementedStream) mustEmbedUnimplementedStream() {}
