/*
 *
 * Copyright 2024 gRPC authors.
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
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	pb "google.golang.org/grpc/examples/features/proto/echo"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/tls/certprovider/pemfile"
	"google.golang.org/grpc/security/advancedtls"
)

type server struct {
	pb.UnimplementedEchoServer
	name string
}

const credRefreshInterval = 1 * time.Minute
const goodServerWithCrlPort int = 50051
const revokedServerWithCrlPort int = 50052
const insecurePort int = 50053

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: req.Message}, nil
}

func tlsServers(credentialsDirectory string) {
	go func() {
		createAndRunTLSServer(credentialsDirectory, false, goodServerWithCrlPort)
	}()
	go func() {
		createAndRunTLSServer(credentialsDirectory, true, revokedServerWithCrlPort)
	}()
}

func insecureServer() {
	go func() {
		createAndRunInsecureServer(insecurePort)
	}()
}

func createAndRunInsecureServer(port int) {
	creds := insecure.NewCredentials()
	s := grpc.NewServer(grpc.Creds(creds))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
	}
	pb.RegisterEchoServer(s, &server{name: "Insecure Server"})
	if err := s.Serve(lis); err != nil {
		fmt.Printf("Failed to serve: %v\n", err)
		os.Exit(1)
	}
}

func createAndRunTLSServer(credsDirectory string, useRevokedCert bool, port int) {
	identityProvider := makeIdentityProvider(useRevokedCert, credsDirectory)
	defer identityProvider.Close()

	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()

	crlProvider := makeCrlProvider(filepath.Join(credsDirectory, "crl"))
	defer crlProvider.Close()

	options := &advancedtls.Options{
		IdentityOptions: advancedtls.IdentityCertificateOptions{
			IdentityProvider: identityProvider,
		},
		RootOptions: advancedtls.RootCertificateOptions{
			RootProvider: rootProvider,
		},
		RequireClientCert: true,
		VerificationType:  advancedtls.CertVerification,
	}

	options.RevocationOptions = &advancedtls.RevocationOptions{
		CRLProvider: crlProvider,
	}

	serverTLSCreds, err := advancedtls.NewServerCreds(options)
	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}

	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
	}
	name := "Good TLS Server"
	if useRevokedCert {
		name = "Revoked TLS Server"
	}
	pb.RegisterEchoServer(s, &server{name: name})
	if err := s.Serve(lis); err != nil {
		fmt.Printf("Failed to serve: %v\n", err)
		os.Exit(1)
	}

}

func makeRootProvider(credsDirectory string) certprovider.Provider {
	rootOptions := pemfile.Options{
		RootFile:        filepath.Join(credsDirectory, "/ca_cert.pem"),
		RefreshDuration: credRefreshInterval,
	}

	rootProvider, err := pemfile.NewProvider(rootOptions)
	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}
	return rootProvider
}

func makeIdentityProvider(useRevokedCert bool, credsDirectory string) certprovider.Provider {
	certFilePath := ""
	if useRevokedCert {
		certFilePath = filepath.Join(credsDirectory, "server_cert_revoked.pem")
	} else {
		certFilePath = filepath.Join(credsDirectory, "server_cert.pem")
	}
	identityOptions := pemfile.Options{
		CertFile:        certFilePath,
		KeyFile:         filepath.Join(credsDirectory, "server_key.pem"),
		RefreshDuration: credRefreshInterval,
	}
	identityProvider, err := pemfile.NewProvider(identityOptions)
	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}
	return identityProvider
}

func makeCrlProvider(crlDirectory string) *advancedtls.FileWatcherCRLProvider {
	options := advancedtls.FileWatcherOptions{
		CRLDirectory: crlDirectory,
	}
	provider, err := advancedtls.NewFileWatcherCRLProvider(options)
	if err != nil {
		fmt.Printf("Error making CRL Provider: %v\nExiting...", err)
		os.Exit(1)
	}
	return provider
}

func main() {
	credentialsDirectory := flag.String("credentials_directory", "", "Path to the creds directory of this repo")
	flag.Parse()
	if *credentialsDirectory == "" {
		fmt.Println("Must set credentials_directory argument")
		os.Exit(1)
	}
	tlsServers(*credentialsDirectory)
	insecureServer()
	for {
		time.Sleep(1 * time.Second)
	}
}
