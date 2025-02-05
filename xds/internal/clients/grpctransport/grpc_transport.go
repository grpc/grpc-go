/*
 *
 * Copyright 2025 gRPC authors.
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

// Package grpctransport provides an implementation of the
// clients.TransportBuilder interface using gRPC.
package grpctransport

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/clients"
)

// ServerConfigExtension holds the settings for connecting to an xDS management server
// using gRPC.
type ServerConfigExtension struct {
	// Credentials is the credential bundle containing the gRPC credentials for
	// connecting to the xDS management server.
	Credentials credentials.Bundle
}

// Builder provides a way to build a gRPC-based transport to an xDS management
// server.
type Builder struct{}

// Build creates a new gRPC-based transport to an xDS management server using
// the provided clients.ServerConfig. This involves creating a
// grpc.ClientConn to the server using the provided credentials and server URI.
//
// If any of ServerURI or Extensions of `sc` are not present, Build() will return
// an error.
func (b *Builder) Build(sc clients.ServerConfig) (clients.Transport, error) {
	if sc.ServerURI == "" {
		return nil, fmt.Errorf("xds: ServerConfig's ServerURI field cannot be empty")
	}
	if sc.Extensions == nil {
		return nil, fmt.Errorf("xds: ServerConfig's Extensions field cannot be nil for gRPC transport")
	}
	gtsce, ok := sc.Extensions.(ServerConfigExtension)
	if !ok {
		return nil, fmt.Errorf("xds: ServerConfig's Extensions field cannot be anything other than grpctransport.ServerConfigExtension for gRPC transport")
	}
	if gtsce.Credentials == nil {
		return nil, fmt.Errorf("xsd: ServerConfigExtensions's Credentials field cannot be nil for gRPC transport")
	}

	// TODO: Incorporate reference count map for existing transports and
	// deduplicate transports based on server URI and credentials so that
	// transport channel to same server can be shared between xDS and LRS
	// client.

	// Dial the xDS management server with the provided credentials, server URI,
	// and a static keepalive configuration that is common across gRPC language
	// implementations.
	kpCfg := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    5 * time.Minute,
		Timeout: 20 * time.Second,
	})
	cc, err := grpc.NewClient(sc.ServerURI, kpCfg, grpc.WithCredentialsBundle(gtsce.Credentials))
	if err != nil {
		return nil, fmt.Errorf("error creating grpc client for server uri %s, %v", sc.ServerURI, err)
	}
	cc.Connect()

	return &grpcTransport{cc: cc}, nil
}

type grpcTransport struct {
	cc *grpc.ClientConn
}

// NewStream creates a new gRPC stream to the xDS management server for the
// specified method.  The returned Stream interface can be used to send and
// receive messages on the stream.
func (g *grpcTransport) NewStream(ctx context.Context, method string) (clients.Stream, error) {
	s, err := g.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: method, ClientStreams: true, ServerStreams: true}, method)
	if err != nil {
		return nil, err
	}
	return &stream{stream: s}, nil
}

type stream struct {
	stream grpc.ClientStream
}

// Send sends a message to the xDS management server.
func (s *stream) Send(msg []byte) error {
	return s.stream.SendMsg(msg)
}

// Recv receives a message from the xDS management server.
func (s *stream) Recv() ([]byte, error) {
	var typedRes []byte
	err := s.stream.RecvMsg(&typedRes)
	if err != nil {
		return typedRes, err
	}
	return typedRes, nil
}

// Close closes the gRPC stream to the xDS management server.
func (g *grpcTransport) Close() error {
	return g.cc.Close()
}
