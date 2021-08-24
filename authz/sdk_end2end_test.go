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

// Package authz_test contains tests for authz.
//
// Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
package authz_test

import (
	"context"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/authz"
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

func (s *testServer) StreamingInputCall(stream pb.TestService_StreamingInputCallServer) error {
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.StreamingInputCallResponse{})
		}
		if err != nil {
			return err
		}
	}
}

func TestSDKEnd2End(t *testing.T) {
	tests := map[string]struct {
		authzPolicy    string
		md             metadata.MD
		wantStatusCode codes.Code
		wantErr        string
	}{
		"DeniesRpcRequestMatchInDenyNoMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": 
				[
					{
						"name": "allow_TestServiceCalls",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall",
								"/grpc.testing.TestService/StreamingOutputCall"
							]
						}
					}
				],
				"deny_rules":
				[
					{
						"name": "deny_TestServiceCalls",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall",
								"/grpc.testing.TestService/StreamingInputCall"
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
			wantErr:        "Unauthorized RPC request rejected.",
		},
		"DeniesRpcRequestMatchInDenyAndAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_TestServiceCalls",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/*"
							]
						}
					}
				],
				"deny_rules":
				[
					{
						"name": "deny_TestServiceCalls",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/*"
							]
						}
					}
				]
			}`,
			wantStatusCode: codes.PermissionDenied,
			wantErr:        "Unauthorized RPC request rejected.",
		},
		"AllowsRpcRequestNoMatchInDenyMatchInAllow": {
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
						"name": "deny_TestServiceCalls",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall",
								"/grpc.testing.TestService/StreamingInputCall"
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
			md:             metadata.Pairs("key-xyz", "val-xyz"),
			wantStatusCode: codes.OK,
		},
		"AllowsRpcRequestNoMatchInDenyAndAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_some_user",
						"source": {
							"principals":
							[
								"some_user"
							]
						}
					}
				],
				"deny_rules":
				[
					{
						"name": "deny_StreamingOutputCall",
						"request": {
							"paths":
							[
								"/grpc.testing.TestService/StreamingOutputCall"
							]
						}
					}
				]
			}`,
			wantStatusCode: codes.PermissionDenied,
			wantErr:        "Unauthorized RPC request rejected.",
		},
		"AllowsRpcRequestEmptyDenyMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_UnaryCall",
						"request":
						{
							"paths":
							[
								"/grpc.testing.TestService/UnaryCall"
							]
						}
					},
					{
						"name": "allow_StreamingInputCall",
						"request":
						{
							"paths":
							[
								"/grpc.testing.TestService/StreamingInputCall"
							]
						}
					}
				]
			}`,
			wantStatusCode: codes.OK,
		},
		"DeniesRpcRequestEmptyDenyNoMatchInAllow": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_StreamingOutputCall",
						"request": 
						{
							"paths":
							[
								"/grpc.testing.TestService/StreamingOutputCall"
							]
						}
					}
				]
			}`,
			wantStatusCode: codes.PermissionDenied,
			wantErr:        "Unauthorized RPC request rejected.",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			i, _ := authz.NewStatic(test.authzPolicy)
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

			dialOptions := []grpc.DialOption{
				grpc.WithInsecure(),
				grpc.WithBlock(),
			}
			clientConn, err := grpc.Dial(lis.Addr().String(), dialOptions...)
			if err != nil {
				t.Fatalf("grpc.Dial(%v, %v) failed: %v", lis.Addr().String(), dialOptions, err)
			}
			defer clientConn.Close()
			client := pb.NewTestServiceClient(clientConn)

			// Verifying Unary RPC
			unaryCtx := metadata.NewOutgoingContext(context.Background(), test.md)
			_, err = client.UnaryCall(unaryCtx, &pb.SimpleRequest{}, grpc.WaitForReady(true))
			gotStatus, _ := status.FromError(err)
			if gotStatus.Code() != test.wantStatusCode {
				t.Fatalf("[UnaryCall] status code want:%v got:%v", test.wantStatusCode, gotStatus.Code())
			}
			if gotStatus.Message() != test.wantErr {
				t.Fatalf("[UnaryCall] error message want:%v got:%v", test.wantErr, gotStatus.Message())
			}

			// Verifying Streaming RPC
			streamCtx := metadata.NewOutgoingContext(context.Background(), test.md)
			stream, err := client.StreamingInputCall(streamCtx, grpc.WaitForReady(true))
			if err != nil {
				t.Fatalf("Failed StreamingInputCall err:%v", err)
			}
			req := &pb.StreamingInputCallRequest{}
			if err := stream.Send(req); err != nil {
				t.Fatalf("stream.Send failed err: %v", err)
			}
			_, err = stream.CloseAndRecv()
			gotStatus, _ = status.FromError(err)
			if gotStatus.Code() != test.wantStatusCode {
				t.Fatalf("[StreamingCall] status code want:%v got:%v", test.wantStatusCode, gotStatus.Code())
			}
			if gotStatus.Message() != test.wantErr {
				t.Fatalf("[StreamingCall] error message want:%v got:%v", test.wantErr, gotStatus.Message())
			}

		})
	}
}
