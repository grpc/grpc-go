/*
 *
 * Copyright 2015 gRPC authors.
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
	"log"
	"net"

	"crypto/tls"
	"crypto/x509"

	"io/ioutil"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/reflection"
)

const (
	port = ":50051"
)

// server is used to implement helloworld.GreeterServer.
type server struct{}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("Greeting: %s", ctx)
        peer, _ := peer.FromContext(ctx)

        tlsInfo := peer.AuthInfo.(credentials.TLSInfo);
	log.Printf("\nGreeting: %s", tlsInfo.State.VerifiedChains[0][0].Subject.CommonName)
	return &pb.HelloReply{Message: "Heiillo " + in.Name}, nil
}

func main() {
	certificate, err := tls.LoadX509KeyPair(
                "server.cert.pem",
	        "server.key.pem",
	)
	certPool := x509.NewCertPool()
	bs, err := ioutil.ReadFile("ca.cert.pem")
	if err != nil {
		  log.Fatalf("failed to read client ca cert: %s", err)
	}

	ok := certPool.AppendCertsFromPEM(bs)
	if !ok {
		  log.Fatal("failed to append client certs")
	  }
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	tlsConfig := &tls.Config{
                ClientAuth:   tls.RequireAndVerifyClientCert,
	        Certificates: []tls.Certificate{certificate},
	        ClientCAs:    certPool,
        }
	// Create the TLS credentials
	serverOption := grpc.Creds(credentials.NewTLS(tlsConfig))
	s := grpc.NewServer(serverOption)
	// creds, err := credentials.NewServerTLSFromFile("server.crt", "server.key")
	// if err != nil {
	//        log.Fatalf("could not load TLS keys: %s", err)
	// }

	// s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterGreeterServer(s, &server{})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
