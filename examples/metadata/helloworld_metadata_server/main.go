/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package main

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/examples/metadata/helloworld"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

const (
	port            = ":9527"
	timestampFormat = time.StampNano
	smallDuration   = time.Second
)

var greetingWords = []string{
	"Aloha",
	"Ahoy",
	"Bonjour",
	"G'day",
	"Hello",
	"Hey",
	"Hi",
	"Hola",
	"Howdy",
	"Sup",
	"What's up",
	"Yo",
}

type server struct{}

// SayHello implements unary call handler with metadata handling.
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	grpclog.Printf("----------- SayHello -----------")
	// create trailer, using defer to record timestamp of function return
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		grpc.SetTrailer(ctx, trailer)
	}()

	// read metadata from client
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return nil, grpc.Errorf(codes.DataLoss, "SayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		grpclog.Printf("timestamp from metadata:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// create and send header
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	grpc.SendHeader(ctx, header)

	grpclog.Printf("request received: %v, sending greeting", in)

	return &pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name)}, nil
}

// ServerStreamingSayHello implements server streaming handler with metadata handling.
func (s *server) ServerStreamingSayHello(in *pb.StreamingHelloRequest, stream pb.Greeter_ServerStreamingSayHelloServer) error {
	grpclog.Printf("----------- ServerStreamingSayHello -----------")
	// create trailer, using defer to record timestamp of function return
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// read metadata from client
	md, ok := metadata.FromContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "ServerStreamingSayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		grpclog.Printf("timestamp from metadata:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// create and send header
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	grpclog.Printf("request received: %v\n", in)

	// read request and send response
	for _, name := range in.Names {
		grpclog.Printf("sending greeting for %v\n", name)
		err := stream.Send(&pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], name)})
		if err != nil {
			return err
		}
	}
	return nil
}

// ClientStreamingSayHello implements client streaming handler with metadata handling
func (s *server) ClientStreamingSayHello(stream pb.Greeter_ClientStreamingSayHelloServer) error {
	grpclog.Printf("----------- ClientStreamingSayHello -----------")
	// create trailer, using defer to record timestamp of function return
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// read metadata from client
	md, ok := metadata.FromContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "ServerStreamingSayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		grpclog.Printf("timestamp from metadata:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// create and send header
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	// read request and send response
	var messages []string
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			grpclog.Printf("sending all greetings")
			return stream.SendAndClose(&pb.StreamingHelloReply{Messages: messages})
		}
		grpclog.Printf("request received: %v, building greeting", in)
		if err != nil {
			return err
		}
		messages = append(messages, fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name))
	}
}

// BidirectionalStreamingSayHello implements bidirectional streaming handler with metadata handling
func (s *server) BidirectionalStreamingSayHello(stream pb.Greeter_BidirectionalStreamingSayHelloServer) error {
	grpclog.Printf("----------- BidirectionalStreamingSayHello -----------")
	// create trailer, using defer to record timestamp of function return
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// read metadata from client
	md, ok := metadata.FromContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "BidirectionalStreamingSayHello: failed to get metadata")
	}

	if t, ok := md["timestamp"]; ok {
		grpclog.Printf("timestamp from metadata:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// create and send header
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	// read request and send response
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		grpclog.Printf("request received %v, sending greeting", in)
		if err := stream.Send(&pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name)}); err != nil {
			return err
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	lis, err := net.Listen("tcp", port)
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpclog.Printf("server listening at port %v", port)

	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	s.Serve(lis)
}
