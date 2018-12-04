package server

import (
	"context"

	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

// New returns an implementation of GreeterServer.
func New() pb.GreeterServer {
	return &server{}
}

// server is used to implement helloworld.GreeterServer.
type server struct{}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}
