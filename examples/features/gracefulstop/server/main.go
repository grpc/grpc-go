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
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	port           = flag.Int("port", 50052, "port number")
	streamMessages int32
	mu             sync.Mutex
)

type server struct {
	pb.UnimplementedEchoServer
}

// ServerStreamingEcho implements the EchoService.ServerStreamingEcho method.
// It receives an EchoRequest and sends back a stream of EchoResponses.
// The stream will contain up to 5 messages, each sent after a 1-second delay.
// If the client cancels the request or if more than 5 messages are sent,
// the stream will be closed with an error.
func (s *server) ServerStreamingEcho(_ *pb.EchoRequest, stream pb.Echo_ServerStreamingEchoServer) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			atomic.AddInt32(&streamMessages, 1)

			mu.Lock()

			if streamMessages > 5 {
				return fmt.Errorf("request failed")
			}

			if err := stream.Send(&pb.EchoResponse{Message: fmt.Sprintf("Messages Sent: %d", streamMessages)}); err != nil {
				return err
			}

			mu.Unlock()

			time.Sleep(1 * time.Second)
		}
	}
}

func main() {
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create a channel to listen for OS signals.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &server{})

	go func() {
		// Wait for an OS signal for graceful shutdown.
		<-stop
		timer := time.AfterFunc(10*time.Second, func() { // forceful stop after 10 seconds
			log.Printf("Graceful shutdown did not complete within 10 seconds. Forcing shutdown...")
			s.Stop()
		})
		defer timer.Stop()
		s.GracefulStop()
	}()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
