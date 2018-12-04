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
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/deadline/deadline"
)

const (
	address = "localhost:50052"
)

func makeRequest(c pb.DeadlinerClient, request *pb.DeadlinerRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.MakeRequest(ctx, request)
	if err != nil {
		log.Printf("could not greet: %v", err)
		return
	}
	log.Printf("Reply: %s", r.Message)
}

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewDeadlinerClient(conn)

	makeRequest(c, &pb.DeadlinerRequest{})
	makeRequest(c, &pb.DeadlinerRequest{Message: "delay"})
	makeRequest(c, &pb.DeadlinerRequest{Hops: 3})
	makeRequest(c, &pb.DeadlinerRequest{Hops: 4})
}
