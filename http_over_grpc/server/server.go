/*
 *
 * Copyright 2019 gRPC authors.
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

package http_over_grpc

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/http_over_grpc/http_over_grpc_proto"
	"google.golang.org/grpc/status"
)

// TLSConfig represents information required to communicate TLS with HTTP
// servers.
type TLSConfig struct {
	// CAFile for accessing the HTTP Server.
	CAFile string
	// BearerToken for accessing the HTTP Server.
	BearerToken string
	// ServerName should match the CA PEM.
	ServerName string
	// InsecureSkipVerify controls whether a client verifies the
	// server's certificate chain and host name.
	// If InsecureSkipVerify is true, TLS accepts any certificate
	// presented by the server and any host name in that certificate.
	// In this mode, TLS is susceptible to man-in-the-middle attacks.
	// This should be used only for testing.
	InsecureSkipVerify bool
}

// HTTPOverGRPCServer implements the GkeConnect gRPC service.
type HTTPOverGRPCServer struct {
	client *HTTPClient
}

// NewGKEConnectServer creates a new GKEConnectServer instance.
func NewHTTPOverGRPCServer(tlsConfig *TLSConfig) (*HTTPOverGRPCServer, error) {
	client, err := NewHTTPClient(tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP client: %v", err)
	}
	return &HTTPOverGRPCServer{
		client: client,
	}, nil
}

// HTTPRequest takes an http request as a gRPC message, makes the request and encapsulates
// the HTTP response in a gRPC message.
func (s *HTTPOverGRPCServer) HTTPRequest(ctx context.Context, req *pb.HTTPOverGRPCRequest) (*pb.HTTPOverGRPCReply, error) {
	if req == nil {
		log.Printf("Warning: got nil request.")
		return nil, status.Errorf(codes.InvalidArgument, "request was nil")
	}

	resp, err := s.client.Do(ctx, req)
	if err != nil {
		log.Printf("Warning: request resulted in error: %v", err)
	}
	return resp, err
}
