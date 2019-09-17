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
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var (
	addr        = flag.String("addr", "localhost:50052", "the address to connect to")
	retryPolicy = `{"retryPolicy": {
		  "maxAttempts": 4,
		  "initialBackoff": "0.1s",
		  "maxBackoff": "1s",
		  "backoffMultiplier": 2,
		  "retryableStatusCodes": [
			"Unavailable"
		  ]
		},
		"retryThrottling": {
		  "maxTokens": 10,
		  "tokenRatio": 0.1
		}
	}`
)

func retryDial() (*grpc.ClientConn, error) {
	return grpc.Dial(*addr, grpc.WithInsecure(), grpc.WithDefaultServiceConfig(retryPolicy))
}

// use it for one value return
func newCtx(timeout time.Duration) context.Context {
	ctx, _ := context.WithTimeout(context.TODO(), timeout)
	return ctx
}

func main() {
	flag.Parse()

	// Set up a connection to the server.
	conn, err := retryDial()
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer func() {
		if e := conn.Close(); e != nil {
			log.Printf("failed to close connection: %s", e)
		}
	}()

	c := pb.NewEchoClient(conn)
	reply, err := c.UnaryEcho(newCtx(1*time.Second), &pb.EchoRequest{Message: "Please Success"})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(reply)
}
