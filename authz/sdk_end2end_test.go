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
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	reqMessage  = "Hi"
	respMessage = "Hello"
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	hwpb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer.
func (s *server) SayHello(ctx context.Context, in *hwpb.HelloRequest) (*hwpb.HelloReply, error) {
	return &hwpb.HelloReply{Message: respMessage + in.GetName()}, nil
}

func startServer(t *testing.T, policy string) string {
	i, _ := NewStatic(policy)

	serverOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(i.UnaryInterceptor),
		grpc.ChainStreamInterceptor(i.StreamInterceptor),
	}

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Error listening: %v", err)
	}

	s := grpc.NewServer(serverOpts...)
	hwpb.RegisterGreeterServer(s, &server{})

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Errorf("failed to serve %v", err)
		}
	}()
	return lis.Addr().String()
}

func runClient(ctx context.Context, t *testing.T, serverAddr string, wantStatusCode codes.Code, wantResp string) {
	dialOptions := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}
	clientConn, err := grpc.Dial(serverAddr, dialOptions...)
	if err != nil {
		t.Fatalf("grpc.Dial(%v, %v) failed: %v", serverAddr, dialOptions, err)
	}
	defer clientConn.Close()
	c := hwpb.NewGreeterClient(clientConn)
	resp, err := c.SayHello(ctx, &hwpb.HelloRequest{Name: reqMessage}, grpc.WaitForReady(true))
	if gotCode := status.Code(err); gotCode != wantStatusCode {
		t.Fatalf("Unexpected Authorization decision. Status code want:%v got:%v", wantStatusCode, gotCode)
	}
	if resp.GetMessage() != wantResp {
		t.Fatalf("Unexpected Authorization decision. Response message want:%v got:%v", wantResp, resp.GetMessage())
	}
}

func TestDeniesUnauthorizedRequest(t *testing.T) {
	authzPolicy := `{
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
				"name": "deny_SayHello",
				"request": {
					"paths": [
						"/helloworld.Greeter/SayHello"
					],
					"headers": [
						{
							"key": "key-abc",
							"values": [
								"val-abc",
								"val-def"
							]
						}
					]
				}
			}
		]
	}`

	serverAddr := startServer(t, authzPolicy)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("key-abc", "val-abc"))
	runClient(ctx, t, serverAddr, codes.PermissionDenied, "")
}

func TestAllowsAuthorizedRequest(t *testing.T) {
	authzPolicy := `{
		"name": "authz",
		"allow_rules": 
		[
			{
				"name": "allow_SayHello",
				"request": {
					"paths": [
						"/helloworld.Greeter/SayHello"
					]
				}
			}
		],
		"deny_rules": 
		[
			{
				"name": "deny_all",
				"request": {
					"headers": [
						{
							"key": "key-abc",
							"values": [
								"val-abc",
								"val-def"
							]
						}
					]
				}
			}
		]
	}`

	serverAddr := startServer(t, authzPolicy)
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("key-xyz", "val-xyz"))
	runClient(ctx, t, serverAddr, codes.OK, respMessage+reqMessage)
}
