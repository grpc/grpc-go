/*
 *
 * Copyright 2022 gRPC authors.
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

// Binary server is an example server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var port = flag.Int("port", 50051, "the port to serve on")

var errMissingMetadata = status.Errorf(codes.InvalidArgument, "no incoming metadata in rpc context")

type server struct {
	pb.UnimplementedEchoServer
}

func unaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errMissingMetadata
	}

	md.Append("key1", "value1")
	ctx = metadata.NewIncomingContext(ctx, md)

	return handler(ctx, req)
}

func (s *server) UnaryEcho(ctx context.Context, in *pb.EchoRequest) (*pb.EchoResponse, error) {
	fmt.Printf("--- UnaryEcho ---\n")

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "UnaryEcho: missing incoming metadata in rpc context")
	}

	// Read and print metadata added by the interceptor.
	if v, ok := md["key1"]; ok {
		fmt.Printf("key1 from metadata: \n")
		for i, e := range v {
			fmt.Printf(" %d. %s\n", i, e)
		}
	}

	return &pb.EchoResponse{Message: in.Message}, nil
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context {
	return s.ctx
}

func streamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	md, ok := metadata.FromIncomingContext(ss.Context())
	if !ok {
		return errMissingMetadata
	}

	md.Append("key1", "value1")
	ctx := metadata.NewIncomingContext(ss.Context(), md)

	return handler(srv, &wrappedStream{ss, ctx})
}

func (s *server) BidirectionalStreamingEcho(stream pb.Echo_BidirectionalStreamingEchoServer) error {
	fmt.Printf("--- BidirectionalStreamingEcho ---\n")

	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Errorf(codes.Internal, "BidirectionalStreamingEcho: missing incoming metadata in rpc context")
	}

	// Read and print metadata added by the interceptor.
	if v, ok := md["key1"]; ok {
		fmt.Printf("key1 from metadata: \n")
		for i, e := range v {
			fmt.Printf(" %d. %s\n", i, e)
		}
	}

	// Read requests and send responses.
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err = stream.Send(&pb.EchoResponse{Message: in.Message}); err != nil {
			return err
		}
	}
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("net.Listen() failed: %v", err)
	}
	fmt.Printf("Server listening at %v\n", lis.Addr())

	s := grpc.NewServer(grpc.UnaryInterceptor(unaryInterceptor), grpc.StreamInterceptor(streamInterceptor))
	pb.RegisterEchoServer(s, &server{})
	s.Serve(lis)
}
