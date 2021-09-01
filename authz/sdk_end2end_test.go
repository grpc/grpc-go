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

package authz_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

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
						"name": "allow_StreamingOutputCall",
						"request": {
							"paths":
							[
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
			wantErr:        "unauthorized RPC request rejected",
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
			wantErr:        "unauthorized RPC request rejected",
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
			wantErr:        "unauthorized RPC request rejected",
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
			wantErr:        "unauthorized RPC request rejected",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Start a gRPC server with SDK unary and stream server interceptors.
			i, _ := authz.NewStatic(test.authzPolicy)
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("error listening: %v", err)
			}
			s := grpc.NewServer(
				grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
				grpc.ChainStreamInterceptor(i.StreamInterceptor))
			pb.RegisterTestServiceServer(s, &testServer{})
			go s.Serve(lis)

			// Establish a connection to the server.
			clientConn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
			if err != nil {
				t.Fatalf("grpc.Dial(%v) failed: %v", lis.Addr().String(), err)
			}
			defer clientConn.Close()
			client := pb.NewTestServiceClient(clientConn)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx = metadata.NewOutgoingContext(ctx, test.md)

			// Verifying authorization decision for Unary RPC.
			_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
			if got := status.Convert(err); got.Code() != test.wantStatusCode || got.Message() != test.wantErr {
				t.Fatalf("[UnaryCall] error want:{%v %v} got:{%v %v}", test.wantStatusCode, test.wantErr, got.Code(), got.Message())
			}

			// Verifying authorization decision for Streaming RPC.
			stream, err := client.StreamingInputCall(ctx)
			if err != nil {
				t.Fatalf("failed StreamingInputCall err: %v", err)
			}
			req := &pb.StreamingInputCallRequest{
				Payload: &pb.Payload{
					Body: []byte("hi"),
				},
			}
			if err := stream.Send(req); err != nil && err != io.EOF {
				t.Fatalf("failed stream.Send err: %v", err)
			}
			_, err = stream.CloseAndRecv()
			if got := status.Convert(err); got.Code() != test.wantStatusCode || got.Message() != test.wantErr {
				t.Fatalf("[StreamingCall] error want:{%v %v} got:{%v %v}", test.wantStatusCode, test.wantErr, got.Code(), got.Message())
			}
		})
	}
}
