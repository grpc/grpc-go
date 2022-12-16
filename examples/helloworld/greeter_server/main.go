/*
 *
 * Copyright 2015 gRPC authors.
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

// Package main implements a server for Greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"io/ioutil"
	"os"
	"path"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/authz"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	//log.Printf("Received: %v", in.GetName())
	return &pb.HelloReply{Message: "Hello " + in.GetName()}, nil
}

func createTmpPolicyFile(dirSuffix string, policy []byte) string {
	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := ioutil.TempDir("", dirSuffix)
	if err != nil {
		log.Printf("ioutil.TempDir() failed: %v", err)
	}
	log.Printf("Using tmpdir: %s", dir)
	// Write policy into file.
	filename := path.Join(dir, "policy.json")
	if err := ioutil.WriteFile(filename, policy, os.ModePerm); err != nil {
		log.Printf("ioutil.WriteFile(%q) failed: %v", filename, err)
	}
	log.Printf("Wrote policy %s to file at %s", string(policy), filename)
	//policyContents, err := ioutil.ReadFile(filename)
	//log.Printf("policy(%s: %s) read failed: %v", filename, policyContents, err)
	return filename
}

func main() {
	flag.Parse()

	authzPolicy := `{
				"name": "authz",
				"allow_rules":
				[
					{
						"name": "allow_all"
					}
				]
			}`
	file := createTmpPolicyFile("testing", []byte(authzPolicy))
	i, _ := authz.NewFileWatcher(file, 100*time.Millisecond)
	defer i.Close()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(grpc.ChainUnaryInterceptor(i.UnaryInterceptor))
	defer s.Stop()

	pb.RegisterGreeterServer(s, &server{})
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
