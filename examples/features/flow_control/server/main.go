/*
 *
 * Copyright 2023 gRPC authors.
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

// Binary server is an example server.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"

	pb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/internal/grpcsync"
)

var port = flag.Int("port", 50052, "port number")

var payload string = string(make([]byte, 8*1024)) // 8KB

// server is used to implement EchoServer.
type server struct {
	pb.UnimplementedEchoServer
}

func (s *server) BidirectionalStreamingEcho(stream pb.Echo_BidirectionalStreamingEchoServer) error {
	log.Printf("New stream began.")
	// First, we wait 2 seconds before reading from the stream, to give the
	// client an opportunity to block while sending its requests.
	time.Sleep(2 * time.Second)

	// Next, read all the data sent by the client to allow it to unblock.
	for i := 0; true; i++ {
		if _, err := stream.Recv(); err != nil {
			log.Printf("Read %v messages.", i)
			if err == io.EOF {
				break
			}
			log.Printf("Error receiving data: %v", err)
			return err
		}
	}

	// Finally, send data until we block, then end the stream after we unblock.
	stopSending := grpcsync.NewEvent()
	sentOne := make(chan struct{})
	go func() {
		for !stopSending.HasFired() {
			after := time.NewTimer(time.Second)
			select {
			case <-sentOne:
				after.Stop()
			case <-after.C:
				log.Printf("Sending is blocked.")
				stopSending.Fire()
				<-sentOne
			}
		}
	}()

	i := 0
	for !stopSending.HasFired() {
		i++
		if err := stream.Send(&pb.EchoResponse{Message: payload}); err != nil {
			log.Printf("Error sending data: %v", err)
			return err
		}
		sentOne <- struct{}{}
	}
	log.Printf("Sent %v messages.", i)

	log.Printf("Stream ended successfully.")
	return nil
}

func main() {
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterEchoServer(grpcServer, &server{})

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
