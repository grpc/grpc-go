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
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/stubserver"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

func startCompressionTestServer(t *testing.T, dopts ...grpc.DialOption) *stubserver.StubServer {
	t.Helper()

	ts := NewTestServer()
	stub := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return ts.UnaryCall(ctx, in)
		},
	}
	if err := stub.Start(nil, dopts...); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	t.Cleanup(stub.Stop)
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

// TestServerCompressedUnaryCall_WireCompression verifies, via a
// stats.Handler observing InPayload on the client, that
// response_compressed actually controls whether the response is
// compressed on the wire: response_compressed=true must shrink
// CompressedLength below Length, and response_compressed=false must leave
// them equal.
func (s) TestServerCompressedUnaryCall_WireCompression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	rec := &inPayloadRecorder{}
	stub := startCompressionTestServer(t, grpc.WithStatsHandler(rec))

	pl := ClientNewPayload(testpb.PayloadType_COMPRESSABLE, largeReqSize)
	for _, compressed := range []bool{true, false} {
		rec.reset()
		req := &testpb.SimpleRequest{
			ResponseType:       testpb.PayloadType_COMPRESSABLE,
			ResponseSize:       int32(largeRespSize),
			ResponseCompressed: &testpb.BoolValue{Value: compressed},
			Payload:            pl,
		}
		reply, err := stub.Client.UnaryCall(ctx, req)
		if err != nil {
			t.Fatalf("UnaryCall(response_compressed=%v) failed: %v", compressed, err)
		}
		checkLargePayload(reply.GetPayload())

		p := rec.get()
		if p == nil {
			t.Fatalf("response_compressed=%v: no InPayload observed", compressed)
		}
		if compressed && p.CompressedLength >= p.Length {
			t.Fatalf("response_compressed=true: got CompressedLength=%d, Length=%d; want CompressedLength < Length", p.CompressedLength, p.Length)
		}
		if !compressed && p.CompressedLength != p.Length {
			t.Fatalf("response_compressed=false: got CompressedLength=%d, Length=%d; want equal", p.CompressedLength, p.Length)
		}
	}
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

// inPayloadRecorder is a stats.Handler that records the most recently
// observed stats.InPayload, used to assert on wire-level compression from
// outside the RPC (see TestServerCompressedUnaryCall_WireCompression).
type inPayloadRecorder struct {
	mu   sync.Mutex
	last *stats.InPayload
}

func (r *inPayloadRecorder) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (r *inPayloadRecorder) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (r *inPayloadRecorder) HandleConn(context.Context, stats.ConnStats) {}

func (r *inPayloadRecorder) HandleRPC(_ context.Context, rs stats.RPCStats) {
	if p, ok := rs.(*stats.InPayload); ok {
		r.mu.Lock()
		r.last = p
		r.mu.Unlock()
	}
}

func (r *inPayloadRecorder) reset() {
	r.mu.Lock()
	r.last = nil
	r.mu.Unlock()
}

func (r *inPayloadRecorder) get() *stats.InPayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}
