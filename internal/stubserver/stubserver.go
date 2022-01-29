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

// Package stubserver is a stubbable implementation of
// google.golang.org/grpc/test/grpc_testing for testing purposes.
package stubserver

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"

	testpb "google.golang.org/grpc/test/grpc_testing"
)

// StubServer is a server that is easy to customize within individual test
// cases.
type StubServer struct {
	// Guarantees we satisfy this interface; panics if unimplemented methods are called.
	testpb.TestServiceServer

	// Customizable implementations of server handlers.
	EmptyCallF      func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error)
	UnaryCallF      func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error)
	FullDuplexCallF func(stream testpb.TestService_FullDuplexCallServer) error

	// A client connected to this service the test may use.  Created in Start().
	Client testpb.TestServiceClient
	CC     *grpc.ClientConn
	S      *grpc.Server

	// Parameters for Listen and Dial. Defaults will be used if these are empty
	// before Start.
	Network string
	Address string
	Target  string

	cleanups []func() // Lambdas executed in Stop(); populated by Start().

	// Set automatically if Target == ""
	R *manual.Resolver
}

// EmptyCall is the handler for testpb.EmptyCall
func (ss *StubServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	return ss.EmptyCallF(ctx, in)
}

// UnaryCall is the handler for testpb.UnaryCall
func (ss *StubServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return ss.UnaryCallF(ctx, in)
}

// FullDuplexCall is the handler for testpb.FullDuplexCall
func (ss *StubServer) FullDuplexCall(stream testpb.TestService_FullDuplexCallServer) error {
	return ss.FullDuplexCallF(stream)
}

// Start starts the server and creates a client connected to it.
func (ss *StubServer) Start(sopts []grpc.ServerOption, dopts ...grpc.DialOption) error {
	if err := ss.StartServer(sopts...); err != nil {
		return err
	}
	return ss.StartClient(dopts...)
}

// StartServer only starts the server. It does not create a client to it.
func (ss *StubServer) StartServer(sopts ...grpc.ServerOption) error {
	if ss.Network == "" {
		ss.Network = "tcp"
	}
	if ss.Address == "" {
		ss.Address = "localhost:0"
	}
	if ss.Target == "" {
		ss.R = manual.NewBuilderWithScheme("whatever")
	}

	lis, err := net.Listen(ss.Network, ss.Address)
	if err != nil {
		return fmt.Errorf("net.Listen(%q, %q) = %v", ss.Network, ss.Address, err)
	}
	ss.Address = lis.Addr().String()
	ss.cleanups = append(ss.cleanups, func() { lis.Close() })

	s := grpc.NewServer(sopts...)
	testpb.RegisterTestServiceServer(s, ss)
	go s.Serve(lis)
	ss.cleanups = append(ss.cleanups, s.Stop)
	ss.S = s
	return nil
}

// StartClient creates a client connected to this service that the test may use.
// The newly created client will be available in the Client field of StubServer.
func (ss *StubServer) StartClient(dopts ...grpc.DialOption) error {
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, dopts...)
	if ss.R != nil {
		ss.Target = ss.R.Scheme() + ":///" + ss.Address
		opts = append(opts, grpc.WithResolvers(ss.R))
	}

	cc, err := grpc.Dial(ss.Target, opts...)
	if err != nil {
		return fmt.Errorf("grpc.Dial(%q) = %v", ss.Target, err)
	}
	ss.CC = cc
	if ss.R != nil {
		ss.R.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: ss.Address}}})
	}
	if err := waitForReady(cc); err != nil {
		return err
	}

	ss.cleanups = append(ss.cleanups, func() { cc.Close() })

	ss.Client = testpb.NewTestServiceClient(cc)
	return nil
}

// NewServiceConfig applies sc to ss.Client using the resolver (if present).
func (ss *StubServer) NewServiceConfig(sc string) {
	if ss.R != nil {
		ss.R.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: ss.Address}}, ServiceConfig: parseCfg(ss.R, sc)})
	}
}

func waitForReady(cc *grpc.ClientConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		s := cc.GetState()
		if s == connectivity.Ready {
			return nil
		}
		if !cc.WaitForStateChange(ctx, s) {
			// ctx got timeout or canceled.
			return ctx.Err()
		}
	}
}

// Stop stops ss and cleans up all resources it consumed.
func (ss *StubServer) Stop() {
	for i := len(ss.cleanups) - 1; i >= 0; i-- {
		ss.cleanups[i]()
	}
}

func parseCfg(r *manual.Resolver, s string) *serviceconfig.ParseResult {
	g := r.CC.ParseServiceConfig(s)
	if g.Err != nil {
		panic(fmt.Sprintf("Error parsing config %q: %v", s, g.Err))
	}
	return g
}
