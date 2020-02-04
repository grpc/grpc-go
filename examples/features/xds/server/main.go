/*
 *
 * Copyright 2020 gRPC authors.
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

// Package main starts Greeter service that will response with the hostname.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

const (
	defaultPort = 50051
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	pb.UnimplementedGreeterServer

	serverName string
}

func newServer(serverName string) *server {
	return &server{
		serverName: serverName,
	}
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Received: %v", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName() + ", from " + s.serverName}, nil
}

func determineHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("Failed to get hostname: %v, will generate one", err)
		rand.Seed(time.Now().UnixNano())
		return fmt.Sprintf("generated-%d", rand.Int())
	}
	return hostname
}

func printHelp() {
	fmt.Printf(`Usage: [port [hostname]]

  port      The listen port. Defaults to %d
  hostname  The name clients will see in greet responses. Defaults to the machine's hostname
`, defaultPort)
}

func main() {
	port := defaultPort
	if len(os.Args) > 1 {
		if os.Args[1] == "--help" {
			printHelp()
			return
		}

		var err error
		port, err = strconv.Atoi(os.Args[1])
		if err != nil {
			log.Printf("Invalid port number: %v", err)
			printHelp()
			return
		}
	}

	var hostname string
	if len(os.Args) > 2 {
		hostname = os.Args[2]
	}
	if hostname == "" {
		hostname = determineHostname()
	}

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, newServer(hostname))
	log.Printf("serving on %s, hostname %s", lis.Addr(), hostname)
	s.Serve(lis)
}
