/*
 *
 * Copyright 2025 gRPC authors.
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

package encoding_test

import (
	"bytes"
	"context"
	"io"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/encoding/gzip"
)

// wrapCompressor is a wrapper of encoding.Compressor which maintains count of
// Compressor method invokes.
type wrapCompressor struct {
	encoding.Compressor
	compressInvokes int32
}

func (wc *wrapCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	atomic.AddInt32(&wc.compressInvokes, 1)
	return wc.Compressor.Compress(w)
}

func setupGzipWrapCompressor(t *testing.T) *wrapCompressor {
	regFn := internal.RegisterCompressorForTesting.(func(encoding.Compressor) func())
	c := &wrapCompressor{Compressor: encoding.GetCompressor("gzip")}
	unreg := regFn(c)
	t.Cleanup(unreg)
	return c
}

func (s) TestSetSendCompressorSuccess(t *testing.T) {
	for _, tt := range []struct {
		name                string
		desc                string
		payload             *testpb.Payload
		dialOpts            []grpc.DialOption
		resCompressor       string
		wantCompressInvokes int32
	}{
		{
			name:                "identity_request_and_gzip_response",
			desc:                "request is uncompressed and response is gzip compressed",
			payload:             &testpb.Payload{Body: []byte("payload")},
			resCompressor:       "gzip",
			wantCompressInvokes: 1,
		},
		{
			name:                "identity_request_and_empty_response",
			desc:                "request is uncompressed and response is gzip compressed",
			payload:             nil,
			resCompressor:       "gzip",
			wantCompressInvokes: 0,
		},
		{
			name:          "gzip_request_and_identity_response",
			desc:          "request is gzip compressed and response is uncompressed with identity",
			payload:       &testpb.Payload{Body: []byte("payload")},
			resCompressor: "identity",
			dialOpts: []grpc.DialOption{
				// Use WithCompressor instead of UseCompressor to avoid counting
				// the client's compressor usage.
				grpc.WithCompressor(grpc.NewGZIPCompressor()),
			},
			wantCompressInvokes: 0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("unary", func(t *testing.T) {
				testUnarySetSendCompressorSuccess(t, tt.payload, tt.resCompressor, tt.wantCompressInvokes, tt.dialOpts)
			})

			t.Run("stream", func(t *testing.T) {
				testStreamSetSendCompressorSuccess(t, tt.payload, tt.resCompressor, tt.wantCompressInvokes, tt.dialOpts)
			})
		})
	}
}

func testUnarySetSendCompressorSuccess(t *testing.T, payload *testpb.Payload, resCompressor string, wantCompressInvokes int32, dialOpts []grpc.DialOption) {
	wc := setupGzipWrapCompressor(t)
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, _ *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			if err := grpc.SetSendCompressor(ctx, resCompressor); err != nil {
				return nil, err
			}
			return &testpb.SimpleResponse{
				Payload: payload,
			}, nil
		},
	}
	if err := ss.Start(nil, dialOpts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected unary call error, got: %v, want: nil", err)
	}

	compressInvokes := atomic.LoadInt32(&wc.compressInvokes)
	if compressInvokes != wantCompressInvokes {
		t.Fatalf("Unexpected compress invokes, got:%d, want: %d", compressInvokes, wantCompressInvokes)
	}
}

func testStreamSetSendCompressorSuccess(t *testing.T, payload *testpb.Payload, resCompressor string, wantCompressInvokes int32, dialOpts []grpc.DialOption) {
	wc := setupGzipWrapCompressor(t)
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}

			if err := grpc.SetSendCompressor(stream.Context(), resCompressor); err != nil {
				return err
			}

			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: payload,
			})
		},
	}
	if err := ss.Start(nil, dialOpts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	s, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("Unexpected full duplex call error, got: %v, want: nil", err)
	}

	if err := s.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("Unexpected full duplex call send error, got: %v, want: nil", err)
	}

	if _, err := s.Recv(); err != nil {
		t.Fatalf("Unexpected full duplex recv error, got: %v, want: nil", err)
	}

	compressInvokes := atomic.LoadInt32(&wc.compressInvokes)
	if compressInvokes != wantCompressInvokes {
		t.Fatalf("Unexpected compress invokes, got:%d, want: %d", compressInvokes, wantCompressInvokes)
	}
}

// fakeCompressor returns a messages of a configured size, irrespective of the
// input.
type fakeCompressor struct {
	decompressedMessageSize int
}

func (f *fakeCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return nopWriteCloser{w}, nil
}

func (f *fakeCompressor) Decompress(io.Reader) (io.Reader, error) {
	return bytes.NewReader(make([]byte, f.decompressedMessageSize)), nil
}

func (f *fakeCompressor) Name() string {
	// Use the name of an existing compressor to avoid interactions with other
	// tests since compressors can't be un-registered.
	return "fake"
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

// TestDecompressionExceedsMaxMessageSize uses a fake compressor that produces
// messages of size 100 bytes on decompression. A server is started with the
// max receive message size restricted to 99 bytes. The test verifies that the
// client receives a ResourceExhausted response from the server.
func (s) TestDecompressionExceedsMaxMessageSize(t *testing.T) {
	const messageLen = 100
	regFn := internal.RegisterCompressorForTesting.(func(encoding.Compressor) func())
	compressor := &fakeCompressor{decompressedMessageSize: messageLen}
	unreg := regFn(compressor)
	defer unreg()
	ss := &stubserver.StubServer{
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{grpc.MaxRecvMsgSize(messageLen - 1)}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	req := &testpb.SimpleRequest{Payload: &testpb.Payload{}}
	_, err := ss.Client.UnaryCall(ctx, req, grpc.UseCompressor(compressor.Name()))
	if got, want := status.Code(err), codes.ResourceExhausted; got != want {
		t.Errorf("Client.UnaryCall(%+v) returned status %v, want %v", req, got, want)
	}
}

// statsHandler is a stats.Handler that counts the number of compressed
// outbound and inbound messages by comparing CompressedLength to Length.
type statsHandler struct {
	stats.Handler
	compress   atomic.Int32
	decompress atomic.Int32
}

func (h *statsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (h *statsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (h *statsHandler) HandleConn(context.Context, stats.ConnStats)                       {}
func (h *statsHandler) HandleRPC(_ context.Context, s stats.RPCStats) {
	switch st := s.(type) {
	case *stats.OutPayload:
		if st.CompressedLength < st.Length {
			h.compress.Add(1)
		}
	case *stats.InPayload:
		if st.CompressedLength < st.Length {
			h.decompress.Add(1)
		}
	}
}

// TestMessageCompression_StreamToggle tests that SetServerStreamMessageCompression
// and SetClientStreamMessageCompression correctly enable and disable per-message
// compression mid-stream on the server and client side respectively.
func (s) TestMessageCompression_StreamToggle(t *testing.T) {
	sh := &statsHandler{}
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: make([]byte, 1000)},
			}); err != nil {
				return err
			}
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := grpc.SetServerStreamMessageCompression(stream.Context(), false); err != nil {
				return err
			}
			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: make([]byte, 1000)},
			}); err != nil {
				return err
			}
			if _, err := stream.Recv(); err != nil {
				return err
			}
			if err := grpc.SetServerStreamMessageCompression(stream.Context(), true); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: &testpb.Payload{Body: make([]byte, 1000)},
			})
		},
	}

	if err := ss.Start(nil, grpc.WithStatsHandler(sh)); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := ss.Client.FullDuplexCall(ctx, grpc.UseCompressor("gzip"))
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}

	// 1. Send first compressed message
	stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: make([]byte, 1000)}})
	stream.Recv()
	if sh.compress.Load() != 1 || sh.decompress.Load() != 1 {
		t.Fatalf("After call 1 (compression enabled): got compress=%d decompress=%d, want compress=1 decompress=1",
			sh.compress.Load(), sh.decompress.Load())
	}

	// 2. Disable message compression and send second message
	grpc.SetClientStreamMessageCompression(stream.Context(), false)
	stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: make([]byte, 1000)}})
	stream.Recv()
	if sh.compress.Load() != 1 || sh.decompress.Load() != 1 {
		t.Fatalf("After call 2 (compression disabled): got compress=%d decompress=%d, want compress=1 decompress=1",
			sh.compress.Load(), sh.decompress.Load())
	}

	// 3. Enable message compression and send third message
	grpc.SetClientStreamMessageCompression(stream.Context(), true)
	stream.Send(&testpb.StreamingOutputCallRequest{Payload: &testpb.Payload{Body: make([]byte, 1000)}})
	stream.Recv()
	if sh.compress.Load() != 2 || sh.decompress.Load() != 2 {
		t.Fatalf("After call 3 (compression re-enabled): got compress=%d decompress=%d, want compress=2 decompress=2",
			sh.compress.Load(), sh.decompress.Load())
	}
}

// TestMessageCompression_AmbiguousContext verifies that
// SetServerStreamMessageCompression and SetClientStreamMessageCompression work
// independently when called on a context that contains keys for both a server
// stream and a client stream. This situation arises when a server handler
// propagates its context to an outbound gRPC call for deadline propagation:
// the outbound ClientStream.Context() inherits the server-stream key from the
// parent and also adds its own client-stream compression key.
func (s) TestMessageCompression_AmbiguousContext(t *testing.T) {
	backendSH := &statsHandler{}
	backend := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if _, err := stream.Recv(); err == io.EOF {
					return nil
				} else if err != nil {
					return err
				}
				if err := stream.Send(&testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{Body: make([]byte, 1000)},
				}); err != nil {
					return err
				}
			}
		},
	}
	if err := backend.StartServer(grpc.StatsHandler(backendSH)); err != nil {
		t.Fatalf("Error starting backend: %v", err)
	}
	defer backend.Stop()

	// errCh carries any errors from the two SetXxx calls inside the proxy handler.
	errCh := make(chan error, 2)
	proxy := &stubserver.StubServer{
		FullDuplexCallF: func(serverStream testgrpc.TestService_FullDuplexCallServer) error {
			// Use the server handler's context as the parent for the outbound
			// call. This is the standard deadline-propagation pattern and is
			// what makes the resulting context "ambiguous": it inherits
			// streamKey{} from the server infrastructure, and the gRPC client
			// stack appends compressKey{} when creating the outbound stream.
			backendConn, err := grpc.NewClient(
				backend.Address,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				return err
			}
			defer backendConn.Close()

			clientStream, err := testgrpc.NewTestServiceClient(backendConn).FullDuplexCall(
				serverStream.Context(), grpc.UseCompressor("gzip"),
			)
			if err != nil {
				return err
			}

			// clientStream.Context() now has BOTH:
			//   - streamKey{}    → the proxy's own server-side transport stream
			//   - compressKey{}  → the outbound client stream's compression flag
			ambiguousCtx := clientStream.Context()

			// Must only affect the server stream (proxy → original caller).
			errCh <- grpc.SetServerStreamMessageCompression(ambiguousCtx, false)
			// Must only affect the client stream (proxy → backend).
			errCh <- grpc.SetClientStreamMessageCompression(ambiguousCtx, false)

			// Forward one request/response pair through the proxy.
			req, err := serverStream.Recv()
			if err != nil {
				return err
			}
			if err := clientStream.Send(req); err != nil {
				return err
			}
			if err := clientStream.CloseSend(); err != nil {
				return err
			}
			resp, err := clientStream.Recv()
			if err != nil {
				return err
			}
			return serverStream.Send(resp)
		},
	}
	proxySH := &statsHandler{}
	if err := proxy.Start([]grpc.ServerOption{grpc.StatsHandler(proxySH)}); err != nil {
		t.Fatalf("Error starting proxy: %v", err)
	}
	defer proxy.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := proxy.Client.FullDuplexCall(ctx, grpc.UseCompressor("gzip"))
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if err := stream.Send(&testpb.StreamingOutputCallRequest{
		Payload: &testpb.Payload{Body: make([]byte, 1000)},
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("Recv failed: %v", err)
	}

	// Collect results from both SetXxx calls.
	for _, name := range []string{"SetServerStreamMessageCompression", "SetClientStreamMessageCompression"} {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("%s on ambiguous context returned unexpected error: %v", name, err)
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s result", name)
		}
	}

	// SetServerStreamMessageCompression disabled compression on the
	// proxy → caller direction: the proxy's server stream must not have
	// sent any compressed messages.
	if got := proxySH.compress.Load(); got != 0 {
		t.Errorf("proxy server outbound compress count = %d, want 0 (SetServerStreamMessageCompression disabled it)", got)
	}
	// SetClientStreamMessageCompression disabled compression on the
	// proxy → backend direction: the backend must not have received any
	// compressed messages.
	if got := backendSH.decompress.Load(); got != 0 {
		t.Errorf("backend inbound decompress count = %d, want 0 (SetClientStreamMessageCompression disabled it)", got)
	}
}

func (s) TestMessageCompression_Unary(t *testing.T) {
	sh := &statsHandler{}
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			grpc.SetSendCompressor(ctx, "gzip")
			if in.ResponseSize == 0 {
				grpc.SetServerStreamMessageCompression(ctx, false)
			}
			return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: make([]byte, 10000)}}, nil
		},
	}

	if err := ss.Start(nil, grpc.WithStatsHandler(sh)); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Call 1: Compression ON
	ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{ResponseSize: 1, Payload: &testpb.Payload{Body: make([]byte, 1000)}}, grpc.UseCompressor("gzip"))
	if sh.compress.Load() != 1 || sh.decompress.Load() != 1 {
		t.Fatalf("After call 1 (compression enabled): got compress=%d decompress=%d, want compress=1 decompress=1",
			sh.compress.Load(), sh.decompress.Load())
	}

	// Call 2: Compression OFF (for response, but request is still compressed by UseCompressor)
	ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{ResponseSize: 0, Payload: &testpb.Payload{Body: make([]byte, 1000)}}, grpc.UseCompressor("gzip"))
	if sh.compress.Load() != 2 || sh.decompress.Load() != 1 {
		t.Fatalf("After call 2 (server response compression disabled): got compress=%d decompress=%d, want compress=2 decompress=1",
			sh.compress.Load(), sh.decompress.Load())
	}
}
