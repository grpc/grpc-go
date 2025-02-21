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

// Binary server is a server for the dualstack example.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
)

type greeterServer struct {
	hwpb.UnimplementedGreeterServer
	addressType string
	address     string
	port        uint32
}

func (s *greeterServer) SayHello(_ context.Context, req *hwpb.HelloRequest) (*hwpb.HelloReply, error) {
	return &hwpb.HelloReply{
		Message: fmt.Sprintf("Hello %s from server<%d> type: %s)", req.GetName(), s.port, s.addressType),
	}, nil
}

func main() {
	servers := []*greeterServer{
		{
			addressType: "both IPv4 and IPv6",
			address:     "[::]",
			port:        50051,
		},
		{
			addressType: "IPv4 only",
			address:     "127.0.0.1",
			port:        50052,
		},
		{
			addressType: "IPv6 only",
			address:     "[::1]",
			port:        50053,
		},
	}

	var wg sync.WaitGroup
	for _, server := range servers {
		bindAddr := fmt.Sprintf("%s:%d", server.address, server.port)
		lis, err := net.Listen("tcp", bindAddr)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		s := grpc.NewServer()
		hwpb.RegisterGreeterServer(s, server)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Serve(lis); err != nil {
				log.Panicf("failed to serve: %v", err)
			}
		}()
		log.Printf("serving on %s\n", bindAddr)
	}
	wg.Wait()
}
