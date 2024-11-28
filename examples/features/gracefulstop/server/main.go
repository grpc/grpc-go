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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	port = flag.Int("port", 50052, "port number")
)

type server struct {
	pb.UnimplementedEchoServer

	unaryRequests atomic.Int32  // to track number of unary RPCs processed
	streamStart   chan struct{} // to signal if server streaming started
}

// ClientStreamingEcho implements the EchoService.ClientStreamingEcho method.
// It signals the server that streaming has started and waits for the stream to
// be done or aborted. If `io.EOF` is received on stream that means client
// has successfully closed the stream using `stream.CloseAndRecv()`, so it
// returns an `EchoResponse` with the total number of unary RPCs processed
// otherwise, it returns the error indicating stream is aborted.
func (s *server) ClientStreamingEcho(stream pb.Echo_ClientStreamingEchoServer) error {
	// Signal streaming start to initiate graceful stop which should wait until
	// server streaming finishes.
	s.streamStart <- struct{}{}

	if err := stream.RecvMsg(&pb.EchoResponse{}); err != nil {
		if errors.Is(err, io.EOF) {
			stream.SendAndClose(&pb.EchoResponse{Message: fmt.Sprintf("%d", s.unaryRequests.Load())})
			return nil
		}
		return err
	}

	return nil
}

// UnaryEcho implements the EchoService.UnaryEcho method. It increments
// `s.unaryRequests` on every call and returns it as part of `EchoResponse`.
func (s *server) UnaryEcho(_ context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	s.unaryRequests.Add(1)
	return &pb.EchoResponse{Message: req.Message}, nil
}

func main() {
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	ss := &server{streamStart: make(chan struct{})}
	pb.RegisterEchoServer(s, ss)

	go func() {
		<-ss.streamStart // wait until server streaming starts
		time.Sleep(1 * time.Second)
		log.Println("Initiating graceful shutdown...")
		timer := time.AfterFunc(10*time.Second, func() {
			log.Println("Server couldn't stop gracefully in time. Doing force stop.")
			s.Stop()
		})
		defer timer.Stop()
		s.GracefulStop() // gracefully stop server after in-flight server streaming rpc finishes
		log.Println("Server stopped gracefully.")
	}()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
