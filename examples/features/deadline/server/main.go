/*
 *
 * Copyright 2018 gRPC authors.
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

package main

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var c pb.EchoClient

// server is used to implement EchoServer.
type server struct{}

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	message := req.Message
	if strings.HasPrefix(message, "[propagate me]") {
		time.Sleep(800 * time.Millisecond)
		message = strings.TrimPrefix(message, "[propagate me]")
		return c.UnaryEcho(ctx, &pb.EchoRequest{Message: message})
	}

	if message == "delay" {
		time.Sleep(1500 * time.Millisecond)
	}

	return &pb.EchoResponse{Message: req.Message}, nil
}

func (s *server) ServerStreamingEcho(req *pb.EchoRequest, stream pb.Echo_ServerStreamingEchoServer) error {
	for i := 0; i < 10; i++ {
		if err := stream.Send(&pb.EchoResponse{Message: req.Message}); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) ClientStreamingEcho(stream pb.Echo_ClientStreamingEchoServer) error {
	var lastMessage string
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.EchoResponse{Message: lastMessage})
		}
		if err != nil {
			return err
		}
		lastMessage = req.Message
	}
}

func (s *server) BidirectionalStreamingEcho(stream pb.Echo_BidirectionalStreamingEchoServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		message := req.Message
		if strings.HasPrefix(message, "[propagate me]") {
			time.Sleep(800 * time.Millisecond)
			message = strings.TrimPrefix(message, "[propagate me]")
			res, err := c.UnaryEcho(stream.Context(), &pb.EchoRequest{Message: message})
			if err != nil {
				return err
			}
			stream.Send(res)
		}

		if message == "delay" {
			time.Sleep(1500 * time.Millisecond)
		}
		stream.Send(&pb.EchoResponse{Message: message})
	}
}

func main() {
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &server{})

	conn, err := grpc.Dial("localhost:50052", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c = pb.NewEchoClient(conn)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
