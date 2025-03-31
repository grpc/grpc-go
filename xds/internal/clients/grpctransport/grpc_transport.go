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
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/clients"
)

var logger = grpclog.Component("grpctransport")

// ServerIdentifierExtension holds settings for connecting to a gRPC server,
// such as an xDS management or an LRS server.
//
// It must be added as value to the clients.ServerIdentifier.Extensions field.
//
// Example:
// clients.ServerIdentifier{ServerURI: "localhost:5678", Extensions: grpctransport.ServerIdentifierExtension{Credentials: "local"}}
type ServerIdentifierExtension struct {
	// Credentials is name of the credentials to use for this connection to the
	// server.
	//
	// It must be present in Builder.Credentials.
	Credentials string
}

// Builder creates gRPC-based Transports. It must be paired with ServerIdentifiers
// that contain an Extension field of type ServerIdentifierExtension.
type Builder struct {
	// Credentials is a map of credentials names to credentials.Bundle which
	// can be used to connect to the server.
	Credentials map[string]credentials.Bundle

	mu sync.Mutex
	// serverIdentifierMap is a map of clients.ServerIdentifiers in use by the
	// Builder to connect to different servers.
	serverIdentifierMap map[clients.ServerIdentifier]any
}

// NewBuilder provides a builder for creating gRPC-based Transports using
// the credentials from provided map of credentials names to
// credentials.Bundle.
func NewBuilder(credentials map[string]credentials.Bundle) *Builder {
	return &Builder{
		Credentials:         credentials,
		serverIdentifierMap: make(map[clients.ServerIdentifier]any),
	}
}

// Build returns a gRPC-based clients.Transport.
//
// The Extension field of the ServerIdentifier must be a ServerIdentifierExtension.
func (b *Builder) Build(si clients.ServerIdentifier) (clients.Transport, error) {
	if si.ServerURI == "" {
		return nil, fmt.Errorf("grpctransport: ServerURI is not set in ServerIdentifier")
	}
	if si.Extensions == nil {
		return nil, fmt.Errorf("grpctransport: Extensions is not set in ServerIdentifier")
	}
	sce, ok := si.Extensions.(ServerIdentifierExtension)
	if !ok {
		return nil, fmt.Errorf("grpctransport: Extensions field is %T, but must be %T in ServerIdentifier", si.Extensions, ServerIdentifierExtension{})
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var credentialsToUse credentials.Bundle
	if credentialsToUse, ok = b.Credentials[sce.Credentials]; !ok {
		return nil, fmt.Errorf("grptransport: Credentials %s in ServerIdentifierExtension is not present in the Builder", sce.Credentials)
	}

	if value, ok := b.serverIdentifierMap[si]; ok {
		if logger.V(2) {
			logger.Info("Reusing existing connection to the server for ServerIdentifier: %v", si)
		}
		return value.(*grpcTransport), nil
	}

	if logger.V(2) {
		logger.Info("Creating a new connection to the server for ServerIdentifier: %v", si)
	}
	// Create a new gRPC client/channel for the server with the provided
	// credentials, server URI, and a byte codec to send and receive messages.
	// Also set a static keepalive configuration that is common across gRPC
	// language implementations.
	kpCfg := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    5 * time.Minute,
		Timeout: 20 * time.Second,
	})
	cc, err := grpc.NewClient(si.ServerURI, kpCfg, grpc.WithCredentialsBundle(credentialsToUse), grpc.WithDefaultCallOptions(grpc.ForceCodec(&byteCodec{})))
	if err != nil {
		return nil, fmt.Errorf("grpctransport: failed to create transport to server %q: %v", si.ServerURI, err)
	}
	tc := &grpcTransport{cc: cc}
	// Add the newly created transport to the map to re-use the connection.
	b.serverIdentifierMap[si] = tc

	return tc, nil
}

type grpcTransport struct {
	cc *grpc.ClientConn
}

// NewStream creates a new gRPC stream to the server for the specified method.
func (g *grpcTransport) NewStream(ctx context.Context, method string) (clients.Stream, error) {
	s, err := g.cc.NewStream(ctx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, method)
	if err != nil {
		return nil, err
	}
	return &stream{stream: s}, nil
}

// Close closes the gRPC channel to the server.
func (g *grpcTransport) Close() error {
	return g.cc.Close()
}

type stream struct {
	stream grpc.ClientStream
}

// Send sends a message to the server.
func (s *stream) Send(msg []byte) error {
	return s.stream.SendMsg(msg)
}

// Recv receives a message from the server.
func (s *stream) Recv() ([]byte, error) {
	var typedRes []byte

	if err := s.stream.RecvMsg(&typedRes); err != nil {
		return nil, err
	}
	return typedRes, nil
}

type byteCodec struct{}

func (c *byteCodec) Marshal(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return nil, fmt.Errorf("grpctransport: message is %T, but must be a []byte", v)
}

func (c *byteCodec) Unmarshal(data []byte, v any) error {
	if b, ok := v.(*[]byte); ok {
		*b = data
		return nil
	}
	return fmt.Errorf("grpctransport: target is %T, but must be *[]byte", v)
}

func (c *byteCodec) Name() string {
	return "grpctransport.byteCodec"
}
