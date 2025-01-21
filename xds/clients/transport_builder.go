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
)

// TransportBuilder is an interface for building a new xDS transport.
type TransportBuilder interface {
	// Build creates a new xDS transport for the provided Server Config.
	Build(ServerConfig ServerConfig) (Transport, error)
}

// Transport provides the functionality to communicate with an xDS server using
// streaming calls.
type Transport interface {
	// NewStream creates a new streaming call to the xDS server for
	// specified method name. The returned Streaming interface can be used
	// to send and receive messages on the stream.
	NewStream(context.Context, string) (Stream, error)

	// Close closes the underlying connection and cleans up any resources used
	// by the Transport.
	Close() error
}

// Stream is an interface that provides a way to send and receive
// messages on a stream. Messages are represented as a byte slice ([]byte).
type Stream interface {
	// Send sends the provided message on the stream.
	Send([]byte) error

	// Recv block until the next message is received on the stream.
	Recv() ([]byte, error)
}
