/*
 *
 * Copyright 2026 gRPC authors.
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

package interop

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/stubserver"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
)

func startCompressionTestServer(t *testing.T) *stubserver.StubServer {
	t.Helper()

	ts := NewTestServer()
	stub := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return ts.UnaryCall(ctx, in)
		},
	}
	if err := stub.StartServer(); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	t.Cleanup(stub.Stop)

	if err := stub.StartClient(); err != nil {
		t.Fatalf("Error starting client: %v", err)
	}
	return stub
}

// TestClientCompressedUnaryCall runs the client_compressed_unary interop
// test case end-to-end: it verifies the server rejects a probe request
// whose ExpectCompressed flag doesn't match the actual wire compression,
// and accepts correctly compressed and uncompressed requests.
func (s) TestClientCompressedUnaryCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stub := startCompressionTestServer(t)
	DoClientCompressedUnaryCall(ctx, stub.Client)
}

// TestServerCompressedUnaryCall runs the server_compressed_unary interop
// test case end-to-end: it verifies the server accepts requests asking for
// both a compressed and an uncompressed response.
func (s) TestServerCompressedUnaryCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stub := startCompressionTestServer(t)
	DoServerCompressedUnaryCall(ctx, stub.Client)
}

// TestUnaryCall_ExpectCompressedMismatch verifies that UnaryCall rejects a
// request whose ExpectCompressed flag doesn't match the compression
// actually used on the wire, directly asserting the INVALID_ARGUMENT status
// rather than relying on DoClientCompressedUnaryCall's own checks.
func (s) TestUnaryCall_ExpectCompressedMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stub := startCompressionTestServer(t)

	req := &testpb.SimpleRequest{
		ResponseType:     testpb.PayloadType_COMPRESSABLE,
		ResponseSize:     1,
		ExpectCompressed: &testpb.BoolValue{Value: true},
	}
	_, err := stub.Client.UnaryCall(ctx, req, grpc.UseCompressor("identity"))
	if got, want := status.Code(err), codes.InvalidArgument; got != want {
		t.Fatalf("UnaryCall with mismatched ExpectCompressed got code %v, want %v", got, want)
	}
}
