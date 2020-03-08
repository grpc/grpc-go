// +build go1.11

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
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var help = flag.Bool("help", false, "Print usage information")

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
		return fmt.Sprintf("generated-%03d", rand.Int()%100)
	}
	return hostname
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `
Usage: server [port [hostname]]

  port
        The listen port. Defaults to %d
  hostname
        The name clients will see in greet responses. Defaults to the machine's hostname
`, defaultPort)

		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if *help {
		flag.Usage()
		return
	}
	args := flag.Args()

	if len(args) > 2 {
		flag.Usage()
		return
	}

	port := defaultPort
	if len(args) > 0 {
		var err error
		port, err = strconv.Atoi(args[0])
		if err != nil {
			log.Printf("Invalid port number: %v", err)
			flag.Usage()
			return
		}
	}

	var hostname string
	if len(args) > 1 {
		hostname = args[1]
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

	reflection.Register(s)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, healthServer)

	log.Printf("serving on %s, hostname %s", lis.Addr(), hostname)
	s.Serve(lis)
}
