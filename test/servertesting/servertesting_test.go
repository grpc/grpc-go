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

package servertesting_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/servertesting"
	pb "google.golang.org/grpc/test/servertesting/proto"
)

type service struct{}

func (service) Echo(ctx context.Context, in *pb.Request) (*pb.Response, error) {
	return &pb.Response{
		Msg: in.Msg,
	}, nil
}

func ExampleTester() {
	st := servertesting.New()
	defer st.Close()

	// Register as many services as necessary. If only one is needed, rather use
	// NewClientConn() which handles all of the additional steps, and accepts
	// identical arguments to RegisterService().

	// This variable declaration is purely demonstrative of the fact that
	// service implements the interface.
	var svc pb.EchoServiceServer = &service{}
	if err := st.RegisterService(pb.RegisterEchoServiceServer, svc); err != nil {
		fmt.Printf("RegisterService() got err %v", err)
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		st.Serve()
	}()

	conn, err := st.Dial()
	if err != nil {
		fmt.Printf("Testservertesting.Dial() got err %v", err)
		return
	}

	c := pb.NewEchoServiceClient(conn)
	resp, err := c.Echo(context.Background(), &pb.Request{Msg: "hello world"})
	if err != nil {
		fmt.Printf("EchoService.Echo() got err %v", err)
		return
	}
	fmt.Println(resp.Msg)

	// Output: hello world
}

func ExampleNewClientConn() {
	// This variable declaration is purely demonstrative of the fact that
	// service implements the interface.
	var svc pb.EchoServiceServer = &service{}
	conn, cleanup, err := servertesting.NewClientConn(pb.RegisterEchoServiceServer, svc)
	if err != nil {
		fmt.Printf("NewClientConn() got err %v; want nil err", err)
		return
	}
	defer cleanup()

	c := pb.NewEchoServiceClient(conn)

	resp, err := c.Echo(context.Background(), &pb.Request{Msg: "foobar"})
	if err != nil {
		fmt.Printf("EchoService.Echo() got err %v; want nil err", err)
	}
	fmt.Println(resp.Msg)

	// Output: foobar
}

func TestServerOpts(t *testing.T) {
	// Test that ServerOptions are propagated by creating an interceptor that
	// adds a suffix to the message.
	const suffix = " world"
	uiOpt := grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}
		switch r := resp.(type) {
		case *pb.Response:
			return &pb.Response{Msg: fmt.Sprintf("%s%s", r.Msg, suffix)}, nil
		default:
			return nil, status.Errorf(codes.Internal, "unexpected response type %T", r)
		}
	})

	conn, cleanup, err := servertesting.NewClientConn(pb.RegisterEchoServiceServer, &service{}, uiOpt)
	if err != nil {
		t.Fatalf("NewClientConn() got err %v; want nil err", err)
	}
	defer cleanup()

	c := pb.NewEchoServiceClient(conn)
	got, err := c.Echo(context.Background(), &pb.Request{Msg: "hello"})

	if err != nil {
		t.Errorf("EchoService.Echo() with interceptor got err %v; want nil err", err)
	}

	want := &pb.Response{
		Msg: "hello world",
	}
	if !proto.Equal(got, want) {
		t.Errorf("EchoService.Echo(hello) with suffix interceptor; got %v; want %v", got, want)
	}
}

func TestRegisterErrors(t *testing.T) {
	tests := []struct {
		name               string
		fn, implementation interface{}

		// want is a string that must be contained in the error
		// message.
		want string
	}{
		{
			name:           "nil register function",
			fn:             nil,
			implementation: &service{},
			want:           "nil",
		},
		{
			name:           "function-typed-nil register function",
			fn:             (func())(nil),
			implementation: &service{},
			want:           "nil",
		},
		{
			name:           "nil implementation",
			fn:             pb.RegisterEchoServiceServer,
			implementation: nil,
			want:           "nil",
		},
		{
			name:           "typed-nil implementation",
			fn:             pb.RegisterEchoServiceServer,
			implementation: (*service)(nil),
			want:           "nil",
		},
		{
			name:           "incorrect register-function kind",
			fn:             "hello",
			implementation: &service{},
			want:           "function",
		},
		{
			name:           "register function num in",
			fn:             func(_, _, _ string) {},
			implementation: &service{},
			want:           "input",
		},
		{
			name:           "register function num out",
			fn:             func(_, _ string) error { return nil },
			implementation: &service{},
			want:           "returns",
		},
		{
			name:           "register function first parameter not gRPC server",
			fn:             func(_ string, _ pb.EchoServiceServer) {},
			implementation: &service{},
			want:           "*grpc.Server",
		},
		{
			name:           "register function second parameter not service interface",
			fn:             func(_ *grpc.Server, _ string) {},
			implementation: &service{},
			want:           reflect.TypeOf(&service{}).String(),
		},
		{
			name:           "bad service implementation",
			fn:             pb.RegisterEchoServiceServer,
			implementation: struct{}{},
			want:           "struct",
		},
		{
			name: "panic recovery",
			fn: func(_ *grpc.Server, _ pb.EchoServiceServer) {
				panic("don't")
			},
			implementation: &service{},
			want:           "don't",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cleanup, err := servertesting.NewClientConn(tt.fn, tt.implementation)
			defer cleanup()
			if err == nil {
				t.Fatalf("NewClientConn() got nil err; want err")
			}
			if got, want := err.Error(), tt.want; !strings.Contains(got, want) {
				t.Errorf("NewClientConn() got err %q; want containing %q", got, want)
			}
		})
	}
}
