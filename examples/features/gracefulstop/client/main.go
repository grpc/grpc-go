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
	"fmt"
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

	// Start a client stream and keep calling the `c.UnaryEcho` until receiving
	// an error. Error will indicate that server graceful stop is initiated and
	// it won't accept any new requests.
	stream, err := c.ClientStreamingEcho(ctx)
	if err != nil {
		log.Fatalf("Error starting stream: %v", err)
	}

	// Keep track of successful unary requests which can be compared later to
	// the successful unary requests reported by the server.
	unaryRequests := 0
	for {
		r, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "Hello"})
		if err != nil {
			log.Printf("Error calling `UnaryEcho`. Server graceful stop initiated: %v", err)
			break
		}
		unaryRequests++
		time.Sleep(200 * time.Millisecond)
		log.Print(r.Message)
	}
	log.Printf("Successful unary requests made by client: %d", unaryRequests)

	r, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("Error closing stream: %v", err)
	}
	if fmt.Sprintf("%d", unaryRequests) != r.Message {
		log.Fatalf("Got %s successful unary requests processed from server, want: %d", r.Message, unaryRequests)
	}
	log.Printf("Successful unary requests processed by server and made by client are same.")
}
