/*
 *
 * Copyright 2021 gRPC authors.
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

package authz

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	pb "google.golang.org/grpc/test/grpc_testing"
)

type testServer struct {
	pb.UnimplementedTestServiceServer
}

func (s *testServer) UnaryCall(ctx context.Context, req *pb.SimpleRequest) (*pb.SimpleResponse, error) {
	return &pb.SimpleResponse{}, nil
}

func startServer(t *testing.T, policy string) string {
	i, _ := NewStatic(policy)
	serverOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
		grpc.ChainStreamInterceptor(i.StreamInterceptor),
	}
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	s := grpc.NewServer(serverOpts...)
	pb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	return lis.Addr().String()
}

func runClient(ctx context.Context, t *testing.T, serverAddr string) (*pb.SimpleResponse, error) {
	dialOptions := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}
	clientConn, err := grpc.Dial(serverAddr, dialOptions...)
	if err != nil {
		t.Fatalf("grpc.Dial(%v, %v) failed: %v", serverAddr, dialOptions, err)
	}
	defer clientConn.Close()
	c := pb.NewTestServiceClient(clientConn)
	return c.UnaryCall(ctx, &pb.SimpleRequest{}, grpc.WaitForReady(true))
}

func TestSdkEnd2End(t *testing.T) {
	tests := map[string]struct {
		authzPolicy    string
		md             metadata.MD
		wantStatusCode codes.Code
	}{
		"DeniesUnauthorizedRpcRequest": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": 
				[
					{
						"name": "allow_all"
					}
				],
				"deny_rules": 
				[
					{
						"name": "deny_Echo",
						"request": {
							"paths": 
							[
								"/grpc.testing.TestService/UnaryCall"
							],
							"headers": 
							[
								{
									"key": "key-abc",
									"values": 
									[
										"val-abc",
										"val-def"
									]
								}
							]
						}
					}
				]
			}`,
			md:             metadata.Pairs("key-abc", "val-abc"),
			wantStatusCode: codes.PermissionDenied,
		},
		"AllowsAuthorizedRpcRequest": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": 
				[
					{
						"name": "allow_Echo",
						"request": 
						{
							"paths": 
							[
								"/grpc.testing.TestService/UnaryCall"
							]
						}
					}
				],
				"deny_rules": 
				[
					{
						"name": "deny_all",
						"request": 
						{
							"headers": 
							[
								{
									"key": "key-abc",
									"values": 
									[
										"val-abc",
										"val-def"
									]
								}
							]
						}
					}
				]
			}`,
			md:             metadata.Pairs("key-xyz", "val-xyz"),
			wantStatusCode: codes.OK,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			serverAddr := startServer(t, test.authzPolicy)
			ctx := metadata.NewOutgoingContext(context.Background(), test.md)
			_, err := runClient(ctx, t, serverAddr)
			if gotStatusCode := status.Code(err); gotStatusCode != test.wantStatusCode {
				t.Fatalf("unexpected authorization decision. status code want:%v got:%v", test.wantStatusCode, gotStatusCode)
			}
		})
	}
}
