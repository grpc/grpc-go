/*
 *
 * Copyright 2024 gRPC authors.
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

// Binary server demonstrates how to handle canceled contexts when a client
// cancels an in-flight RPC.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/transport"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var port = flag.Int("port", 50051, "the port to serve on")

type server struct {
	pb.UnimplementedEchoServer
}

func workOnMessage(ctx context.Context, msg string) {
	fmt.Printf("starting work on message: %q\n", msg)
	i := 0
	for {
		if err := ctx.Err(); err != nil {
			cause := context.Cause(ctx)
			fmt.Printf("'%v' with cause '%v', message worker for message %q stopping.\n", err, cause, msg)
			var httpErr *transport.HTTP2CodeError
			if errors.As(cause, &httpErr) {
				switch httpErr.Code {
				case http2.ErrCodeNo:
					return
				default:
					fmt.Printf("unexpected HTTP/2 error: %v", httpErr)
				}
			}
			return
		}
		// simulate work on message but don't flood the logs
		i++
	}
}

func (s *server) BidirectionalStreamingEcho(stream pb.Echo_BidirectionalStreamingEchoServer) error {
	ctx := stream.Context()
	for {
		recv, err := stream.Recv()
		if err != nil {
			fmt.Printf("server: error receiving from stream: %v\n", err)
			if err == io.EOF {
				return nil
			}
			return err
		}
		msg := recv.Message
		go workOnMessage(ctx, msg)
		stream.Send(&pb.EchoResponse{Message: msg})
	}
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	fmt.Printf("server listening at port %v\n", lis.Addr())
	s := grpc.NewServer()
	pb.RegisterEchoServer(s, &server{})
	s.Serve(lis)
}
