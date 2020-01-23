package main

import (
	"context"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"testing"
)

func TestHelloWorld(t *testing.T) {
	s := server{}

	// set up test cases
	tests := []struct {
		name string
		want string
	}{
		{
			name: "world",
			want: "Hello world",
		},
		{
			name: "123",
			want: "Hello 123",
		},
	}

	for _, tt := range tests {
		req := &pb.HelloRequest{Name: tt.name}
		resp, err := s.SayHello(context.Background(), req)
		if err != nil {
			t.Errorf("HelloTest(%v) got unexpected error", err)
		}
		if resp.Message != tt.want {
			t.Errorf("HelloText(%v) got %v, wanted %v", tt.name, resp.Message, tt.want)
		}
	}
}
