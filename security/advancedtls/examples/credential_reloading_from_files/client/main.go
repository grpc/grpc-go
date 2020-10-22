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

// The client demonstrates how to supply an OAuth2 token for every RPC.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/security/advancedtls"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

var (
	address = "localhost:50051"
)

const (
	// Default timeout for normal connections.
	defaultTestTimeout = 5 * time.Second
	// Intervals that set to monitor the credential updates.
	credRefreshingInterval = 200 * time.Millisecond
)

func main() {
	flag.Parse()

	clientIdentityOptions := advancedtls.PEMFileProviderOptions{
		CertFile:         testdata.Path("client_cert_1.pem"),
		KeyFile:          testdata.Path("client_key_1.pem"),
		IdentityInterval: credRefreshingInterval,
	}
	clientIdentityProvider, err := advancedtls.NewPEMFileProvider(clientIdentityOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed, error: %v", clientIdentityOptions, err)
	}
	clientRootOptions := advancedtls.PEMFileProviderOptions{
		TrustFile:    testdata.Path("client_trust_cert_1.pem"),
		RootInterval: credRefreshingInterval,
	}
	clientRootProvider, err := advancedtls.NewPEMFileProvider(clientRootOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed, error: %v", clientRootOptions, err)
	}

	clientOptions := &advancedtls.ClientOptions{
		IdentityOptions: advancedtls.IdentityCertificateOptions{
			IdentityProvider: clientIdentityProvider,
		},
		VerifyPeer: func(params *advancedtls.VerificationFuncParams) (*advancedtls.VerificationResults, error) {
			return &advancedtls.VerificationResults{}, nil
		},
		RootOptions: advancedtls.RootCertificateOptions{
			RootProvider: clientRootProvider,
		},
		VType: advancedtls.CertVerification,
	}
	clientTLSCreds, err := advancedtls.NewClientCreds(clientOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewClientCreds(%v) failed, error: %v", clientOptions, err)
	}

	// At initialization, the connection should be good.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(clientTLSCreds))
	if err != nil {
		log.Fatalf("grpc.DialContext to %s failed, error: %v", address, err)
	}
	greetClient := pb.NewGreeterClient(conn)
	reply, err := greetClient.SayHello(ctx, &pb.HelloRequest{Name: "gRPC"})
	if err != nil {
		log.Fatalf("greetClient.SayHello failed, error: %v", err)
	}
	defer conn.Close()
	fmt.Printf("Getting message from server: %s...\n", reply.Message)
}
