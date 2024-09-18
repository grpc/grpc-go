package stream

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

type s struct {
	UnimplementedStreamTestServer
}

func (s *s) StreamBuffer(_ *BufferRequest, stream StreamTest_StreamBufferServer) error {
	for i := 0; i < 100000000; i++ {
		err := stream.Send(&Buffer{
			Date:  timestamppb.New(time.Date(2017, 1, 1, 0, 0, 0, 0, time.Local).Add(time.Duration(i) * 15 * time.Minute)),
			Value: strconv.Itoa(i),
		})
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}

	return nil
}

func TestLongRunningStream(t *testing.T) {
	ctx := context.Background()

	grpcServer := grpc.NewServer()

	RegisterStreamTestServer(grpcServer, &s{})
	lis := bufconn.Listen(8192)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Fatalf("grpcServer exited with error: %v", err)
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	c := NewStreamTestClient(conn)
	resp, err := c.StreamBuffer(ctx, &BufferRequest{})

	for {
		_, err = resp.Recv()
		if err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("Failed to receive response: %v", err)
		}
	}
}
