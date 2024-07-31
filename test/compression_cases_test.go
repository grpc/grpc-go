package test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
)

type mockCompressor struct {
	grpc.Compressor
}

func newMockCompressor() grpc.Compressor {
	return &mockCompressor{grpc.NewGZIPCompressor()}
}

func (c *mockCompressor) Type() string {
	return "mock"
}

type mockDecompressor struct {
	grpc.Decompressor
}

func newMockDecompressor() grpc.Decompressor {
	return &mockDecompressor{grpc.NewGZIPDecompressor()}
}

func (d *mockDecompressor) Type() string {
	return "mock"
}

func TestCompressionCases(t *testing.T) {
	cases := []struct {
		desc           string
		clientUseMock  bool
		serverUseMock  bool
		expectedStatus codes.Code
	}{
		{
			desc:           "Client and Server use mock compression",
			clientUseMock:  true,
			serverUseMock:  true,
			expectedStatus: codes.OK,
		},
		{
			desc:           "Only Client use mock compression",
			clientUseMock:  true,
			serverUseMock:  false,
			expectedStatus: codes.Unimplemented,
		},
		{
			desc:           "Only Server use mock compression",
			clientUseMock:  false,
			serverUseMock:  true,
			expectedStatus: codes.Internal,
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
		if tc.serverUseMock {
			sopts = append(sopts, grpc.RPCCompressor(newMockCompressor()), grpc.RPCDecompressor(newMockDecompressor()))
		}
		if err := ss.Start(sopts); err != nil {
			t.Fatalf("Error starting server: %v", err)
		}

		defer ss.Stop()
		dOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		if tc.clientUseMock {
			dOpts = append(dOpts, grpc.WithCompressor(newMockCompressor()), grpc.WithDecompressor(newMockDecompressor()))
		}
		cc, err := grpc.Dial(ss.Address, dOpts...)
		if err != nil {
			t.Fatalf("Failed to dial server: %v", err)
		}
		defer cc.Close()
		ss.Client = testpb.NewTestServiceClient(cc)

		payload := &testpb.SimpleRequest{
			Payload: &testpb.Payload{
				Body: []byte("test message"),
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		_, err = ss.Client.UnaryCall(ctx, payload)
		if st, _ := status.FromError(err); st.Code() != tc.expectedStatus {
			t.Fatalf("got %v want %v", st.Code(), tc.expectedStatus)
		}
	}
}
