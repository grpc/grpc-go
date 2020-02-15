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

// Package main implements a client for Greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"

	_ "google.golang.org/grpc/xds/experimental" // To install the xds resolvers and balancers.
)

const (
	defaultTarget = "localhost:50051"
	defaultName   = "world"
)

var help = flag.Bool("help", false, "Print usage information")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `
Usage: client [name [target]]

  name
        The name you wish to be greeted by. Defaults to %q
  target
        The URI of the server, e.g. "xds-experimental:///helloworld-service". Defaults to %q
`, defaultName, defaultTarget)

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

	name := defaultName
	if len(args) > 0 {
		name = args[0]
	}

	target := defaultTarget
	if len(args) > 1 {
		target = args[1]
	}

	// Set up a connection to the server.
	conn, err := grpc.Dial(target, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.GetMessage())
}
