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
	"io/ioutil"
	"net"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/logs"
	"google.golang.org/grpc/metadata"
)

var (
	useTLS                = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	caFile                = flag.String("tls_ca_file", "testdata/ca.pem", "The file containning the CA root cert file")
	serviceAccountKeyFile = flag.String("service_account_key_file", "", "Path to service account json key file")
	oauthScope            = flag.String("oauth_scope", "", "The scope for OAuth2 tokens")
	defaultServiceAccount = flag.String("default_service_account", "", "Email of GCE default service account")
	serverHost            = flag.String("server_host", "127.0.0.1", "The server host name")
	serverPort            = flag.Int("server_port", 10000, "The server port number")
	tlsServerName         = flag.String("server_host_override", "x.test.youtube.com", "The server name use to verify the hostname returned by TLS handshake if it is not empty. Otherwise, --server_host is used.")
	testCase              = flag.String("test_case", "large_unary",
		`Configure different test cases. Valid options are:
        empty_unary : empty (zero bytes) request and response;
        large_unary : single request and (large) response;
        client_streaming : request streaming with single response;
        server_streaming : single request with response streaming;
        ping_pong : full-duplex streaming;
        compute_engine_creds: large_unary with compute engine auth;
	service_account_creds: large_unary with service account auth;
	cancel_after_begin: cancellation after metadata has been sent but before payloads are sent;
	cancel_after_first_response: cancellation after receiving 1st message from the server.`)
	logger = logs.DefaultLogger
)

var (
	reqSizes      = []int{27182, 8, 1828, 45904}
	respSizes     = []int{31415, 9, 2653, 58979}
	largeReqSize  = 271828
	largeRespSize = 314159
)

func newPayload(t testpb.PayloadType, size int) *testpb.Payload {
	if size < 0 {
		logger.Fatalf("Requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	case testpb.PayloadType_UNCOMPRESSABLE:
		logger.Fatalf("PayloadType UNCOMPRESSABLE is not supported")
	default:
		logger.Fatalf("Unsupported payload type: %d", t)
	}
	return &testpb.Payload{
		Type: t.Enum(),
		Body: body,
	}
}

func doEmptyUnaryCall(tc testpb.TestServiceClient) {
	reply, err := tc.EmptyCall(context.Background(), &testpb.Empty{})
	if err != nil {
		logger.Fatal("/TestService/EmptyCall RPC failed: ", err)
	}
	if !proto.Equal(&testpb.Empty{}, reply) {
		logger.Fatalf("/TestService/EmptyCall receives %v, want %v", reply, testpb.Empty{})
	}
	logger.Println("EmptyUnaryCall done")
}

func doLargeUnaryCall(tc testpb.TestServiceClient) {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, largeReqSize)
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseSize: proto.Int32(int32(largeRespSize)),
		Payload:      pl,
	}
	reply, err := tc.UnaryCall(context.Background(), req)
	if err != nil {
		logger.Fatal("/TestService/UnaryCall RPC failed: ", err)
	}
	t := reply.GetPayload().GetType()
	s := len(reply.GetPayload().GetBody())
	if t != testpb.PayloadType_COMPRESSABLE || s != largeRespSize {
		logger.Fatalf("Got the reply with type %d len %d; want %d, %d", t, s, testpb.PayloadType_COMPRESSABLE, largeRespSize)
	}
	logger.Println("LargeUnaryCall done")
}

func doClientStreaming(tc testpb.TestServiceClient) {
	stream, err := tc.StreamingInputCall(context.Background())
	if err != nil {
		logger.Fatalf("%v.StreamingInputCall(_) = _, %v", tc, err)
	}
	var sum int
	for _, s := range reqSizes {
		pl := newPayload(testpb.PayloadType_COMPRESSABLE, s)
		req := &testpb.StreamingInputCallRequest{
			Payload: pl,
		}
		if err := stream.Send(req); err != nil {
			logger.Fatalf("%v.Send(%v) = %v", stream, req, err)
		}
		sum += s
		logger.Printf("Sent a request of size %d, aggregated size %d", s, sum)

	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		logger.Fatalf("%v.CloseAndRecv() got error %v, want %v", stream, err, nil)
	}
	if reply.GetAggregatedPayloadSize() != int32(sum) {
		logger.Fatalf("%v.CloseAndRecv().GetAggregatePayloadSize() = %v; want %v", stream, reply.GetAggregatedPayloadSize(), sum)
	}
	logger.Println("ClientStreaming done")
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
		logger.Fatalf("%v.StreamingOutputCall(_) = _, %v", tc, err)
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
			logger.Fatalf("Got the reply of type %d, want %d", t, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			logger.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
		respCnt++
	}
	if rpcStatus != io.EOF {
		logger.Fatalf("Failed to finish the server streaming rpc: %v", err)
	}
	if respCnt != len(respSizes) {
		logger.Fatalf("Got %d reply, want %d", len(respSizes), respCnt)
	}
	logger.Println("ServerStreaming done")
}

func doPingPong(tc testpb.TestServiceClient) {
	stream, err := tc.FullDuplexCall(context.Background())
	if err != nil {
		logger.Fatalf("%v.FullDuplexCall(_) = _, %v", tc, err)
	}
	var index int
	for index < len(reqSizes) {
		respParam := []*testpb.ResponseParameters{
			{
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
			logger.Fatalf("%v.Send(%v) = %v", stream, req, err)
		}
		reply, err := stream.Recv()
		if err != nil {
			logger.Fatalf("%v.Recv() = %v", stream, err)
		}
		t := reply.GetPayload().GetType()
		if t != testpb.PayloadType_COMPRESSABLE {
			logger.Fatalf("Got the reply of type %d, want %d", t, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			logger.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
	}
	if err := stream.CloseSend(); err != nil {
		logger.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		logger.Fatalf("%v failed to complele the ping pong test: %v", stream, err)
	}
	logger.Println("Pingpong done")
}

func doComputeEngineCreds(tc testpb.TestServiceClient) {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, largeReqSize)
	req := &testpb.SimpleRequest{
		ResponseType:   testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseSize:   proto.Int32(int32(largeRespSize)),
		Payload:        pl,
		FillUsername:   proto.Bool(true),
		FillOauthScope: proto.Bool(true),
	}
	reply, err := tc.UnaryCall(context.Background(), req)
	if err != nil {
		logger.Fatal("/TestService/UnaryCall RPC failed: ", err)
	}
	user := reply.GetUsername()
	scope := reply.GetOauthScope()
	if user != *defaultServiceAccount {
		logger.Fatalf("Got user name %q, want %q.", user, *defaultServiceAccount)
	}
	if !strings.Contains(*oauthScope, scope) {
		logger.Fatalf("Got OAuth scope %q which is NOT a substring of %q.", scope, *oauthScope)
	}
	logger.Println("ComputeEngineCreds done")
}

func getServiceAccountJSONKey() []byte {
	jsonKey, err := ioutil.ReadFile(*serviceAccountKeyFile)
	if err != nil {
		logger.Fatalf("Failed to read the service account key file: %v", err)
	}
	return jsonKey
}

func doServiceAccountCreds(tc testpb.TestServiceClient) {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, largeReqSize)
	req := &testpb.SimpleRequest{
		ResponseType:   testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseSize:   proto.Int32(int32(largeRespSize)),
		Payload:        pl,
		FillUsername:   proto.Bool(true),
		FillOauthScope: proto.Bool(true),
	}
	reply, err := tc.UnaryCall(context.Background(), req)
	if err != nil {
		logger.Fatal("/TestService/UnaryCall RPC failed: ", err)
	}
	jsonKey := getServiceAccountJSONKey()
	user := reply.GetUsername()
	scope := reply.GetOauthScope()
	if !strings.Contains(string(jsonKey), user) {
		logger.Fatalf("Got user name %q which is NOT a substring of %q.", user, jsonKey)
	}
	if !strings.Contains(*oauthScope, scope) {
		logger.Fatalf("Got OAuth scope %q which is NOT a substring of %q.", scope, *oauthScope)
	}
	logger.Println("ServiceAccountCreds done")
}

var (
	testMetadata = metadata.MD{
		"key1": "value1",
		"key2": "value2",
	}
)

func doCancelAfterBegin(tc testpb.TestServiceClient) {
	ctx, cancel := context.WithCancel(metadata.NewContext(context.Background(), testMetadata))
	stream, err := tc.StreamingInputCall(ctx)
	if err != nil {
		logger.Fatalf("%v.StreamingInputCall(_) = _, %v", tc, err)
	}
	cancel()
	_, err = stream.CloseAndRecv()
	if grpc.Code(err) != codes.Canceled {
		logger.Fatalf("%v.CloseAndRecv() got error code %d, want %d", stream, grpc.Code(err), codes.Canceled)
	}
	logger.Println("CancelAfterBegin done")
}

func doCancelAfterFirstResponse(tc testpb.TestServiceClient) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		logger.Fatalf("%v.FullDuplexCall(_) = _, %v", tc, err)
	}
	respParam := []*testpb.ResponseParameters{
		{
			Size: proto.Int32(31415),
		},
	}
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, 27182)
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseParameters: respParam,
		Payload:            pl,
	}
	if err := stream.Send(req); err != nil {
		logger.Fatalf("%v.Send(%v) = %v", stream, req, err)
	}
	if _, err := stream.Recv(); err != nil {
		logger.Fatalf("%v.Recv() = %v", stream, err)
	}
	cancel()
	if _, err := stream.Recv(); grpc.Code(err) != codes.Canceled {
		logger.Fatalf("%v compleled with error code %d, want %d", stream, grpc.Code(err), codes.Canceled)
	}
	logger.Println("CancelAfterFirstResponse done")
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
				logger.Fatalf("Failed to create TLS credentials %v", err)
			}
		} else {
			creds = credentials.NewClientTLSFromCert(nil, sn)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
		if *testCase == "compute_engine_creds" {
			opts = append(opts, grpc.WithPerRPCCredentials(credentials.NewComputeEngine()))
		} else if *testCase == "service_account_creds" {
			jwtCreds, err := credentials.NewServiceAccountFromFile(*serviceAccountKeyFile, *oauthScope)
			if err != nil {
				logger.Fatalf("Failed to create JWT credentials: %v", err)
			}
			opts = append(opts, grpc.WithPerRPCCredentials(jwtCreds))
		}
	}
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		logger.Fatalf("Fail to dial: %v", err)
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
	case "compute_engine_creds":
		if !*useTLS {
			logger.Fatalf("TLS is not enabled. TLS is required to execute compute_engine_creds test case.")
		}
		doComputeEngineCreds(tc)
	case "service_account_creds":
		if !*useTLS {
			logger.Fatalf("TLS is not enabled. TLS is required to execute service_account_creds test case.")
		}
		doServiceAccountCreds(tc)
	case "cancel_after_begin":
		doCancelAfterBegin(tc)
	case "cancel_after_first_response":
		doCancelAfterFirstResponse(tc)
	default:
		logger.Fatal("Unsupported test case: ", *testCase)
	}
}
