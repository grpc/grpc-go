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

// Package fakeserver provides a fake implementation of the RouteLookupService,
// to be used in unit tests.
package fakeserver

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	rlsgrpc "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

const defaultDialTimeout = 5 * time.Second

// Response wraps the response protobuf (xds/LRS) and error that the Server
// should send out to the client through a call to stream.Send()
type Response struct {
	Resp *rlspb.RouteLookupResponse
	Err  error
}

// Server is a fake implementation of RLS. It exposes channels to send/receive
// RLS requests and responses.
type Server struct {
	RequestChan  chan *rlspb.RouteLookupRequest
	ResponseChan chan Response
	Address      string
}

// Start makes a new Server and gets it to start listening on a local port for
// gRPC requests. The returned cancel function should be invoked by the caller
// upon completion of the test.
func Start() (*Server, func(), error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, func() {}, fmt.Errorf("net.Listen() failed: %v", err)
	}

	s := &Server{
		// Give the channels a buffer size of 1 so that we can setup
		// expectations for one lookup call, without blocking.
		RequestChan:  make(chan *rlspb.RouteLookupRequest, 1),
		ResponseChan: make(chan Response, 1),
		Address:      lis.Addr().String(),
	}

	server := grpc.NewServer()
	rlsgrpc.RegisterRouteLookupServiceServer(server, s)
	go server.Serve(lis)

	return s, func() { server.Stop() }, nil
}

// RouteLookup implements the RouteLookupService.
func (s *Server) RouteLookup(ctx context.Context, req *rlspb.RouteLookupRequest) (*rlspb.RouteLookupResponse, error) {
	s.RequestChan <- req
	resp := <-s.ResponseChan
	return resp.Resp, resp.Err
}

// ClientConn returns a grpc.ClientConn connected to the fakeServer.
func (s *Server) ClientConn() (*grpc.ClientConn, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()

	cc, err := grpc.DialContext(ctx, s.Address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, nil, fmt.Errorf("grpc.DialContext(%s) failed: %v", s.Address, err)
	}
	return cc, func() { cc.Close() }, nil
}
