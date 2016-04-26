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
	"flag"
	"io"
	"net"
	"runtime"
	"strconv"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

var (
	driverPort = flag.Int("driver_port", 10000, "port for communication with driver")
	serverPort = flag.Int("server_port", 0, "default port for benchmark server")
)

type byteBufCodec struct {
}

func (byteBufCodec) Marshal(v interface{}) ([]byte, error) {
	return v.([]byte), nil
}

func (byteBufCodec) Unmarshal(data []byte, v interface{}) error {
	v = data
	return nil
}

func (byteBufCodec) String() string {
	return "byteBufCodec"
}

type workerServer struct {
	stop       chan<- bool
	serverPort int
}

func (s *workerServer) RunServer(stream testpb.WorkerService_RunServerServer) error {
	var bs *benchmarkServer
	defer func() {
		// Close benchmark server when stream ends.
		grpclog.Printf("closing benchmark server")
		if bs != nil {
			bs.close()
		}
	}()
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var out *testpb.ServerStatus
		switch argtype := in.Argtype.(type) {
		case *testpb.ServerArgs_Setup:
			grpclog.Printf("server setup received:")
			newbs, err := startBenchmarkServerWithSetup(argtype.Setup, s.serverPort)
			if err != nil {
				return err
			}
			if bs != nil {
				grpclog.Printf("server setup received when server already exists, closing the existing server")
				bs.close()
			}
			bs = newbs
			out = &testpb.ServerStatus{
				Stats: bs.getStats(),
				Port:  int32(bs.port),
				Cores: int32(bs.cores),
			}

		case *testpb.ServerArgs_Mark:
			grpclog.Printf("server mark received:")
			grpclog.Printf(" - %v", argtype)
			if bs == nil {
				return grpc.Errorf(codes.InvalidArgument, "server does not exist when mark received")
			}
			out = &testpb.ServerStatus{
				Stats: bs.getStats(),
				Port:  int32(bs.port),
				Cores: int32(bs.cores),
			}
			if argtype.Mark.Reset_ {
				bs.reset()
			}
		}

		if err := stream.Send(out); err != nil {
			return err
		}
	}

	return nil
}

func (s *workerServer) RunClient(stream testpb.WorkerService_RunClientServer) error {
	var bc *benchmarkClient
	defer func() {
		// Shut down benchmark client when stream ends.
		grpclog.Printf("shuting down benchmark client")
		if bc != nil {
			bc.shutdown()
		}
	}()
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var out *testpb.ClientStatus
		switch t := in.Argtype.(type) {
		case *testpb.ClientArgs_Setup:
			grpclog.Printf("client setup received:")
			newbc, err := startBenchmarkClientWithSetup(t.Setup)
			if err != nil {
				return err
			}
			if bc != nil {
				grpclog.Printf("client setup received when client already exists, shuting down the existing client")
				bc.shutdown()
			}
			bc = newbc
			out = &testpb.ClientStatus{
				Stats: bc.getStats(),
			}

		case *testpb.ClientArgs_Mark:
			grpclog.Printf("client mark received:")
			grpclog.Printf(" - %v", t)
			if bc == nil {
				return grpc.Errorf(codes.InvalidArgument, "client does not exist when mark received")
			}
			out = &testpb.ClientStatus{
				Stats: bc.getStats(),
			}
			if t.Mark.Reset_ {
				bc.reset()
			}
		}

		if err := stream.Send(out); err != nil {
			return err
		}
	}

	return nil
}

func (s *workerServer) CoreCount(ctx context.Context, in *testpb.CoreRequest) (*testpb.CoreResponse, error) {
	grpclog.Printf("core count: %v", runtime.NumCPU())
	return &testpb.CoreResponse{int32(runtime.NumCPU())}, nil
}

func (s *workerServer) QuitWorker(ctx context.Context, in *testpb.Void) (*testpb.Void, error) {
	grpclog.Printf("quiting worker")
	defer func() { s.stop <- true }()
	return &testpb.Void{}, nil
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", ":"+strconv.Itoa(*driverPort))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpclog.Printf("worker listening at port %v", *driverPort)

	s := grpc.NewServer()
	stop := make(chan bool)
	testpb.RegisterWorkerServiceServer(s, &workerServer{
		stop:       stop,
		serverPort: *serverPort,
	})

	go func() {
		<-stop
		s.Stop()
	}()

	s.Serve(lis)
}
