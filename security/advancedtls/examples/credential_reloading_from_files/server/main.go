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

// The server demonstrates how to consume and validate OAuth2 tokens provided by
// clients for each RPC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/security/advancedtls"
	"google.golang.org/grpc/security/advancedtls/testdata"

	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

var (
	port = ":50051"
)

const (
	// Intervals that set to monitor the credential updates.
	credRefreshingInterval = 200 * time.Millisecond
)

type greeterServer struct {
	pb.UnimplementedGreeterServer
}

// sayHello is a simple implementation of the pb.GreeterServer SayHello method.
func (greeterServer) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

func main() {
	flag.Parse()
	fmt.Printf("server starting on port %s...\n", port)

	serverIdentityOptions := advancedtls.PEMFileProviderOptions{
		CertFile:         testdata.Path("server_cert_1.pem"),
		KeyFile:          testdata.Path("server_key_1.pem"),
		IdentityInterval: credRefreshingInterval,
	}
	serverIdentityProvider, err := advancedtls.NewPEMFileProvider(serverIdentityOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed, error: %v", serverIdentityOptions, err)
	}
	defer serverIdentityProvider.Close()
	serverRootOptions := advancedtls.PEMFileProviderOptions{
		TrustFile:    testdata.Path("server_trust_cert_1.pem"),
		RootInterval: credRefreshingInterval,
	}
	serverRootProvider, err := advancedtls.NewPEMFileProvider(serverRootOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed, error: %v", serverRootOptions, err)
	}
	defer serverRootProvider.Close()

	// Start a server and create a client using advancedtls API with Provider.
	serverOptions := &advancedtls.ServerOptions{
		IdentityOptions: advancedtls.IdentityCertificateOptions{
			IdentityProvider: serverIdentityProvider,
		},
		RootOptions: advancedtls.RootCertificateOptions{
			RootProvider: serverRootProvider,
		},
		RequireClientCert: true,
		VerifyPeer: func(params *advancedtls.VerificationFuncParams) (*advancedtls.VerificationResults, error) {
			return &advancedtls.VerificationResults{}, nil
		},
		VType: advancedtls.CertVerification,
	}
	serverTLSCreds, err := advancedtls.NewServerCreds(serverOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewServerCreds(%v) failed, error: %v", serverOptions, err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	pb.RegisterGreeterServer(s, greeterServer{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
