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
const goodServerPort string = "8885"
const revokedServerPort string = "8884"
const insecurePort string = "8883"
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
	var cert_file string
	if revoked {
		cert_file = filepath.Join(credsDirectory, "client_cert_revoked.pem")
	} else {
		cert_file = filepath.Join(credsDirectory, "client_cert.pem")
	}
	identityOptions := pemfile.Options{
		CertFile:        cert_file,
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

func TlsWithCrlsToGoodServer(credsDirectory string) {
	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()
	identityProvider := makeIdentityProvider(false, credsDirectory)
	defer identityProvider.Close()
	crlProvider := makeCrlProvider(credsDirectory)
	defer crlProvider.Close()

	fmt.Println("Client running against good server.")
	runClientWithProviders(rootProvider, identityProvider, crlProvider, goodServerPort, false)
}

func TlsWithCrlsToRevokedServer(credsDirectory string) {
	rootProvider := makeRootProvider(credsDirectory)
	defer rootProvider.Close()
	identityProvider := makeIdentityProvider(false, credsDirectory)
	defer identityProvider.Close()
	crlProvider := makeCrlProvider(credsDirectory)
	defer crlProvider.Close()

	fmt.Println("Client running against revoked server.")
	runClientWithProviders(rootProvider, identityProvider, crlProvider, revokedServerPort, true)
}

func TlsWithCrls(credsDirectory string) {
	fmt.Println("---------- Running TLS with CRLs to Good Server ----------")
	TlsWithCrlsToGoodServer(credsDirectory)
	fmt.Println("---------- Running TLS with CRLs to Revoked Server ----------")
	TlsWithCrlsToRevokedServer(credsDirectory)
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

// --- Custom Verification ---
func customVerificaitonSucceed(info *advancedtls.HandshakeVerificationInfo) (*advancedtls.PostHandshakeVerificationResults, error) {
	// Looks at info for what you care about as the custom verification implementer
	if info.ServerName != "localhost:8885" {
		return nil, fmt.Errorf("expected servername of localhost:8885, got %v", info.ServerName)
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

func CustomVerification(credsDirectory string) {
	fmt.Println("---------- Running TLS with Custom Verification ----------")
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
func CredentialsNewTLSExample(credsDirectory string) {
	fmt.Println("---------- Running client using NewTLS to create Credentials ----------")
	cert, err := tls.LoadX509KeyPair(filepath.Join(credsDirectory, "client_cert.pem"), filepath.Join(credsDirectory, "client_key.pem"))
	if err != nil {
		os.Exit(1)
	}
	rootPem, err := os.ReadFile(filepath.Join(credsDirectory, "ca_cert.pem"))
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
func InsecureCredentialsExample(credsDirectory string) {
	fmt.Println("---------- Running client using Insecure Credentials ----------")
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

	if shouldSucceed {
		if err != nil {
			fmt.Printf("Error during client.UnaryEcho %v\n", err)
		} else {
			fmt.Printf("Response: %v\n", resp.Message)
			if resp.Message != message {
				fmt.Println("Didn't get correct response")
			}
		}
	} else {
		// This should fail
		if err == nil {
			fmt.Printf("Should have failed but didn't, got response: %v\n", resp)
		} else {
			fmt.Printf("Handshake failed expectedly with error: %v\n", err)
		}
	}

}
func main() {
	credsDirectory := flag.String("credentials_directory", "", "Path to the creds directory of this example repo")
	flag.Parse()

	if *credsDirectory == "" {
		fmt.Println("Must set credentials_directory argument to this repo's creds directory")
		os.Exit(1)
	}
	TlsWithCrls(*credsDirectory)
	CustomVerification(*credsDirectory)
	CredentialsNewTLSExample(*credsDirectory)
	InsecureCredentialsExample(*credsDirectory)
}
