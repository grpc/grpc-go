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
	"log"
	"net"
	"strconv"

	testpb "github.com/google/arcwire-go/interop/grpc_testing"
	"github.com/google/arcwire-go/rpc"
	"code.google.com/p/goprotobuf/proto"
	"golang.org/x/net/context"
)

var (
	useTLS     = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	caFile     = flag.String("tls_ca_file", "", "The file containning the CA root cert file")
	serverHost = flag.String("server_host", "127.0.0.1", "The server host name")
	serverPort = flag.Int("server_port", 10000, "The server port number")
	testCase   = flag.String("test_case", "large_unary_call", "The RPC method to be tested")
)

func doLargeUnaryCall(tc testpb.TestServiceClient) {
	argSize := 271828
	respSize := 314159
	pl := &testpb.Payload{
		Type: testpb.PayloadType_COMPRESSABLE.Enum(),
		Body: make([]byte, argSize),
	}
	args := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE.Enum(),
		ResponseSize: proto.Int32(int32(respSize)),
		Payload:      pl,
	}
	reply, err := tc.UnaryCall(context.Background(), args)
	if err != nil {
		log.Fatal("/TestService/UnaryCall RPC failed: ", err)
	}
	t := reply.GetPayload().GetType()
	s := len(reply.GetPayload().GetBody())
	if t != testpb.PayloadType_COMPRESSABLE || s != respSize {
		log.Fatalf("Got the reply with type %d len %d; want %d, %d", t, s, testpb.PayloadType_COMPRESSABLE, respSize)
	}
}

func main() {
	flag.Parse()
	serverAddr := net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	conn, err := rpc.Dial(serverAddr, func(cc *rpc.ClientConn) {
		cc.UseTLS = *useTLS
		cc.CAFile = *caFile
	})
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()
	tc := testpb.NewTestServiceClient(conn)
	switch *testCase {
	case "large_unary_call":
		doLargeUnaryCall(tc)
	default:
		log.Println("Unsupported test case: ", *testCase)
	}
}
