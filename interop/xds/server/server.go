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

// Binary server for xDS interop tests.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	port     = flag.Int("port", 8080, "The server port")
	serverID = flag.String("server_id", "go_server", "Server ID included in response")
	hostname = getHostname()

	logger = grpclog.Component("interop")
)

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("failed to get hostname: %v", err)
	}
	return hostname
}

type server struct {
	testgrpc.UnimplementedTestServiceServer
}

func (s *server) EmptyCall(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
	grpc.SetHeader(ctx, metadata.Pairs("hostname", hostname))
	return &testpb.Empty{}, nil
}

func (s *server) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	grpc.SetHeader(ctx, metadata.Pairs("hostname", hostname))
	return &testpb.SimpleResponse{ServerId: *serverID, Hostname: hostname}, nil
}

func main() {
	flag.Parse()
	p := strconv.Itoa(*port)
	lis, err := net.Listen("tcp", ":"+p)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(s, &server{})
	s.Serve(lis)
}
