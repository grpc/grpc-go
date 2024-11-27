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
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/xds"
)

type GRPCTransportServerConfigExtension interface {
	GRPCTransportServerConfig() *GRPCTransportServerConfig
}

type GRPCTransportServerConfig struct {
	ServerCredentials map[string]credentials.Bundle
}

func (s *GRPCTransportServerConfig) GRPCTransportServerConfig() *GRPCTransportServerConfig {
	return s
}

// dialer captures the Dialer method specified via the credentials bundle.
type dialer interface {
	// Dialer specifies how to dial the xDS server.
	Dialer(context.Context, string) (net.Conn, error)
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
	gtsce, ok := opts.ServerConfig.Extensions.(GRPCTransportServerConfigExtension)
	if !ok {
		return nil, fmt.Errorf("ServerConfig field in opts cannot be anything other than GRPCTransportServerConfigExtension")
	}

	gtsc := gtsce.GRPCTransportServerConfig()
	bundle, ok := gtsc.ServerCredentials[opts.ServerConfig.ServerURI]
	if !ok {
		return nil, fmt.Errorf("server credentials were not found for server uri: %s", opts.ServerConfig.ServerURI)
	}

	credsDialOption := grpc.WithCredentialsBundle(bundle)
	d, _ := bundle.(dialer)
	dialerOption := grpc.WithContextDialer(d.Dialer)

	grpcClient, _ := grpc.NewClient(opts.ServerConfig.ServerURI, dialerOption, credsDialOption)

	return &grpcTransport{cc: grpcClient}, nil
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
