//go:build linux
// +build linux

/*
 *
 * Copyright 2021 gRPC authors.
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

// Binary client is an example client which dials a server on an abstract unix
// socket.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ecpb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	// A dial target of `unix:@abstract-unix-socket` should also work fine for
	// this example because of golang conventions (net.Dial behavior). But we do
	// not recommend this since we explicitly added the `unix-abstract` scheme
	// for cross-language compatibility.
	addr = flag.String("addr", "abstract-unix-socket", "The unix abstract socket address")
)

func callUnaryEcho(c ecpb.EchoClient, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.UnaryEcho(ctx, &ecpb.EchoRequest{Message: message})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	fmt.Println(r.Message)
}

func makeRPCs(cc *grpc.ClientConn, n int) {
	hwc := ecpb.NewEchoClient(cc)
	for i := 0; i < n; i++ {
		callUnaryEcho(hwc, "this is examples/unix_abstract")
	}
}

func main() {
	flag.Parse()
	sockAddr := fmt.Sprintf("unix-abstract:%v", *addr)
	cc, err := grpc.Dial(sockAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("grpc.Dial(%q) failed: %v", sockAddr, err)
	}
	defer cc.Close()

	fmt.Printf("--- calling echo.Echo/UnaryEcho to %s\n", sockAddr)
	makeRPCs(cc, 10)
	fmt.Println()
}
