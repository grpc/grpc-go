/*
 *
 * Copyright 2014, Google Inc.
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
	"log"
	"net"
	"strconv"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	useTLS        = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	caFile        = flag.String("tls_ca_file", "testdata/ca.pem", "The file containning the CA root cert file")
	serverHost    = flag.String("server_host", "127.0.0.1", "The server host name")
	serverPort    = flag.Int("server_port", 10000, "The server port number")
	tlsServerName = flag.String("tls_server_name", "x.test.youtube.com", "The server name use to verify the hostname returned by TLS handshake")
	testCase      = flag.String("test_case", "large_unary", "The RPC method to be tested: large_unary|empty_unary|client_streaming|server_streaming|ping_pong|all")
)

var (
	reqSizes  = []int{27182, 8, 1828, 45904}
	respSizes = []int{31415, 9, 2653, 58979}
)

func newPayload(t testpb.PayloadType, size int) *testpb.Payload {
	if size < 0 {
		log.Fatalf("Requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	case testpb.PayloadType_UNCOMPRESSABLE:
		log.Fatalf("PayloadType UNCOMPRESSABLE is not supported")
	default:
		log.Fatalf("Unsupported payload type: %d", t)
	}
	return &testpb.Payload{
		Type: t.Enum(),
		Body: body,
	}
}

func doEmptyUnaryCall(tc testpb.TestServiceClient) {
	reply, err := tc.EmptyCall(context.Background(), &testpb.Empty{})
	if err != nil {
		log.Fatal("/TestService/EmptyCall RPC failed: ", err)
	}
	if !proto.Equal(&testpb.Empty{}, reply) {
		log.Fatalf("/TestService/EmptyCall receives %v, want %v", reply, testpb.Empty{})
	}
}

func doLargeUnaryCall(tc testpb.TestServiceClient) {
	argSize := 271828
	respSize := 314159
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseSize: proto.Int32(int32(respSize)),
		Payload:      pl,
	}
	reply, err := tc.UnaryCall(context.Background(), req)
	if err != nil {
		log.Fatal("/TestService/UnaryCall RPC failed: ", err)
	}
	t := reply.GetPayload().GetType()
	s := len(reply.GetPayload().GetBody())
	if t != testpb.PayloadType_COMPRESSABLE || s != respSize {
		log.Fatalf("Got the reply with type %d len %d; want %d, %d", t, s, testpb.PayloadType_COMPRESSABLE, respSize)
	}
}

func doClientStreaming(tc testpb.TestServiceClient) {
	stream, err := tc.StreamingInputCall(context.Background())
	if err != nil {
		log.Fatalf("%v.StreamingInputCall(_) = _, %v", tc, err)
	}
	var sum int
	for _, s := range reqSizes {
		pl := newPayload(testpb.PayloadType_COMPRESSABLE, s)
		req := &testpb.StreamingInputCallRequest{
			Payload: pl,
		}
		if err := stream.Send(req); err != nil {
			log.Fatalf("%v.Send(%v) = %v", stream, req, err)
		}
		sum += s
		log.Printf("Sent a request of size %d, aggregated size %d", s, sum)

	}
	reply, err := stream.CloseAndRecv()
	if err != io.EOF {
		log.Fatalf("%v.CloseAndRecv() got error %v, want %v", stream, err, io.EOF)
	}
	if reply.GetAggregatedPayloadSize() != int32(sum) {
		log.Fatalf("%v.CloseAndRecv().GetAggregatePayloadSize() = %v; want %v", stream, reply.GetAggregatedPayloadSize(), sum)
	}
}

func doServerStreaming(tc testpb.TestServiceClient) {
	respParam := make([]*testpb.ResponseParameters, len(respSizes))
	for i, s := range respSizes {
		respParam[i] = &testpb.ResponseParameters{
			Size: proto.Int32(int32(s)),
		}
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseParameters: respParam,
	}
	stream, err := tc.StreamingOutputCall(context.Background(), req)
	if err != nil {
		log.Fatalf("%v.StreamingOutputCall(_) = _, %v", tc, err)
	}
	var rpcStatus error
	var respCnt int
	var index int
	for {
		reply, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		t := reply.GetPayload().GetType()
		if t != testpb.PayloadType_COMPRESSABLE {
			log.Fatalf("Got the reply of type %d, want %d", t, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			log.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
		respCnt++
	}
	if rpcStatus != io.EOF {
		log.Fatalf("Failed to finish the server streaming rpc: %v", err)
	}
	if respCnt != len(respSizes) {
		log.Fatalf("Got %d reply, want %d", len(respSizes), respCnt)
	}
}

func doPingPong(tc testpb.TestServiceClient) {
	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		log.Fatalf("%v.FullDuplexCall(_) = _, %v", tc, err)
	}
	var index int
	for index < len(reqSizes) {
		respParam := []*testpb.ResponseParameters{
			&testpb.ResponseParameters{
				Size: proto.Int32(int32(respSizes[index])),
			},
		}
		pl := newPayload(testpb.PayloadType_COMPRESSABLE, reqSizes[index])
		req := &testpb.StreamingOutputCallRequest{
			ResponseType:       testpb.PayloadType_COMPRESSABLE.Enum(),
			ResponseParameters: respParam,
			Payload:            pl,
		}
		if err := stream.Send(req); err != nil {
			log.Fatalf("%v.Send(%v) = %v", stream, req, err)
		}
		reply, err := stream.Recv()
		if err != nil {
			log.Fatalf("%v.Recv() = %v", stream, err)
		}
		t := reply.GetPayload().GetType()
		if t != testpb.PayloadType_COMPRESSABLE {
			log.Fatalf("Got the reply of type %d, want %d", t, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			log.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
	}
	if err := stream.CloseSend(); err != nil {
		log.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		log.Fatalf("%v failed to complele the ping pong test: %v", stream, err)
	}
}

func main() {
	flag.Parse()
	serverAddr := net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	var opts []grpc.DialOption
	if *useTLS {
		var sn string
		if *tlsServerName != "" {
			sn = *tlsServerName
		}
		var creds credentials.TransportAuthenticator
		if *caFile != "" {
			var err error
			creds, err = credentials.NewClientTLSFromFile(*caFile, sn)
			if err != nil {
				log.Fatalf("Failed to create credentials %v", err)
			}
		} else {
			creds = credentials.NewClientTLSFromCert(nil, sn)
		}
		opts = append(opts, grpc.WithClientTLS(creds))
	}
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	tc := testpb.NewTestServiceClient(conn)
	switch *testCase {
	case "empty_unary":
		doEmptyUnaryCall(tc)
	case "large_unary":
		doLargeUnaryCall(tc)
	case "client_streaming":
		doClientStreaming(tc)
	case "server_streaming":
		doServerStreaming(tc)
	case "ping_pong":
		doPingPong(tc)
	case "all":
		doEmptyUnaryCall(tc)
		doLargeUnaryCall(tc)
		doClientStreaming(tc)
		doServerStreaming(tc)
		doPingPong(tc)
	default:
		log.Fatal("Unsupported test case: ", *testCase)
	}
}
