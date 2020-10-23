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

// The client demonstrates how to use the credential reloading feature in
// advancedtls to make a mTLS connection to the server.
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

var address = "localhost:50051"

const (
	// Default timeout for normal connections.
	defaultConnTimeout = 10 * time.Second
	// Intervals that set to monitor the credential updates.
	credRefreshingInterval = 1 * time.Minute
)

func main() {
	flag.Parse()

	// TODO(ZhenLian): change function signatures to reflect the changes in
	// https://github.com/grpc/grpc-go/pull/3981.
	identityOptions := advancedtls.PEMFileProviderOptions{
		CertFile:         testdata.Path("client_cert_1.pem"),
		KeyFile:          testdata.Path("client_key_1.pem"),
		IdentityInterval: credRefreshingInterval,
	}
	identityProvider, err := advancedtls.NewPEMFileProvider(identityOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed: %v", identityOptions, err)
	}
	rootOptions := advancedtls.PEMFileProviderOptions{
		TrustFile:    testdata.Path("client_trust_cert_1.pem"),
		RootInterval: credRefreshingInterval,
	}
	rootProvider, err := advancedtls.NewPEMFileProvider(rootOptions)
	if err != nil {
		log.Fatalf("advancedtls.NewPEMFileProvider(%v) failed: %v", rootOptions, err)
	}

	options := &advancedtls.ClientOptions{
		IdentityOptions: advancedtls.IdentityCertificateOptions{
			IdentityProvider: identityProvider,
		},
		VerifyPeer: func(params *advancedtls.VerificationFuncParams) (*advancedtls.VerificationResults, error) {
			return &advancedtls.VerificationResults{}, nil
		},
		RootOptions: advancedtls.RootCertificateOptions{
			RootProvider: rootProvider,
		},
		VType: advancedtls.CertVerification,
	}
	clientTLSCreds, err := advancedtls.NewClientCreds(options)
	if err != nil {
		log.Fatalf("advancedtls.NewClientCreds(%v) failed: %v", options, err)
	}

	// At initialization, the connection should be good.
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(clientTLSCreds))
	if err != nil {
		log.Fatalf("grpc.DialContext to %s failed: %v", address, err)
	}
	greetClient := pb.NewGreeterClient(conn)
	reply, err := greetClient.SayHello(ctx, &pb.HelloRequest{Name: "gRPC"}, grpc.WaitForReady(true))
	if err != nil {
		log.Fatalf("greetClient.SayHello failed: %v", err)
	}
	defer conn.Close()
	fmt.Printf("Getting message from server: %s...\n", reply.Message)
}
