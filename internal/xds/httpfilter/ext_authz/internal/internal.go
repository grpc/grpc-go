/*
 *
 * Copyright 2026 gRPC authors.
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

// Package internal contains functionality internal to the extauthz package.
package internal

import (
	"fmt"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
)

var (
	// RegisterForTesting registers the external authorization HTTP Filter for
	// testing purposes.
	RegisterForTesting func()

	// UnregisterForTesting unregisters the external authorization HTTP Filter for
	// testing purposes.
	UnregisterForTesting func()

	// ParseGRPCServiceConfig parses the gRPC service configuration from the given
	// protobuf message.
	ParseGRPCServiceConfig = func(*v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("extauthz: ParseGRPCServiceConfig not implemented")
	}

	// CreateExtAuthzChannel creates a gRPC client channel to the external
	// authorization server.
	CreateExtAuthzChannel = func(xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		return nil, nil, fmt.Errorf("extauthz: dialing external authorization server not implemented")
	}
)

// ParseGRPCServiceConfigForTesting is a helper function that parses a GrpcService
// proto message into a GRPCServiceConfig. This is a temporary test
// implementation that will be removed once gRFC A102 is implemented.
func ParseGRPCServiceConfigForTesting(grpcService *v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
	if grpcService.GetGoogleGrpc() == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("only google_grpc grpc_service is supported")
	}
	if grpcService.GetGoogleGrpc().GetTargetUri() == "" {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("targetURI must be a non-empty string")
	}

	var initialMD metadata.MD
	if len(grpcService.GetInitialMetadata()) > 0 {
		initialMD = metadata.MD{}
		for _, h := range grpcService.GetInitialMetadata() {
			initialMD.Append(h.GetKey(), h.GetValue())
		}
	}

	sc := xdsresource.GRPCServiceConfig{
		TargetURI:       grpcService.GetGoogleGrpc().GetTargetUri(),
		Timeout:         grpcService.GetTimeout().AsDuration(),
		InitialMetadata: initialMD,
	}
	return sc, nil
}
