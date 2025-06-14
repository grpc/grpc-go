/*
 *
 * Copyright 2025 gRPC authors.
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

package jwt_test

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/jwt"
)

// ExampleNewTokenFileCallCredentials demonstrates how to create and use JWT
// token file call credentials for authentication.
func ExampleNewTokenFileCallCredentials() {
	// Create JWT call credentials that read tokens from a file
	creds, err := jwt.NewTokenFileCallCredentials(
		"/path/to/jwt.token", // Path to JWT token file
	)
	if err != nil {
		log.Fatalf("Failed to create JWT credentials: %v", err)
	}

	// Use the credentials when creating a gRPC connection
	conn, err := grpc.NewClient(
		"service.example.com:443",
		grpc.WithPerRPCCredentials(creds),
		// ... other dial options
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Use the connection for RPC calls
	// The JWT token will be automatically included in the authorization header
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// ... make RPC calls using ctx and conn
	_ = ctx
}
