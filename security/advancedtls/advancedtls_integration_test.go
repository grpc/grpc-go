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
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	ecpb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

// serverImpl is used to implement pb.GreeterServer.
type serverImpl struct{}

// SayHello is a simple implementation of pb.GreeterServer.
func (s *serverImpl) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

type ecServerImpl struct {
	ecpb.UnimplementedEchoServer
}

func (s *ecServerImpl) UnaryEcho(ctx context.Context, req *ecpb.EchoRequest) (*ecpb.EchoResponse, error) {
	return &ecpb.EchoResponse{Message: req.Message}, nil
}

func TestHelloWorld(t *testing.T) {
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

func TestTls(t *testing.T) {
	address     := "localhost:50051"
	port := ":50051"
	// Create tls based credential.
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("server_cert_1.pem"), testdata.Path("server_key_1.pem"))
	if err != nil {
		log.Fatalf("failed to create credentials: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(creds))
	defer s.Stop()
	go func(s *grpc.Server) {
		lis, err := net.Listen("tcp", port)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		// Register EchoServer on the server.
		ecpb.RegisterEchoServer(s, &ecServerImpl{})
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}(s)
	log.Println("zhen: server created")
	clientcreds, err := credentials.NewClientTLSFromFile(testdata.Path("client_trust_cert_1.pem"), "foo.bar.com")
	if err != nil {
		log.Fatalf("failed to load credentials: %v", err)
	}
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(clientcreds))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	log.Println("zhen: connection established")
	// Make a echo client and send an RPC.
	rgc := ecpb.NewEchoClient(conn)
	log.Println("zhen: client created")
	callUnaryEcho(rgc, "hello world")
	log.Println("zhen: call finished")
}

func callUnaryEcho(client ecpb.EchoClient, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.UnaryEcho(ctx, &ecpb.EchoRequest{Message: message})
	if err != nil {
		log.Fatalf("client.UnaryEcho(_) = _, %v: ", err)
	}
	fmt.Println("UnaryEcho: ", resp.Message)
}
