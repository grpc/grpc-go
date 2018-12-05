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
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/status"
)

// server is used to implement helloworld.GreeterServer.
type server struct{}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

// serve starts listening with a 2 seconds delay.
func serve() {
	time.Sleep(2 * time.Second)
	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// connectAndRequest creates a connection, makes a request and compares error code with the expected code.
func connectAndRequest(requestID int, waitForReady bool, timeout time.Duration, want codes.Code) {
	conn, err := grpc.Dial("localhost:50053", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("[%v] did not connect: %v", requestID, err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err = c.SayHello(ctx, &pb.HelloRequest{}, grpc.FailFast(!waitForReady))

	got := status.Code(err)
	if got != want {
		log.Fatalf("[%v] wanted = %v, got = %v (error code)", requestID, want, got)
	}
}

func main() {
	go serve()

	var wg sync.WaitGroup
	wg.Add(3)
	// "Wait for ready" is not enabled, returns error with code "Unavailable".
	go func() {
		defer wg.Done()
		connectAndRequest(1, false, 10*time.Second, codes.Unavailable)
	}()
	// "Wait for ready" is enabled, returns nil error.
	go func() {
		defer wg.Done()
		connectAndRequest(2, true, 10*time.Second, codes.OK)
	}()
	// "Wait for ready" is enabled but exceeds the deadline before server starts listening,
	// returns error with code "DeadlineExceeded".
	go func() {
		defer wg.Done()
		connectAndRequest(3, true, 1*time.Second, codes.DeadlineExceeded)
	}()
	wg.Wait()
}
