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
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/authz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
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

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var sdkTests = map[string]struct {
	authzPolicy string
	md          metadata.MD
	wantStatus  *status.Status
}{
	"DeniesRpcMatchInDenyNoMatchInAllow": {
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
		md:         metadata.Pairs("key-abc", "val-abc"),
		wantStatus: status.New(codes.PermissionDenied, "unauthorized RPC request rejected"),
	},
	"DeniesRpcMatchInDenyAndAllow": {
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
		wantStatus: status.New(codes.PermissionDenied, "unauthorized RPC request rejected"),
	},
	"AllowsRpcNoMatchInDenyMatchInAllow": {
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
		md:         metadata.Pairs("key-xyz", "val-xyz"),
		wantStatus: status.New(codes.OK, ""),
	},
	"DeniesRpcNoMatchInDenyAndAllow": {
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
		wantStatus: status.New(codes.PermissionDenied, "unauthorized RPC request rejected"),
	},
	"AllowsRpcEmptyDenyMatchInAllow": {
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
		wantStatus: status.New(codes.OK, ""),
	},
	"DeniesRpcEmptyDenyNoMatchInAllow": {
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
		wantStatus: status.New(codes.PermissionDenied, "unauthorized RPC request rejected"),
	},
}

func (s) TestSDKStaticPolicyEnd2End(t *testing.T) {
	for name, test := range sdkTests {
		t.Run(name, func(t *testing.T) {
			// Start a gRPC server with SDK unary and stream server interceptors.
			i, _ := authz.NewStatic(test.authzPolicy)
			s := grpc.NewServer(
				grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
				grpc.ChainStreamInterceptor(i.StreamInterceptor))
			defer s.Stop()
			pb.RegisterTestServiceServer(s, &testServer{})

			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("error listening: %v", err)
			}
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
			if got := status.Convert(err); got.Code() != test.wantStatus.Code() || got.Message() != test.wantStatus.Message() {
				t.Fatalf("[UnaryCall] error want:{%v} got:{%v}", test.wantStatus.Err(), got.Err())
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
			if got := status.Convert(err); got.Code() != test.wantStatus.Code() || got.Message() != test.wantStatus.Message() {
				t.Fatalf("[StreamingCall] error want:{%v} got:{%v}", test.wantStatus.Err(), got.Err())
			}
		})
	}
}

func (s) TestSDKFileWatcherEnd2End(t *testing.T) {
	for name, test := range sdkTests {
		t.Run(name, func(t *testing.T) {
			file := createTmpPolicyFile(t, name, []byte(test.authzPolicy))
			i, _ := authz.NewFileWatcher(file, 1*time.Second)
			defer i.Close()

			// Start a gRPC server with SDK unary and stream server interceptors.
			s := grpc.NewServer(
				grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
				grpc.ChainStreamInterceptor(i.StreamInterceptor))
			defer s.Stop()
			pb.RegisterTestServiceServer(s, &testServer{})

			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("error listening: %v", err)
			}
			defer lis.Close()
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
			if got := status.Convert(err); got.Code() != test.wantStatus.Code() || got.Message() != test.wantStatus.Message() {
				t.Fatalf("[UnaryCall] error want:{%v} got:{%v}", test.wantStatus.Err(), got.Err())
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
			if got := status.Convert(err); got.Code() != test.wantStatus.Code() || got.Message() != test.wantStatus.Message() {
				t.Fatalf("[StreamingCall] error want:{%v} got:{%v}", test.wantStatus.Err(), got.Err())
			}
		})
	}
}

func retryUntil(ctx context.Context, tsc pb.TestServiceClient, want *status.Status) (lastErr error) {
	for ctx.Err() == nil {
		_, lastErr = tsc.UnaryCall(ctx, &pb.SimpleRequest{})
		if s := status.Convert(lastErr); s.Code() == want.Code() && s.Message() == want.Message() {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return lastErr
}

func (s) TestSDKFileWatcher_ValidPolicyRefresh(t *testing.T) {
	valid1 := sdkTests["DeniesRpcMatchInDenyAndAllow"]
	file := createTmpPolicyFile(t, "valid_policy_refresh", []byte(valid1.authzPolicy))
	i, _ := authz.NewFileWatcher(file, 100*time.Millisecond)
	defer i.Close()

	// Start a gRPC server with SDK unary server interceptor.
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor))
	defer s.Stop()
	pb.RegisterTestServiceServer(s, &testServer{})

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	defer lis.Close()
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

	// Verifying authorization decision.
	_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
	if got := status.Convert(err); got.Code() != valid1.wantStatus.Code() || got.Message() != valid1.wantStatus.Message() {
		t.Fatalf("error want:{%v} got:{%v}", valid1.wantStatus.Err(), got.Err())
	}

	// Rewrite the file with a different valid authorization policy.
	valid2 := sdkTests["AllowsRpcEmptyDenyMatchInAllow"]
	if err := ioutil.WriteFile(file, []byte(valid2.authzPolicy), os.ModePerm); err != nil {
		t.Fatalf("ioutil.WriteFile(%q) failed: %v", file, err)
	}

	// Verifying authorization decision.
	if got := retryUntil(ctx, client, valid2.wantStatus); got != nil {
		t.Fatalf("error want:{%v} got:{%v}", valid2.wantStatus.Err(), got)
	}
}

func (s) TestSDKFileWatcher_InvalidPolicySkipReload(t *testing.T) {
	valid := sdkTests["DeniesRpcMatchInDenyAndAllow"]
	file := createTmpPolicyFile(t, "invalid_policy_skip_reload", []byte(valid.authzPolicy))
	i, _ := authz.NewFileWatcher(file, 20*time.Millisecond)
	defer i.Close()

	// Start a gRPC server with SDK unary server interceptors.
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor))
	defer s.Stop()
	pb.RegisterTestServiceServer(s, &testServer{})

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	defer lis.Close()
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

	// Verifying authorization decision.
	_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
	if got := status.Convert(err); got.Code() != valid.wantStatus.Code() || got.Message() != valid.wantStatus.Message() {
		t.Fatalf("error want:{%v} got:{%v}", valid.wantStatus.Err(), got.Err())
	}

	// Skips the invalid policy update, and continues to use the valid policy.
	if err := ioutil.WriteFile(file, []byte("{}"), os.ModePerm); err != nil {
		t.Fatalf("ioutil.WriteFile(%q) failed: %v", file, err)
	}

	// Wait 40 ms for background go routine to read updated files.
	time.Sleep(40 * time.Millisecond)

	// Verifying authorization decision.
	_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
	if got := status.Convert(err); got.Code() != valid.wantStatus.Code() || got.Message() != valid.wantStatus.Message() {
		t.Fatalf("error want:{%v} got:{%v}", valid.wantStatus.Err(), got.Err())
	}
}

func (s) TestSDKFileWatcher_RecoversFromReloadFailure(t *testing.T) {
	valid1 := sdkTests["DeniesRpcMatchInDenyAndAllow"]
	file := createTmpPolicyFile(t, "recovers_from_reload_failure", []byte(valid1.authzPolicy))
	i, _ := authz.NewFileWatcher(file, 100*time.Millisecond)
	defer i.Close()

	// Start a gRPC server with SDK unary server interceptors.
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor))
	defer s.Stop()
	pb.RegisterTestServiceServer(s, &testServer{})

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	defer lis.Close()
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

	// Verifying authorization decision.
	_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
	if got := status.Convert(err); got.Code() != valid1.wantStatus.Code() || got.Message() != valid1.wantStatus.Message() {
		t.Fatalf("error want:{%v} got:{%v}", valid1.wantStatus.Err(), got.Err())
	}

	// Skips the invalid policy update, and continues to use the valid policy.
	if err := ioutil.WriteFile(file, []byte("{}"), os.ModePerm); err != nil {
		t.Fatalf("ioutil.WriteFile(%q) failed: %v", file, err)
	}

	// Wait 120 ms for background go routine to read updated files.
	time.Sleep(120 * time.Millisecond)

	// Verifying authorization decision.
	_, err = client.UnaryCall(ctx, &pb.SimpleRequest{})
	if got := status.Convert(err); got.Code() != valid1.wantStatus.Code() || got.Message() != valid1.wantStatus.Message() {
		t.Fatalf("error want:{%v} got:{%v}", valid1.wantStatus.Err(), got.Err())
	}

	// Rewrite the file with a different valid authorization policy.
	valid2 := sdkTests["AllowsRpcEmptyDenyMatchInAllow"]
	if err := ioutil.WriteFile(file, []byte(valid2.authzPolicy), os.ModePerm); err != nil {
		t.Fatalf("ioutil.WriteFile(%q) failed: %v", file, err)
	}

	// Verifying authorization decision.
	if got := retryUntil(ctx, client, valid2.wantStatus); got != nil {
		t.Fatalf("error want:{%v} got:{%v}", valid2.wantStatus.Err(), got)
	}
}
