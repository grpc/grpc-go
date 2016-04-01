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
	"io"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/metadata/helloworld"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

const (
	address         = "localhost:9527"
	timestampFormat = time.StampNano // "Jan _2 15:04:05.000"
)

func unaryCallWithMetadata(c pb.GreeterClient, name string) {
	grpclog.Printf("------------ unary ------------")
	// create metadata and context
	md := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
	ctx := metadata.NewContext(context.Background(), md)

	// call RPC
	var header, trailer metadata.MD
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name}, grpc.Header(&header), grpc.Trailer(&trailer))
	if err != nil {
		grpclog.Fatalf("failed to call SayHello: %v", err)
	}

	if t, ok := header["timestamp"]; ok {
		grpclog.Printf("timestamp from header:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
	if l, ok := header["location"]; ok {
		grpclog.Printf("location from header:")
		for i, e := range l {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
	grpclog.Printf("message:")
	grpclog.Printf(" - %s", r.Message)
	if t, ok := trailer["timestamp"]; ok {
		grpclog.Printf("timestamp from trailer:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
}

func serverStreamingWithMetadata(c pb.GreeterClient, names []string) {
	grpclog.Printf("------------ server streaming ------------")
	// create metadata and context
	md := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
	ctx := metadata.NewContext(context.Background(), md)

	// call RPC
	stream, err := c.ServerStreamingSayHello(ctx, &pb.StreamingHelloRequest{Names: names})
	if err != nil {
		grpclog.Fatalf("failed to call ServerStreamingSayHello: %v", err)
	}

	// read header
	header, err := stream.Header()
	if err != nil {
		grpclog.Fatalf("failed to get header from stream: %v", err)
	}
	if t, ok := header["timestamp"]; ok {
		grpclog.Printf("timestamp from header:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
	if l, ok := header["location"]; ok {
		grpclog.Printf("location from header:")
		for i, e := range l {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// read response
	var rpcStatus error
	grpclog.Printf("message:")
	for {
		r, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		grpclog.Printf(" - %s", r.Message)
	}
	if rpcStatus != io.EOF {
		grpclog.Fatalf("failed to finish server streaming: %v", rpcStatus)
	}

	// read trailer
	trailer := stream.Trailer()
	if t, ok := trailer["timestamp"]; ok {
		grpclog.Printf("timestamp from trailer:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
}

func clientStreamWithMetadata(c pb.GreeterClient, names []string) {
	grpclog.Printf("------------ client streaming ------------")
	// create metadata and context
	md := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
	ctx := metadata.NewContext(context.Background(), md)

	// call RPC
	stream, err := c.ClientStreamingSayHello(ctx)
	if err != nil {
		grpclog.Fatalf("failed to call ClientStreamingSayHello: %v\n", err)
	}

	// read header
	header, err := stream.Header()
	if err != nil {
		grpclog.Fatalf("failed to get header from stream: %v", err)
	}
	if t, ok := header["timestamp"]; ok {
		grpclog.Printf("timestamp from header:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
	if l, ok := header["location"]; ok {
		grpclog.Printf("location from header:")
		for i, e := range l {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

	// send request to stream
	for _, name := range names {
		if err := stream.Send(&pb.HelloRequest{Name: name}); err != nil {
			grpclog.Fatalf("failed to send streaming: %v\n", err)
		}
	}

	// read response
	r, err := stream.CloseAndRecv()
	if err != nil {
		grpclog.Fatalf("failed to CloseAndRecv: %v\n", err)
	}
	grpclog.Printf("message:")
	for _, m := range r.Messages {
		grpclog.Printf(" - %s\n", m)
	}

	// read trailer
	trailer := stream.Trailer()
	if t, ok := trailer["timestamp"]; ok {
		grpclog.Printf("timestamp from trailer:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}
}

func bidirectionalWithMetadata(c pb.GreeterClient, names []string) {
	grpclog.Printf("------------ bidirectional ------------")
	// create metadata and context
	md := metadata.Pairs("timestamp", time.Now().Format(timestampFormat))
	ctx := metadata.NewContext(context.Background(), md)

	// call RPC
	stream, err := c.BidirectionalStreamingSayHello(ctx)
	if err != nil {
		grpclog.Fatalf("failed to call BidirectionalStreamingSayHello: %v\n", err)
	}

	go func() {
		// read header
		header, err := stream.Header()
		if err != nil {
			grpclog.Fatalf("failed to get header from stream: %v", err)
		}
		if t, ok := header["timestamp"]; ok {
			grpclog.Printf("timestamp from header:")
			for i, e := range t {
				grpclog.Printf(" %d. %s", i, e)
			}
		}
		if l, ok := header["location"]; ok {
			grpclog.Printf("location from header:")
			for i, e := range l {
				grpclog.Printf(" %d. %s", i, e)
			}
		}

		// send request
		for _, name := range names {
			if err := stream.Send(&pb.HelloRequest{Name: name}); err != nil {
				grpclog.Fatalf("failed to send streaming: %v\n", err)
			}
		}
		stream.CloseSend()
	}()

	// read response
	var rpcStatus error
	grpclog.Printf("message:")
	for {
		r, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		grpclog.Printf(" - %s", r.Message)
	}
	if rpcStatus != io.EOF {
		grpclog.Fatalf("failed to finish server streaming: %v", rpcStatus)
	}

	// read trailer
	trailer := stream.Trailer()
	if t, ok := trailer["timestamp"]; ok {
		grpclog.Printf("timestamp from trailer:")
		for i, e := range t {
			grpclog.Printf(" %d. %s", i, e)
		}
	}

}

var names = []string{
	"Anne",
	"Hope",
	"Margeret",
	"Jamar",
	"Judson",
	"Carrol",
}

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		grpclog.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewGreeterClient(conn)

	unaryCallWithMetadata(c, names[0])
	time.Sleep(1 * time.Second)

	serverStreamingWithMetadata(c, names)
	time.Sleep(1 * time.Second)

	clientStreamWithMetadata(c, names)
	time.Sleep(1 * time.Second)

	bidirectionalWithMetadata(c, names)
}
