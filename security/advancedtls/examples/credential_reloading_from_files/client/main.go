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
	"io/ioutil"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider/pemfile"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/security/advancedtls"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

var address = "localhost:50051"

const (
	// Default timeout for normal connections.
	defaultConnTimeout = 10 * time.Second
	// Intervals that set to monitor the credential updates.
	credRefreshingInterval = 500 * time.Millisecond
)

func main() {
	flag.Parse()

	// Create temporary files holding old credential contents.
	certTmpFile, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		log.Fatalf("ioutil.TempFile(%s, %s) failed: %v", os.TempDir(), "pre-", err)
	}
	if err := copyFileContents(testdata.Path("client_cert_1.pem"), certTmpFile.Name()); err != nil {
		log.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("another_client_cert_1.pem"), certTmpFile.Name(), err)
	}
	keyTmpFile, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		log.Fatalf("ioutil.TempFile(%s, %s) failed: %v", os.TempDir(), "pre-", err)
	}
	if err := copyFileContents(testdata.Path("client_key_1.pem"), keyTmpFile.Name()); err != nil {
		log.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("another_client_key_1.pem"), keyTmpFile.Name(), err)
	}

	// Initialize credential struct using reloading API.
	identityOptions := pemfile.Options{
		CertFile:        certTmpFile.Name(),
		KeyFile:         keyTmpFile.Name(),
		RefreshDuration: credRefreshingInterval,
	}
	identityProvider, err := pemfile.NewProvider(identityOptions)
	if err != nil {
		log.Fatalf("pemfile.NewProvider(%v) failed: %v", identityOptions, err)
	}
	rootOptions := pemfile.Options{
		RootFile:        testdata.Path("client_trust_cert_1.pem"),
		RefreshDuration: credRefreshingInterval,
	}
	rootProvider, err := pemfile.NewProvider(rootOptions)
	if err != nil {
		log.Fatalf("pemfile.NewProvider(%v) failed: %v", rootOptions, err)
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

	// Make a connection using the old credentials.
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnTimeout)
	defer cancel()
	oldConn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(clientTLSCreds))
	if err != nil {
		log.Fatalf("grpc.DialContext to %s failed: %v", address, err)
	}
	oldClient := pb.NewGreeterClient(oldConn)
	_, err = oldClient.SayHello(ctx, &pb.HelloRequest{Name: "gRPC"}, grpc.WaitForReady(true))
	if err != nil {
		log.Fatalf("oldClient.SayHello failed: %v", err)
	}
	defer oldConn.Close()

	// Change the contents of the credential files.
	// Cert "another_client_cert_1.pem" is also trusted by "server_cert_1.pem".
	if err := copyFileContents(testdata.Path("another_client_cert_1.pem"), certTmpFile.Name()); err != nil {
		log.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("another_client_cert_1.pem"), certTmpFile.Name(), err)
	}
	if err := copyFileContents(testdata.Path("another_client_key_1.pem"), keyTmpFile.Name()); err != nil {
		log.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("another_client_key_1.pem"), keyTmpFile.Name(), err)
	}

	// Wait for 1 second for the provider to load the new contents.
	time.Sleep(time.Second)

	// Make a connection using the new credential contents, while the credential
	// struct remains the same.
	// The common name of the old and new certificate will be logged on the
	// server side.
	newConn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(clientTLSCreds))
	if err != nil {
		log.Fatalf("grpc.DialContext to %s failed: %v", address, err)
	}
	newClient := pb.NewGreeterClient(newConn)
	_, err = newClient.SayHello(ctx, &pb.HelloRequest{Name: "gRPC"}, grpc.WaitForReady(true))
	if err != nil {
		log.Fatalf("newClient.SayHello failed: %v", err)
	}
}

func copyFileContents(sourceFile, destinationFile string) error {
	input, err := ioutil.ReadFile(sourceFile)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(destinationFile, input, 0644)
	if err != nil {
		return err
	}
	return nil
}
