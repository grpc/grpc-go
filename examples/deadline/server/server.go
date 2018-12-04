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
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/deadline/deadline"
)

const (
	address = "localhost:50052"
)
const (
	port = ":50052"
)

type server struct{}

// SayHello implements helloworld.GreeterServer
func (s *server) MakeRequest(ctx context.Context, in *pb.DeadlinerRequest) (*pb.DeadlinerReply, error) {
	if in.Hops > 0 {
		conn, err := grpc.Dial(address, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer conn.Close()
		c := pb.NewDeadlinerClient(conn)

		in.Hops--
		time.Sleep(300 * time.Millisecond)
		r, err := c.MakeRequest(ctx, in)
		if err != nil {
			log.Printf("could not greet: %v", err)
			return nil, err
		}
		return r, nil
	}

	if in.Message == "delay" {
		time.Sleep(2 * time.Second)
	}

	return &pb.DeadlinerReply{Message: "pong "}, nil
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterDeadlinerServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
