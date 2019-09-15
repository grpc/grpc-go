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

// Binary server is an example server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	server "github.com/grpc-ecosystem/go-grpc-middleware/testing"
	pb "github.com/grpc-ecosystem/go-grpc-middleware/testing/testproto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

var port = flag.Int("port", 50052, "port number")
var (
	retriableErrors = []codes.Code{codes.Unavailable, codes.DataLoss}
	goodPing        = &pb.PingRequest{Value: "something"}
	noSleep         = 0 * time.Second
	retryTimeout    = 50 * time.Millisecond
)

type failingService struct {
	pb.TestServiceServer
	mu sync.Mutex

	reqCounter uint
	reqModulo  uint
	reqSleep   time.Duration
	reqError   codes.Code
}

func (s *failingService) resetFailingConfiguration(modulo uint, errorCode codes.Code, sleepTime time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reqCounter = 0
	s.reqModulo = modulo
	s.reqError = errorCode
	s.reqSleep = sleepTime
}

func (s *failingService) requestCount() uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reqCounter
}

func (s *failingService) maybeFailRequest() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqCounter++
	if (s.reqModulo > 0) && (s.reqCounter%s.reqModulo == 0) {
		return nil
	}
	time.Sleep(s.reqSleep)
	return grpc.Errorf(s.reqError, "maybeFailRequest: failing it")
}

func (s *failingService) Ping(ctx context.Context, ping *pb.PingRequest) (*pb.PingResponse, error) {
	if err := s.maybeFailRequest(); err != nil {
		return nil, err
	}
	return s.TestServiceServer.Ping(ctx, ping)
}

func (s *failingService) PingList(ping *pb.PingRequest, stream pb.TestService_PingListServer) error {
	if err := s.maybeFailRequest(); err != nil {
		return err
	}
	return s.TestServiceServer.PingList(ping, stream)
}

func (s *failingService) PingStream(stream pb.TestService_PingStreamServer) error {
	if err := s.maybeFailRequest(); err != nil {
		return err
	}
	return s.TestServiceServer.PingStream(stream)
}
func main() {
	flag.Parse()

	address := fmt.Sprintf(":%v", *port)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterTestServiceServer(s, &server.TestPingService{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
