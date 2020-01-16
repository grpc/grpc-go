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

package advancedtls

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

// serverImpl is used to implement pb.GreeterServer.
type serverImpl struct{}

// SayHello is a simple implementation of pb.GreeterServer.
func (s *serverImpl) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

// Test Scenarios:
// At initialization(stage = 0), client will be initialized with cert clientPeer1 and clientTrust1, server with serverPeer1 and serverTrust1.
// The mutual authentication works at the beginning, since clientPeer1 is trusted by serverTrust1, and serverPeer1 by clientTrust1.
// At stage 1, client changes clientPeer1 to clientPeer2. Since clientPeer2 is not trusted by serverTrust1, following rpc calls are expected
// to fail, while the previous rpc calls are still good because those are already authenticated.
// At stage 2, the server changes serverTrust1 to serverTrust2, and we should see it again accepts the connection, since clientPeer2 is trusted
// by serverTrust2.
func TestClientPeerCertReloadServerTrustCertReload(t *testing.T) {
	address     := "localhost:50051"
	defaultName := "world"
	port := ":50051"

	// Start a server using ServerOptions in another goroutine.
	s := grpc.NewServer()
	defer s.Stop()
	go func(s *grpc.Server) {
		lis, err := net.Listen("tcp", port)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		pb.RegisterGreeterServer(s, &serverImpl{})
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}(s)

	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	name := defaultName
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.GetMessage())
}
