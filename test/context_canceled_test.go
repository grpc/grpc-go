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

// Binary wait_for_ready is an example for "wait for ready".
package test

import (
	"context"
	"log"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// server is used to implement EchoServer.
type server struct {
	wait chan struct{}
}

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: req.Message}, nil
}

func (s *server) ServerStreamingEcho(req *pb.EchoRequest, stream pb.Echo_ServerStreamingEchoServer) error {
	return status.Error(codes.Unimplemented, "RPC unimplemented")
}

func (s *server) ClientStreamingEcho(stream pb.Echo_ClientStreamingEchoServer) error {
	return status.Error(codes.Unimplemented, "RPC unimplemented")
}

func (s *server) BidirectionalStreamingEcho(stream pb.Echo_BidirectionalStreamingEchoServer) error {
	stream.SetTrailer(metadata.New(map[string]string{"a": "b"}))
	close(s.wait)
	return status.Error(codes.PermissionDenied, "perm denied")
}

//TestRace tests.
func TestContextCanceled(t *testing.T) {
	lis, err := net.Listen("tcp", ":50054")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	server := &server{make(chan struct{})}
	pb.RegisterEchoServer(s, server)
	go func() {
		err := s.Serve(lis)
		if err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	defer s.Stop()

	conn, err := grpc.Dial("localhost:50054", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel
	str, err := c.BidirectionalStreamingEcho(ctx)
	<-server.wait
	time.Sleep(time.Millisecond)
	cancel()
	_, err = str.Recv()
	if err == nil {
		t.Fatalf("non-nil error expected from Recv()")
	}
	trl := str.Trailer()
	if status.Code(err) == codes.PermissionDenied && trl["a"] == nil {
		t.Fatalf("<<a>> not in trailer")
	} else if status.Code(err) == codes.Canceled && trl["a"] != nil {
		t.Fatalf("<<a>> in trailer")
	}
}
