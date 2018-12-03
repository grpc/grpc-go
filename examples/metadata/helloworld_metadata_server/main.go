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
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	pb "google.golang.org/grpc/examples/metadata/helloworld"
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
	log.Printf("----------- SayHello -----------")
	// Create trailer in defer to record function return time.
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		grpc.SetTrailer(ctx, trailer)
	}()

	// Read metadata from client.
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, grpc.Errorf(codes.DataLoss, "SayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		log.Printf("timestamp from metadata:")
		for i, e := range t {
			log.Printf(" %d. %s", i, e)
		}
	}

	// Create and send header.
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	grpc.SendHeader(ctx, header)

	log.Printf("request received: %v, sending greeting", in)

	return &pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name)}, nil
}

// ServerStreamingSayHello implements server streaming handler with metadata handling.
func (s *server) ServerStreamingSayHello(in *pb.StreamingHelloRequest, stream pb.Greeter_ServerStreamingSayHelloServer) error {
	log.Printf("----------- ServerStreamingSayHello -----------")
	// Create trailer in defer to record function return time.
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// Read metadata from client.
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "ServerStreamingSayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		log.Printf("timestamp from metadata:")
		for i, e := range t {
			log.Printf(" %d. %s", i, e)
		}
	}

	// Create and send header.
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	log.Printf("request received: %v\n", in)

	// Read requests and send responses.
	for _, name := range in.Names {
		log.Printf("sending greeting for %v\n", name)
		err := stream.Send(&pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], name)})
		if err != nil {
			return err
		}
	}
	return nil
}

// ClientStreamingSayHello implements client streaming handler with metadata handling
func (s *server) ClientStreamingSayHello(stream pb.Greeter_ClientStreamingSayHelloServer) error {
	log.Printf("----------- ClientStreamingSayHello -----------")
	// Create trailer in defer to record function return time.
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// Read metadata from client.
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "ServerStreamingSayHello: failed to get metadata")
	}
	if t, ok := md["timestamp"]; ok {
		log.Printf("timestamp from metadata:")
		for i, e := range t {
			log.Printf(" %d. %s", i, e)
		}
	}

	// Create and send header.
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	// Read requests and send responses.
	var messages []string
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			log.Printf("sending all greetings")
			return stream.SendAndClose(&pb.StreamingHelloReply{Messages: messages})
		}
		log.Printf("request received: %v, building greeting", in)
		if err != nil {
			return err
		}
		messages = append(messages, fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name))
	}
}

// BidirectionalStreamingSayHello implements bidirectional streaming handler with metadata handling
func (s *server) BidirectionalStreamingSayHello(stream pb.Greeter_BidirectionalStreamingSayHelloServer) error {
	log.Printf("----------- BidirectionalStreamingSayHello -----------")
	// Create trailer in defer to record function return time.
	defer func() {
		trailer := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
		stream.SetTrailer(trailer)
	}()

	// Read metadata from client.
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return grpc.Errorf(codes.DataLoss, "BidirectionalStreamingSayHello: failed to get metadata")
	}

	if t, ok := md["timestamp"]; ok {
		log.Printf("timestamp from metadata:")
		for i, e := range t {
			log.Printf(" %d. %s", i, e)
		}
	}

	// Create and send header.
	header := metadata.New(map[string]string{"location": "MTV", "timestamp": time.Now().Format(timestampFormat)})
	stream.SendHeader(header)

	// Read requests and send responses.
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		log.Printf("request received %v, sending greeting", in)
		if err := stream.Send(&pb.HelloReply{Message: fmt.Sprintf("%s, %s", greetingWords[rand.Intn(len(greetingWords))], in.Name)}); err != nil {
			return err
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("server listening at port %v", port)

	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	s.Serve(lis)
}
