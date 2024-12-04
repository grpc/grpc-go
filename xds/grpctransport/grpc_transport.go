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

package grpctransport

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/xds"
)

// ServerConfigExtension is an interface to extend the `xds.ServerConfig` for
// gRPC transport builder. Any implementation needs to implement `ServerConfig`
// method.
type ServerConfigExtension interface {
	ServerConfig() *ServerConfig
}

// ServerConfig contains the configuration for the gRPC transport server.
type ServerConfig struct {
	// Credentials is the credential bundle to be used.
	Credentials credentials.Bundle
}

// ServerConfig returns the ServerConfig itself. This method is designed
// to satisfy interface requirement.
func (s *ServerConfig) ServerConfig() *ServerConfig {
	return s
}

// Builder provides a way to build a gRPC-based transport to an xDS server.
type Builder struct{}

// Build creates a new gRPC-based transport to an xDS server using the provided
// options. This involves creating a grpc.ClientConn to the server identified by
// the server URI in the provided options.
func (b *Builder) Build(opts xds.TransportBuildOptions) (xds.Transport, error) {
	if opts.ServerConfig.ServerURI == "" {
		return nil, fmt.Errorf("ServerConfig Uri field in opts cannot be empty")
	}
	if opts.ServerConfig.Extensions == nil {
		return nil, fmt.Errorf("ServerConfig Extensions field in opts cannot be nil")
	}
	gtsce, ok := opts.ServerConfig.Extensions.(ServerConfigExtension)
	if !ok {
		return nil, fmt.Errorf("ServerConfig field in opts cannot be anything other than GRPCTransportServerConfigExtension")
	}
	gtsc := gtsce.ServerConfig()
	if gtsc.Credentials == nil {
		return nil, fmt.Errorf("ServerConfig Credentials field in opts cannot be nil")
	}

	// Actual implementation of this function will incorporate reference
	// count map for existing transports and deduplicate transports based on
	// server URI and credentials. The deduping logic and reference incr/decr
	// logic is not shown here for brevity.
	//
	// Build() increments the count and returns the existing transport. When
	// Close() is called on the transport, the reference count is decremented.
	// The transport will be removed from map only when the reference count
	// reaches zero.

	cc, err := grpc.NewClient(opts.ServerConfig.ServerURI, grpc.WithCredentialsBundle(gtsc.Credentials))
	if err != nil {
		return nil, fmt.Errorf("error creating grpc client for server uri %s, %v", opts.ServerConfig.ServerURI, err)
	}
	cc.Connect()
	return &grpcTransport{cc: cc}, nil
}

type grpcTransport struct {
	cc *grpc.ClientConn
}

func (g *grpcTransport) NewStream(ctx context.Context, method string) (xds.Stream[any, any], error) {
	return nil, nil
}

func (g *grpcTransport) Close() error {
	return g.cc.Close()
}

type ADSStream[Req any, Res any] struct {
	xds.Stream[any, any]
}

func (a *ADSStream[Req, Res]) Send(m *Req) error {
	return a.Stream.Send(m)
}

func (a *ADSStream[Req, Res]) Recv() (*Res, error) {
	m := new(Res)
	msg, err := a.Stream.Recv()
	if err != nil {
		return nil, err
	}
	*m = msg.(Res)
	return m, nil
}

type LRSStream[Req any, Res any] struct {
	xds.Stream[any, any]
}

func (a *LRSStream[Req, Res]) Send(m *Req) error {
	return a.Stream.Send(m)
}

func (a *LRSStream[Req, Res]) Recv() (*Res, error) {
	m := new(Res)
	msg, err := a.Stream.Recv()
	if err != nil {
		return nil, err
	}
	*m = msg.(Res)
	return m, nil
}
