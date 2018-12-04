/*
 *
 * Copyright 2018 gRPC authors.
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

package main

import (
	"context"
	"log"
	"net"

	"google.golang.org/grpc/codes"

	"google.golang.org/grpc"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	rgpb "google.golang.org/grpc/examples/route_guide/routeguide"
	"google.golang.org/grpc/status"
)

const (
	port = ":50051"
)

// hwServer is used to implement helloworld.GreeterServer.
type hwServer struct{}

// SayHello implements helloworld.GreeterServer
func (s *hwServer) SayHello(ctx context.Context, in *hwpb.HelloRequest) (*hwpb.HelloReply, error) {
	return &hwpb.HelloReply{Message: "Hello " + in.Name}, nil
}

type rgServer struct{}

func (s *rgServer) GetFeature(ctx context.Context, point *rgpb.Point) (*rgpb.Feature, error) {
	return &rgpb.Feature{Name: "Unknown", Location: point}, nil
}

func (s *rgServer) ListFeatures(rect *rgpb.Rectangle, stream rgpb.RouteGuide_ListFeaturesServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *rgServer) RecordRoute(stream rgpb.RouteGuide_RecordRouteServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

func (s *rgServer) RouteChat(stream rgpb.RouteGuide_RouteChatServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()

	// Register Greeter on the server.
	hwpb.RegisterGreeterServer(s, &hwServer{})

	// Register RouteGuide on the same server.
	rgpb.RegisterRouteGuideServer(s, &rgServer{})

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
