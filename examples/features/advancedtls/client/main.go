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
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pb "google.golang.org/grpc/examples/features/proto/echo"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/tls/certprovider/pemfile"
	"google.golang.org/grpc/security/advancedtls"
)

const credRefreshInterval = 1 * time.Minute
const serverAddr = "localhost"
const goodServerPort string = "50051"
const revokedServerPort string = "50053"
const insecurePort string = "50054"
const message string = "Hello"

// -- TLS --

func makeRootProvider(credsDirectory string) certprovider.Provider {
	rootOptions := pemfile.Options{
		RootFile:        filepath.Join(credsDirectory, "ca_cert.pem"),
		RefreshDuration: credRefreshInterval,
	}
	rootProvider, err := pemfile.NewProvider(rootOptions)
	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}
	return rootProvider
}

func makeIdentityProvider(revoked bool, credsDirectory string) certprovider.Provider {
	var certFile string
	if revoked {
		certFile = filepath.Join(credsDirectory, "client_cert_revoked.pem")
	} else {
		certFile = filepath.Join(credsDirectory, "client_cert.pem")
	}
	identityOptions := pemfile.Options{
		CertFile:        certFile,
		KeyFile:         filepath.Join(credsDirectory, "client_key.pem"),
		RefreshDuration: credRefreshInterval,
	}
	identityProvider, err := pemfile.NewProvider(identityOptions)
	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}
	return identityProvider
}

func runClientWithProviders(rootProvider certprovider.Provider, identityProvider certprovider.Provider, crlProvider advancedtls.CRLProvider, port string, shouldFail bool) {
	options := &advancedtls.Options{
		// Setup the certificates to be used
		IdentityOptions: advancedtls.IdentityCertificateOptions{
			IdentityProvider: identityProvider,
		},
		// Setup the roots to be used
		RootOptions: advancedtls.RootCertificateOptions{
			RootProvider: rootProvider,
		},
		// Tell the client to verify the server cert
		VerificationType: advancedtls.CertVerification,
	}

	// Configure revocation and CRLs
	options.RevocationOptions = &advancedtls.RevocationOptions{
		CRLProvider: crlProvider,
	}

	clientTLSCreds, err := advancedtls.NewClientCreds(options)

	if err != nil {
		fmt.Printf("Error %v\n", err)
		os.Exit(1)
	}
	fullServerAddr := serverAddr + ":" + port
	runWithCredentials(clientTLSCreds, fullServerAddr, !shouldFail)
}

func tlsWithCRLsToGoodServer(credsDirectory string) {
	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()
	identityProvider := makeIdentityProvider(false, credsDirectory)
	defer identityProvider.Close()
	crlProvider := makeCRLProvider(credsDirectory)
	defer crlProvider.Close()

	runClientWithProviders(rootProvider, identityProvider, crlProvider, goodServerPort, false)
}

func tlsWithCRLsToRevokedServer(credsDirectory string) {
	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()
	identityProvider := makeIdentityProvider(false, credsDirectory)
	defer identityProvider.Close()
	crlProvider := makeCRLProvider(credsDirectory)
	defer crlProvider.Close()

	runClientWithProviders(rootProvider, identityProvider, crlProvider, revokedServerPort, true)
}

func tlsWithCRLs(credsDirectory string) {
	tlsWithCRLsToGoodServer(credsDirectory)
	tlsWithCRLsToRevokedServer(credsDirectory)
}

func makeCRLProvider(crlDirectory string) *advancedtls.FileWatcherCRLProvider {
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

// --- Custom Verification ---
func customVerificaitonSucceed(info *advancedtls.HandshakeVerificationInfo) (*advancedtls.PostHandshakeVerificationResults, error) {
	// Looks at info for what you care about as the custom verification implementer
	if info.ServerName != "localhost:50051" {
		return nil, fmt.Errorf("expected servername of localhost:50051, got %v", info.ServerName)
	}
	return &advancedtls.PostHandshakeVerificationResults{}, nil
}

func customVerificaitonFail(info *advancedtls.HandshakeVerificationInfo) (*advancedtls.PostHandshakeVerificationResults, error) {
	// Looks at info for what you care about as the custom verification implementer
	if info.ServerName != "ExampleDesignedToFail" {
		return nil, fmt.Errorf("expected servername of ExampleDesignedToFail, got %v", info.ServerName)
	}
	return &advancedtls.PostHandshakeVerificationResults{}, nil
}

func customVerification(credsDirectory string) {
	runClientWithCustomVerification(credsDirectory, goodServerPort)

}

func runClientWithCustomVerification(credsDirectory string, port string) {
	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()
	identityProvider := makeIdentityProvider(false, credsDirectory)
	defer identityProvider.Close()
	fullServerAddr := serverAddr + ":" + port
	{
		// Run with the custom verification func that will succeed
		options := &advancedtls.Options{
			// Setup the certificates to be used
			IdentityOptions: advancedtls.IdentityCertificateOptions{
				IdentityProvider: identityProvider,
			},
			// Setup the roots to be used
			RootOptions: advancedtls.RootCertificateOptions{
				RootProvider: rootProvider,
			},
			// Tell the client to verify the server cert
			VerificationType:           advancedtls.CertVerification,
			AdditionalPeerVerification: customVerificaitonSucceed,
		}

		clientTLSCreds, err := advancedtls.NewClientCreds(options)

		if err != nil {
			fmt.Printf("Error %v\n", err)
			os.Exit(1)
		}
		runWithCredentials(clientTLSCreds, fullServerAddr, true)
	}
	{
		// Run with the custom verification func that will fail
		options := &advancedtls.Options{
			// Setup the certificates to be used
			IdentityOptions: advancedtls.IdentityCertificateOptions{
				IdentityProvider: identityProvider,
			},
			// Setup the roots to be used
			RootOptions: advancedtls.RootCertificateOptions{
				RootProvider: rootProvider,
			},
			// Tell the client to verify the server cert
			VerificationType:           advancedtls.CertVerification,
			AdditionalPeerVerification: customVerificaitonFail,
		}

		clientTLSCreds, err := advancedtls.NewClientCreds(options)

		if err != nil {
			fmt.Printf("Error %v\n", err)
			os.Exit(1)
		}
		runWithCredentials(clientTLSCreds, fullServerAddr, false)
	}
}

// -- credentials.NewTLS example --
func credentialsNewTLSExample(credsDirectory string) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(credsDirectory, "client_cert.pem"), filepath.Join(credsDirectory, "client_key.pem"))
	if err != nil {
		os.Exit(1)
	}
	rootPem, err := os.ReadFile(filepath.Join(credsDirectory, "ca_cert.pem"))
	if err != nil {
		os.Exit(1)
	}
	root := x509.NewCertPool()
	if !root.AppendCertsFromPEM(rootPem) {
		os.Exit(1)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      root,
	}

	// Directly create credentials from a tls.Config.
	creds := credentials.NewTLS(config)
	port := goodServerPort
	fullServerAddr := serverAddr + ":" + port
	runWithCredentials(creds, fullServerAddr, true)

}

// -- Insecure --
func insecureCredentialsExample() {
	creds := insecure.NewCredentials()
	port := insecurePort
	fullServerAddr := serverAddr + ":" + port
	runWithCredentials(creds, fullServerAddr, true)
}

// -- Main and Runner --

// All of these examples differ in how they configure the
// credentials.TransportCredentials object. Once we have that, actually making
// the calls with gRPC is the same.
func runWithCredentials(creds credentials.TransportCredentials, fullServerAddr string, shouldSucceed bool) {
	conn, err := grpc.NewClient(fullServerAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		fmt.Printf("Error during grpc.NewClient %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	client := pb.NewEchoClient(conn)
	req := &pb.EchoRequest{
		Message: message,
	}
	context, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := client.UnaryEcho(context, req)
	defer cancel()

	if shouldSucceed && err != nil {
		fmt.Printf("Error during client.UnaryEcho %v\n", err)
	} else if !shouldSucceed && err == nil {
		fmt.Printf("Should have failed but didn't, got response: %v\n", resp)
	}

}
func main() {
	credsDirectory := flag.String("credentials_directory", "", "Path to the creds directory of this example repo")
	flag.Parse()

	if *credsDirectory == "" {
		fmt.Println("Must set credentials_directory argument to this repo's creds directory")
		os.Exit(1)
	}
	tlsWithCRLs(*credsDirectory)
	customVerification(*credsDirectory)
	credentialsNewTLSExample(*credsDirectory)
	insecureCredentialsExample()
}
