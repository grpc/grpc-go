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
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/channelz/service"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/internal/grpcrand"
)

var (
	ports = []string{":10001", ":10002", ":10003"}
)

// server is used to implement helloworld.GreeterServer.
type server struct{}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

// slow server is used to simulate a server that has a variable delay in its response.
type slowServer struct{}

// SayHello implements helloworld.GreeterServer
func (s *slowServer) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	// Delay 100ms ~ 200ms before replying
	time.Sleep(time.Duration(100+grpcrand.Intn(100)) * time.Millisecond)
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

func main() {
	/***** Set up the server serving channelz service. *****/
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()
	s := grpc.NewServer()
	service.RegisterChannelzServiceToServer(s)
	go s.Serve(lis)
	defer s.Stop()

	/***** Start three GreeterServers(with one of them to be the slowServer). *****/
	var listeners []net.Listener
	var svrs []*grpc.Server
	for i := 0; i < 3; i++ {
		lis, err := net.Listen("tcp", ports[i])
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		listeners = append(listeners, lis)
		s := grpc.NewServer()
		svrs = append(svrs, s)
		if i == 2 {
			pb.RegisterGreeterServer(s, &slowServer{})
		} else {
			pb.RegisterGreeterServer(s, &server{})
		}
		go s.Serve(lis)
	}

	/***** Wait for CTRL+C to exit *****/
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	// Block until a signal is received.
	<-ch
	for i := 0; i < 3; i++ {
		svrs[i].Stop()
		listeners[i].Close()
	}
}
