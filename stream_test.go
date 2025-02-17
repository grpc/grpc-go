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

package grpc_test

import (
	"context"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestStream_Header_TrailersOnly(t *testing.T) {
	ss := stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			return status.Errorf(codes.NotFound, "a test error")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	s, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatal("Error staring call", err)
	}
	if md, err := s.Header(); md != nil || err != nil {
		t.Fatalf("s.Header() = %v, %v; want nil, nil", md, err)
	}
	if _, err := s.Recv(); status.Code(err) != codes.NotFound {
		t.Fatalf("s.Recv() = _, %v; want _, err.Code()=codes.NotFound", err)
	}
}

func TestCardinalityViolation(t *testing.T) {
	ss := stubserver.StubServer{
		StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
			stream.Recv()
			// if err != nil {
			// 	return err
			// }
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()
	// Establish a connection to the server.
	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%v) failed: %v", ss.Address, err)
	}
	defer cc.Close()
	client := testgrpc.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// ctx = metadata.NewOutgoingContext(ctx, test.md)

	// Verifying authorization decision for Streaming RPC.
	stream, err := client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("failed StreamingInputCall err: %v", err)
	}
	req := &testpb.StreamingInputCallRequest{
		Payload: &testpb.Payload{
			Body: []byte("hi"),
		},
	}
	err = stream.Send(req)
	if err != nil {
		t.Fatalf("failed stream.Send err: %v", err)
	}
	_, err = stream.CloseAndRecv()
	t.Fatalf("error should have been received , %v", err)
	if err == nil {
		t.Fatalf("error should have been received , %v", err)
	}
}

func TestCardinalityViolation2(t *testing.T) {
	ss := stubserver.StubServer{
		StreamingInputCallF: func(stream testgrpc.TestService_StreamingInputCallServer) error {
			stream.SendAndClose(&testgrpc.StreamingInputCallResponse{})
			_, err := stream.Recv()
			return err
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()
	// Establish a connection to the server.
	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%v) failed: %v", ss.Address, err)
	}
	defer cc.Close()
	client := testgrpc.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// ctx = metadata.NewOutgoingContext(ctx, test.md)

	// Verifying authorization decision for Streaming RPC.
	stream, err := client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("failed StreamingInputCall err: %v", err)
	}
	req := &testpb.StreamingInputCallRequest{
		Payload: &testpb.Payload{
			Body: []byte("hi"),
		},
	}
	for {
		if err := stream.Send(req); err != nil && err != io.EOF {
			t.Fatalf("failed stream.Send err: %v", err)
		}
	}
	// _, err = stream.CloseAndRecv()
	// t.Fatalf("error should have been received , %v", err)
	// if err == nil {
	// 	t.Fatalf("error should have been received , %v", err)
	// }
}
