/*
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

package engine

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"
)

var (
	port = flag.Int("port", 50051, "the port to serve on")

	// Error codes used in CEL engine interceptors.
	errMissingMetadata        = status.Errorf(codes.InvalidArgument, "missing metadata")
	errMissingPeerInformation = status.Errorf(codes.InvalidArgument, "missing peer information")
	errUnauthorized           = status.Errorf(codes.PermissionDenied, "unauthorized")
)

// Returns whether or not a given context is authorized.
func authorized(ctx context.Context, engine CelEvaluationEngine) (bool, error) {
	// Extract authorization arguments.
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false, errMissingMetadata
	}
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return false, errMissingPeerInformation
	}
	args := AuthorizationArgs{md, peerInfo}

	// Evaluate against CEL engine.
	authDecision := engine.Evaluate(args)
	if authDecision.decision == DecisionDeny {
		return false, nil
	} else if authDecision.decision == DecisionAllow {
		return true, nil
	} else { // DecisionUnknown
		return false, nil
	}
}

// wrappedStream wraps around the embedded grpc.ServerStream, and intercepts the RecvMsg and
// SendMsg method call.
type wrappedStream struct {
	grpc.ServerStream
}

func (w *wrappedStream) RecvMsg(m interface{}) error {
	return w.ServerStream.RecvMsg(m)
}

func (w *wrappedStream) SendMsg(m interface{}) error {
	return w.ServerStream.SendMsg(m)
}

func newWrappedStream(s grpc.ServerStream) grpc.ServerStream {
	return &wrappedStream{s}
}

// Example of how a gRPC user would create a server with interceptors.
// In this example, interceptors are created as first-class functions,
// and evaluation is currently done against an empty instance of
// CelEvaluationEngine. In a real use case, users are expected to
// initialize engine to be created with an actual RBAC policy.
func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create tls based credential.
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		log.Fatalf("failed to create credentials: %v", err)
	}

	// Create CEL engine.
	// User TODO: initialize engine with RBAC policy
	engine := CelEvaluationEngine{}

	// Create interceptors.
	rbacUnaryInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// authorization (CEL engine)
		auth, err := authorized(ctx, engine)
		if err != nil {
			return nil, err
		} else if !auth {
			return nil, errUnauthorized
		}
		// invoking handler
		m, err := handler(ctx, req)
		return m, err
	}

	rbacStreamInterceptor := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// authorization (CEL engine)
		auth, err := authorized(ss.Context(), engine)
		if err != nil {
			return err
		} else if !auth {
			return errUnauthorized
		}
		// invoking handler
		err = handler(srv, newWrappedStream(ss))
		return err
	}

	// Create the server.
	s := grpc.NewServer(grpc.Creds(creds), grpc.UnaryInterceptor(rbacUnaryInterceptor), grpc.StreamInterceptor(rbacStreamInterceptor))
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
