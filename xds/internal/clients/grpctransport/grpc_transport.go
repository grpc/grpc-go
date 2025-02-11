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

// ServerConfigExtension holds settings for connecting to a gRPC server,
// such as an xDS management or an LRS server.
type ServerConfigExtension struct {
	// Credentials will be used for all gRPC transports. If it is unset,
	// transport creation will fail.
	Credentials credentials.Bundle
}

// Builder creates gRPC-based Transports. It must be paired with ServerConfigs
// that contain its ServerConfigExtension.
type Builder struct{}

// Build returns a gRPC-based clients.Transport.
//
// The Extension field of the ServerConfig must be a ServerConfigExtension.
func (b *Builder) Build(sc clients.ServerConfig) (clients.Transport, error) {
	if sc.ServerURI == "" {
		return nil, fmt.Errorf("ServerConfig's ServerURI field cannot be empty")
	}
	if sc.Extensions == nil {
		return nil, fmt.Errorf("ServerConfig's Extensions field cannot be nil for gRPC transport")
	}
	gtsce, ok := sc.Extensions.(ServerConfigExtension)
	if !ok {
		return nil, fmt.Errorf("ServerConfig Extensions field is %T, but must be %T", sc.Extensions, ServerConfigExtension{})
	}
	if gtsce.Credentials == nil {
		return nil, fmt.Errorf("ServerConfigExtensions's Credentials field cannot be nil for gRPC transport")
	}

	// TODO: Incorporate reference count map for existing transports and
	// deduplicate transports based on the provided ServerConfig so that
	// transport channel to same server can be shared between xDS and LRS
	// client.

	// Create a new gRPC client/channel for the xDS management server with the
	// provided credentials, server URI, and a byte codec to send and receive
	// messages. Aso set a static keepalive configuration that is common across
	// gRPC language implementations.
	kpCfg := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    5 * time.Minute,
		Timeout: 20 * time.Second,
	})
	cc, err := grpc.NewClient(sc.ServerURI, kpCfg, grpc.WithCredentialsBundle(gtsce.Credentials), grpc.WithDefaultCallOptions(grpc.ForceCodec(&byteCodec{})))
	if err != nil {
		return nil, fmt.Errorf("error creating grpc client for server uri %s, %v", sc.ServerURI, err)
	}

	return &grpcTransport{cc: cc}, nil
}

type grpcTransport struct {
	cc *grpc.ClientConn
}

// NewStream creates a new gRPC stream to the server for the specified method.
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

// Send sends a message to the server.
func (s *stream) Send(msg []byte) error {
	return s.stream.SendMsg(msg)
}

// Recv receives a message from the server.
func (s *stream) Recv() ([]byte, error) {
	var typedRes []byte
	err := s.stream.RecvMsg(&typedRes)
	if err != nil {
		return typedRes, err
	}
	return typedRes, nil
}

// Close closes all the gRPC streams to the server.
func (g *grpcTransport) Close() error {
	return g.cc.Close()
}

type byteCodec struct{}

func (c *byteCodec) Marshal(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return nil, fmt.Errorf("message must be a byte slice")
}

func (c *byteCodec) Unmarshal(data []byte, v any) error {
	if b, ok := v.(*[]byte); ok {
		*b = data
		return nil
	}
	return fmt.Errorf("target must be a pointer to a byte slice")
}

func (c *byteCodec) Name() string {
	return "byteCodec"
}
