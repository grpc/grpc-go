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
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/encoding/internal"
	"google.golang.org/grpc/internal/stubserver"
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
