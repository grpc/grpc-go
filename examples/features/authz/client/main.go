/*
 *
 * Copyright 2023 gRPC authors.
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

// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/examples/data"
	"google.golang.org/grpc/examples/features/authz/token"
	ecpb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")

func callUnaryEcho(ctx context.Context, client ecpb.EchoClient, message string, opts ...grpc.CallOption) error {
	resp, err := client.UnaryEcho(ctx, &ecpb.EchoRequest{Message: message}, opts...)
	if err != nil {
		return fmt.Errorf("UnaryEcho RPC failed: %w", err)
	}
	fmt.Println("UnaryEcho: ", resp.Message)
	return nil
}

func callBidiStreamingEcho(ctx context.Context, client ecpb.EchoClient, opts ...grpc.CallOption) error {
	c, err := client.BidirectionalStreamingEcho(ctx, opts...)
	if err != nil {
		return fmt.Errorf("BidirectionalStreamingEcho RPC failed: %w", err)
	}
	for i := 0; i < 5; i++ {
		if err := c.Send(&ecpb.EchoRequest{Message: fmt.Sprintf("Request %d", i+1)}); err != nil {
			return fmt.Errorf("sending StreamingEcho message: %w", err)
		}
	}
	c.CloseSend()
	for {
		resp, err := c.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receiving StreamingEcho message: %w", err)
		}
		fmt.Println("BidiStreaming Echo: ", resp.Message)
	}
	return nil
}

func newCredentialsCallOption(t token.Token) grpc.CallOption {
	tokenBase64, err := t.Encode()
	if err != nil {
		log.Fatalf("encoding token: %v", err)
	}
	oath2Token := oauth2.Token{AccessToken: tokenBase64}
	return grpc.PerRPCCredentials(oauth.TokenSource{TokenSource: oauth2.StaticTokenSource(&oath2Token)})
}

func main() {
	flag.Parse()

	// Create tls based credential.
	creds, err := credentials.NewClientTLSFromFile(data.Path("x509/ca_cert.pem"), "x.test.example.com")
	if err != nil {
		log.Fatalf("failed to load credentials: %v", err)
	}
	// Set up a connection to the server.
	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("grpc.Dial(%q): %v", *addr, err)
	}
	defer conn.Close()

	// Make a echo client and send RPCs.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := ecpb.NewEchoClient(conn)
	authorisedUserTokenCallOption := newCredentialsCallOption(token.Token{Username: "super-user", Secret: "super-secret"})
	if err := callUnaryEcho(ctx, client, "hello world", authorisedUserTokenCallOption); err != nil {
		log.Fatalf("callUnaryEcho failed with authorised user token: %v", err)
	}
	if err := callBidiStreamingEcho(ctx, client, authorisedUserTokenCallOption); err != nil {
		log.Fatalf("callBidiStreamingEcho failed with authorised user token: %v", err)
	}
	unauthorisedUserTokenCallOption := newCredentialsCallOption(token.Token{Username: "bad-actor", Secret: "super-secret"})
	if err := callUnaryEcho(ctx, client, "hello world", unauthorisedUserTokenCallOption); err != nil {
		log.Printf("callUnaryEcho failed as expected with unauthorised user token: %v", err)
	}
	if err := callBidiStreamingEcho(ctx, client, unauthorisedUserTokenCallOption); err != nil {
		log.Printf("callBidiStreamingEcho failed as expected with unauthorised user token: %v", err)
	}
}
