/*
 *
 * Copyright 2020 gRPC authors.
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

package version

import (
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/version/common"
)

var (
	m = make(map[TransportAPI]ClientBuilder)
)

// RegisterClientBuilder registers a client builder for xDS transport protocol
// version specified by b.Version().
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple builders are
// registered for the same version, the one registered last will take effect.
func RegisterClientBuilder(b ClientBuilder) {
	m[b.Version()] = b
}

// GetClientBuilder returns the client builder registered for the provided xDS
// transport API version.
func GetClientBuilder(version TransportAPI) ClientBuilder {
	if b, ok := m[version]; ok {
		return b
	}
	return nil
}

// BuildOptions contains options to be passed to client builders.
type BuildOptions struct {
	// Parent is a top-level xDS client or server which has the intelligence to
	// take appropriate action based on xDS responses received from the
	// management server.
	Parent common.UpdateHandler
	// NodeProto contains the Node proto to be used in xDS requests. The actual
	// type depends on the transport protocol version used.
	NodeProto proto.Message
	// Backoff returns the amount of time to backoff before retrying broken
	// streams.
	Backoff func(int) time.Duration
	// Logger provides enhanced logging capabilities.
	Logger *grpclog.PrefixLogger
}

// ClientBuilder creates an xDS client for a specific xDS transport protocol
// version.
type ClientBuilder interface {
	// Build builds a transport protocol specific implementation of the xDS
	// client based on the provided clientConn to the management server and the
	// provided options.
	Build(*grpc.ClientConn, BuildOptions) (APIClient, error)
	// Version returns the xDS transport protocol version used by clients build
	// using this builder.
	Version() TransportAPI
}

// APIClient represents the functionality provided by transport protocol
// version specific implementations of the xDS client.
type APIClient interface {
	// AddWatch adds a watch for an xDS resource given its type and name.
	AddWatch(resourceType, resourceName string)
	// RemoveWatch cancels an already registered watch for an xDS resource
	// given its type and name.
	RemoveWatch(resourceType, resourceName string)
	// Close cleans up resources allocated by the API client.
	Close()
}
