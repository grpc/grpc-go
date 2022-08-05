/*
 *
 * Copyright 2022 gRPC authors.
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
 */

package transport

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/protobuf/types/known/anypb"
)

// versionedTransport performs transport protocol version specific operations
// like creating streams, sending requests, receiving responses etc. It does not
// understand the semantics of the xDS transport protocol. It is intended to be
// controlled by the version agnostic transport implementation which understands
// the semantics of the xDS transport protocol.
type versionedTransport interface {
	adsTransport
	lrsTransport
}

// adsTransport provides Aggregated Discovery Server (ADS) functionality.
type adsTransport interface {
	// NewAggregatedDiscoveryServiceStream returns a new ADS client stream, on
	// the provided ClientConn. The method name used by the newly created stream
	// ADS stream is specific to the underlying transport protocol version.
	NewAggregatedDiscoveryServiceStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error)

	// SendAggregatedDiscoveryServiceRequest constructs a DiscoveryRequest
	// message using the provided arguments and sends it out on the provided
	// ClientStream.  The DiscoveryRequest message used is specific to the
	// underlying transport protocol version.
	SendAggregatedDiscoveryServiceRequest(s grpc.ClientStream, resourceNames []string, resourceURL, version, nonce, errMsg string) error

	// RecvAggregatedDiscoveryServiceResponse reads a DiscoveryResponse message
	// off of the provided stream and returns the values specified in the
	// response. If reading from the stream fails, a non-nil error is to
	// returned.
	//
	// Implementations should not attempt to decode the contents of the
	// resources encoded in the anypb.Any protos, and should simply return them
	// as received, except when reading from the stream fails.
	RecvAggregatedDiscoveryServiceResponse(s grpc.ClientStream) (resources []*anypb.Any, resourceURL, version, nonce string, err error)
}

// lrsTransport provides Load Reporting Service (LRS) functionality.
type lrsTransport interface {
	// NewLoadStatsStream returns a new LRS client stream, on the provided
	// ClientConn. The method name used by the newly created stream is specific
	// to the underlying transport protocol version.
	NewLoadStatsStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error)

	// SendFirstLoadStatsRequest constructs and sends the first LoadStatsRequest
	// message, specific to the underlying transport protocol version, on the
	// LRS stream. This message is expected to carry only the Node proto and no
	// load data.
	SendFirstLoadStatsRequest(s grpc.ClientStream) error

	// RecvFirstLoadStatsResponse reads the first LoadStatsResponse message,
	// specific to the underlying transport protocol version, off of the
	// provided ClientStream. Returns the load reporting interval and the
	// clusters for which load reports are requested.
	//
	// If the SendAllClusters field of the response is to true, the returned
	// clusters is nil.
	RecvFirstLoadStatsResponse(s grpc.ClientStream) (clusters []string, reportingInterval time.Duration, err error)

	// SendLoadStatsRequest constructs and sends a LoadStatsRequest message,
	// specific to the underlying transport protocol version, with the provided
	// load data.
	//
	// This method is expected to be invoked at regular intervals, by the
	// transport version agnostic implementation, to send load data reported
	// since the last time this method was invoked.
	SendLoadStatsRequest(s grpc.ClientStream, loads []*load.Data) error
}
