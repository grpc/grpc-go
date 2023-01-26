/*
 *
 * Copyright 2022 gRPC authors.
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
	"context"
	"flag"
	"io"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/gcp/observability"
	"google.golang.org/grpc/metadata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	serverHost     = flag.String("server_host", "localhost", "The server host name")
	serverPort     = flag.Int("server_port", 10000, "The server port number")
	exportInterval = flag.Int("export_interval", 0, "Number of seconds to wait to export observability data")
	action         = flag.String("action", "doUnaryCall", "The action to perform")

	reqSize             = 271828
	respSize            = 314159
	rpcMetadataKey      = "o11y-header"
	rpcMetadataBinKey   = "o11y-header-bin"
	rpcMetadataValue    = "o11y-header-value"
	rpcMetadataBinValue = "\x0a\x0b\x0a\x0b\x0a\x0b"
	customMetadata      = metadata.Pairs(
		rpcMetadataKey, rpcMetadataValue,
		rpcMetadataBinKey, rpcMetadataBinValue,
	)
)

func makePayload(size int32) *testpb.Payload {
	body := make([]byte, reqSize)
	pl := &testpb.Payload{
		Body: body,
	}
	return pl
}

func DoFullDuplexCall(tc testgrpc.TestServiceClient, args ...grpc.CallOption) {
	stream, err := tc.FullDuplexCall(context.Background(), args...)
	if err != nil {
		log.Fatalf("%v.FullDuplexCall(_) = _, %v", tc, err)
	}
	for index := 0; index < 5; index++ {
		respParam := []*testpb.ResponseParameters{
			{
				Size: int32(respSize),
			},
		}
		pl := makePayload(int32(reqSize))
		req := &testpb.StreamingOutputCallRequest{
			ResponseParameters: respParam,
			Payload:            pl,
		}
		if err := stream.Send(req); err != nil {
			log.Fatalf("%v has error %v while sending %v", stream, err, req)
		}
		reply, err := stream.Recv()
		if err != nil {
			log.Fatalf("%v.Recv() = %v", stream, err)
		}
		size := len(reply.GetPayload().GetBody())
		log.Printf("FullDuplexCall client receives response size %d", size)
		if size != respSize {
			log.Fatalf("Got reply body of length %d, want %d", size, respSize)
		}
	}
	if err := stream.CloseSend(); err != nil {
		log.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		log.Fatalf("%v failed to complele the FullDuplexCall test: %v", stream, err)
	}
}

func DoUnaryCall(tc testgrpc.TestServiceClient, args ...grpc.CallOption) {
	pl := makePayload(int32(reqSize))
	req := &testpb.SimpleRequest{
		ResponseSize: int32(respSize),
		Payload:      pl,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), customMetadata)
	var header, trailer metadata.MD
	args = append(args, grpc.Header(&header), grpc.Trailer(&trailer))
	reply, err := tc.UnaryCall(ctx, req, args...)
	if err != nil {
		log.Fatalf("/TestService/UnaryCall RPC failed: ", err)
	}
	s := len(reply.GetPayload().GetBody())
	log.Printf("UnaryCall client receives response size %d", s)
	if s != respSize {
		log.Fatalf("Got the reply with len %d; want %d", s, respSize)
	}
}

func main() {
	err := observability.Start(context.Background())
	if err != nil {
		log.Fatalf("observability start failed: %v", err)
	}
	defer observability.End()
	flag.Parse()
	serverAddr := *serverHost
	if *serverPort != 0 {
		serverAddr = net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	}
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		log.Fatalf("Fail to dial: %v", err)
	}
	defer conn.Close()
	tc := testgrpc.NewTestServiceClient(conn)
	actions := strings.Split(*action, ",")
	for _, action := range actions {
		if action == "doFullDuplexCall" {
			DoFullDuplexCall(tc)
		} else { // "doUnaryCall"
			DoUnaryCall(tc)
		}
	}
	log.Printf("Sleeping %d seconds before closing client", *exportInterval)
	ss := *exportInterval
	for ss > 0 {
		next := int(math.Min(float64(ss), 15))
		time.Sleep(time.Duration(next) * time.Second)
		ss -= next
		log.Printf("%d more seconds to sleep", ss)
	}
}
