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
 */

// Binary server demonstrates how to gracefully stop a gRPC server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	port           = flag.Int("port", 50052, "port number")
	streamMessages int32
	mu             sync.Mutex
	streamCh       chan struct{} // to signal if server streaming started
)

type server struct {
	pb.UnimplementedEchoServer
}

// ServerStreamingEcho implements the EchoService.ServerStreamingEcho method.
// It receives an EchoRequest and sends back a stream of EchoResponses until an
// error occurs or the stream is closed.
func (s *server) ServerStreamingEcho(_ *pb.EchoRequest, stream pb.Echo_ServerStreamingEchoServer) error {
	// Signal streaming start to initiate graceful stop which should wait until
	// server streaming finishes.
	streamCh <- struct{}{}

	for {
		atomic.AddInt32(&streamMessages, 1)

		mu.Lock()
		if err := stream.Send(&pb.EchoResponse{Message: fmt.Sprintf("Messages Sent: %d", streamMessages)}); err != nil {
			log.Printf("Stream is sending data: %v. Stop Streaming", err)
			return err
		}
		mu.Unlock()
	}
}

func main() {
	streamCh = make(chan struct{})
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &server{})

	go func() {
		<-streamCh // wait until server streaming starts
		log.Println("Initiating graceful shutdown...")
		s.GracefulStop() // gracefully stop server after in-flight server streaming rpc finishes
		log.Println("Server stopped gracefully.")
	}()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
