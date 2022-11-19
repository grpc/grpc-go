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

// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")

func callUnaryEcho(client pb.EchoClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.UnaryEcho(ctx, &pb.EchoRequest{Message: "hello world"})
	if err != nil {
		log.Fatalf("UnaryEcho %v", err)
	}
	fmt.Println("UnaryEcho: ", resp.Message)
}

func callBidiStreamingEcho(client pb.EchoClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.BidirectionalStreamingEcho(ctx)
	if err != nil {
		log.Fatalf("BidiStreamingEcho %v", err)
	}

	if err := c.Send(&pb.EchoRequest{Message: "hello world"}); err != nil {
		log.Fatalf("Sending echo request: %v", err)
	}
	c.CloseSend()

	for {
		resp, err := c.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Receiving echo response: %v", err)
		}
		fmt.Println("BidiStreaming Echo: ", resp.Message)
	}
}

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("grpc.Dial(%q): %v", *addr, err)
	}
	defer conn.Close()

	ec := pb.NewEchoClient(conn)

	callUnaryEcho(ec)

	callBidiStreamingEcho(ec)
}
