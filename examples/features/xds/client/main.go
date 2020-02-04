/*
 *
 * Copyright 2015 gRPC authors.
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

// Package main implements a client for Greeter service.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"

	_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
)

const (
	defaultTarget = "localhost:50051"
	defaultName   = "world"
)

func printHelp() {
	fmt.Printf(`Usage: [name [target]]

  name    The name you wish to be greeted by. Defaults to %s
  target  The server to connect to. Defaults to %s
`, defaultTarget, defaultName)
}

func main() {
	name := defaultName
	if len(os.Args) > 1 {
		if os.Args[1] == "--help" {
			printHelp()
			return
		}
		name = os.Args[1]
	}

	target := defaultTarget
	if len(os.Args) > 2 {
		target = os.Args[2]
	}

	// Set up a connection to the server.
	conn, err := grpc.Dial(target, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.GetMessage())
}
