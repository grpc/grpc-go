package test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/stubserver"

	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
)

func TestCompressionCases(t *testing.T) {
	cases := []struct {
		desc                 string
		clientUseCompression bool
		serverUseCompression bool
		expectedStatus       codes.Code
	}{
		{
			desc:                 "Client usecompression true, server usecompression true",
			clientUseCompression: true,
			serverUseCompression: true,
			expectedStatus:       codes.OK,
		},
		{
			desc:                 "Client usecompression true, server usecompression false",
			clientUseCompression: true,
			serverUseCompression: false,
			expectedStatus:       codes.Unimplemented,
		},
		{
			desc:                 "Client usecompression false, server usecompression true",
			clientUseCompression: false,
			serverUseCompression: true,
			expectedStatus:       codes.Internal,
		},
	}
	for i, tc := range cases {
		fmt.Println("TESTCASE: ", i)

		ss := &stubserver.StubServer{
			UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return &testpb.SimpleResponse{
					Payload: in.Payload,
				}, nil
			},
		}
		sopts := []grpc.ServerOption{}
		if tc.serverUseCompression {
			sopts = append(sopts, grpc.RPCCompressor(grpc.NewGZIPCompressor()), grpc.RPCDecompressor(grpc.NewGZIPDecompressor()))
		}
		if err := ss.Start(sopts); err != nil {
			t.Fatalf("Error starting server: %v", err)
		}
		defer ss.Stop()
		payload := &testpb.SimpleRequest{
			Payload: &testpb.Payload{
				Body: []byte("test message"),
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		opts := []grpc.CallOption{}
		if tc.clientUseCompression {
			opts = append(opts, grpc.UseCompressor("gzip"))
		}
		_, err := ss.Client.UnaryCall(ctx, payload, opts...)
		if st, _ := status.FromError(err); st.Code() != tc.expectedStatus {
			t.Fatalf("got %v want %v", st.Code(), tc.expectedStatus)
		}
	}

}
