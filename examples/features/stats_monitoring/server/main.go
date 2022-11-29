/*
 *
 * Copyright 2022 gRPC authors.
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

// Binary server is an example server to illustrate the use of the stats handler.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"

	echogrpc "google.golang.org/grpc/examples/features/proto/echo"
	echopb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/examples/features/stats_monitoring/statshandler"
)

var port = flag.Int("port", 50051, "the port to serve on")

type server struct {
	echogrpc.UnimplementedEchoServer
}

func (s *server) UnaryEcho(ctx context.Context, req *echopb.EchoRequest) (*echopb.EchoResponse, error) {
	time.Sleep(2 * time.Second)
	return &echopb.EchoResponse{Message: req.Message}, nil
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen on port %d: %v", *port, err)
	}
	log.Printf("server listening at %v\n", lis.Addr())

	s := grpc.NewServer(grpc.StatsHandler(statshandler.New()))
	echogrpc.RegisterEchoServer(s, &server{})

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
