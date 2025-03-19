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
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/clients"
)

// ServerIdentifierExtension holds settings for connecting to a gRPC server,
// such as an xDS management or an LRS server.
type ServerIdentifierExtension struct {
	// Credentials will be used for all gRPC transports. If it is unset,
	// transport creation will fail.
	Credentials credentials.Bundle
}

// String returns a string representation of the ServerIdentifierExtension.
func (sie *ServerIdentifierExtension) String() string {
	if sie.Credentials == nil {
		return ""
	}
	var tcParts []string
	if sie.Credentials.TransportCredentials() != nil {
		tcInfo := sie.Credentials.TransportCredentials().Info()
		for _, v := range []string{tcInfo.ProtocolVersion, tcInfo.SecurityProtocol, tcInfo.ServerName} {
			if v != "" {
				tcParts = append(tcParts, v)
			}
		}
	}
	if sie.Credentials.PerRPCCredentials() != nil {
		tcParts = append(tcParts, fmt.Sprintf("%v", sie.Credentials.PerRPCCredentials().RequireTransportSecurity()))
	}
	return strings.Join(tcParts, "-")
}

// Equal returns true if sie and other are considered equal.
func (sie *ServerIdentifierExtension) Equal(other any) bool {
	sie2, ok := other.(*ServerIdentifierExtension)
	if !ok {
		return false
	}

	switch {
	case sie.Credentials == nil && sie2.Credentials == nil:
		return true
	case (sie.Credentials != nil) != (sie2.Credentials != nil):
		return false
	case (sie.Credentials.TransportCredentials() != nil) != (sie2.Credentials.TransportCredentials() != nil):
		return false
	case (sie.Credentials.PerRPCCredentials() != nil) != (sie2.Credentials.PerRPCCredentials() != nil):
		return false
	}

	if sie.Credentials.TransportCredentials() != nil {
		switch {
		case sie.Credentials.TransportCredentials().Info().ProtocolVersion != sie2.Credentials.TransportCredentials().Info().ProtocolVersion:
			return false
		case sie.Credentials.TransportCredentials().Info().SecurityProtocol != sie2.Credentials.TransportCredentials().Info().SecurityProtocol:
			return false
		case sie.Credentials.TransportCredentials().Info().ServerName != sie2.Credentials.TransportCredentials().Info().ServerName:
			return false
		}
	}

	if sie.Credentials.PerRPCCredentials() != nil {
		if sie.Credentials.PerRPCCredentials().RequireTransportSecurity() != sie2.Credentials.PerRPCCredentials().RequireTransportSecurity() {
			return false
		}
	}

	return true
}

// Builder creates gRPC-based Transports. It must be paired with ServerIdentifiers
// that contain an Extension field of type ServerIdentifierExtension.
type Builder struct{}

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
	sce, ok := si.Extensions.(*ServerIdentifierExtension)
	if !ok {
		return nil, fmt.Errorf("grpctransport: Extensions field is %T, but must be %T in ServerIdentifier", si.Extensions, ServerIdentifierExtension{})
	}
	if sce.Credentials == nil {
		return nil, fmt.Errorf("grptransport: Credentials field is not set in ServerIdentifierExtension")
	}

	// TODO: Incorporate reference count map for existing transports and
	// deduplicate transports based on the provided ServerIdentifier so that
	// transport channel to same server can be shared between xDS and LRS
	// client.

	// Create a new gRPC client/channel for the server with the provided
	// credentials, server URI, and a byte codec to send and receive messages.
	// Also set a static keepalive configuration that is common across gRPC
	// language implementations.
	kpCfg := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    5 * time.Minute,
		Timeout: 20 * time.Second,
	})
	cc, err := grpc.NewClient(si.ServerURI, kpCfg, grpc.WithCredentialsBundle(sce.Credentials), grpc.WithDefaultCallOptions(grpc.ForceCodec(&byteCodec{})))
	if err != nil {
		return nil, fmt.Errorf("grpctransport: failed to create transport to server %q: %v", si.ServerURI, err)
	}

	return &grpcTransport{cc: cc}, nil
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
