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
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/status"
)

// server is used to implement EchoServer.
type server struct{}

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
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
	for {
		var messages []string
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.EchoResponse{Message: strings.Join(messages, ";")})
		}
		if err != nil {
			return err
		}
		messages = append(messages, req.Message)
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
		stream.Send(&pb.EchoResponse{Message: req.Message})
	}
}

// serve starts listening with a 2 seconds delay.
func serve() {
	time.Sleep(2 * time.Second)
	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &server{})

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// unaryCall makes a unary request and compares error code with the expected code.
func unaryCall(c pb.EchoClient, requestID int, waitForReady bool, timeout time.Duration, want codes.Code) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "Hi!"}, grpc.FailFast(!waitForReady))

	got := status.Code(err)
	if got != want {
		log.Fatalf("[%v] wanted = %v, got = %v (error code)", requestID, want, got)
	}
}

func main() {
	conn, err := grpc.Dial("localhost:50053", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewEchoClient(conn)

	go serve()

	var wg sync.WaitGroup
	wg.Add(3)
	// "Wait for ready" is not enabled, returns error with code "Unavailable".
	go func() {
		defer wg.Done()
		unaryCall(c, 1, false, 10*time.Second, codes.Unavailable)
	}()
	// "Wait for ready" is enabled, returns nil error.
	go func() {
		defer wg.Done()
		unaryCall(c, 2, true, 10*time.Second, codes.OK)
	}()
	// "Wait for ready" is enabled but exceeds the deadline before server starts listening,
	// returns error with code "DeadlineExceeded".
	go func() {
		defer wg.Done()
		unaryCall(c, 3, true, 1*time.Second, codes.DeadlineExceeded)
	}()

	wg.Wait()
}
