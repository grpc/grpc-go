/*
 *
 * Copyright 2024 gRPC authors.
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
 */

// Binary client demonstrates sending multiple requests to server and observe
// graceful stop.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50052", "the address to connect to")

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create new client: %v", err)
	}
	defer conn.Close()
	c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Start a server stream and receive 5 messages before closing the stream.
	// This will initiate the graceful stop on the server.
	stream, err := c.ServerStreamingEcho(ctx, &pb.EchoRequest{})
	if err != nil {
		log.Fatalf("Error starting stream: %v", err)
	}
	// Server must complete the in-flight streaming RPC so client should
	// receive 5 messages before stopping.
	for i := 0; i < 5; i++ {
		r, err := stream.Recv()
		if err != nil {
			log.Fatalf("Error receiving message: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
		log.Printf(r.Message)
	}
	stream.CloseSend()
	log.Printf("Client finished streaming.")
}
