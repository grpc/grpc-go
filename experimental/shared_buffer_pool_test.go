/*
 *
 * Copyright 2023 gRPC authors.
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

package experimental_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/experimental"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 10 * time.Second

func (s) TestRecvBufferPool(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for i := 0; i < 10; i++ {
				preparedMsg := &grpc.PreparedMsg{}
				err := preparedMsg.Encode(stream, &testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{
						Body: []byte{'0' + uint8(i)},
					},
				})
				if err != nil {
					return err
				}
				stream.SendMsg(preparedMsg)
			}
			return nil
		},
	}
	sopts := []grpc.ServerOption{experimental.RecvBufferPool(grpc.NewSharedBufferPool())}
	dopts := []grpc.DialOption{experimental.WithRecvBufferPool(grpc.NewSharedBufferPool())}
	if err := ss.Start(sopts, dopts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	var ngot int
	var buf bytes.Buffer
	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ngot++
		if buf.Len() > 0 {
			buf.WriteByte(',')
		}
		buf.Write(reply.GetPayload().GetBody())
	}
	if want := 10; ngot != want {
		t.Errorf("Got %d replies, want %d", ngot, want)
	}
	if got, want := buf.String(), "0,1,2,3,4,5,6,7,8,9"; got != want {
		t.Errorf("Got replies %q; want %q", got, want)
	}
}

func newPayload(t testpb.PayloadType, size int32) (*testpb.Payload, error) {
	if size < 0 {
		return nil, fmt.Errorf("requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	default:
		return nil, fmt.Errorf("unsupported payload type: %d", t)
	}
	return &testpb.Payload{
		Type: t,
		Body: body,
	}, nil
}

func (s) TestRecvBufferPoolUnary(t *testing.T) {
	const largeSize = 1024
	const bufSize = 1030

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
			if err != nil {
				t.Fatal(err)
			}

			return &testpb.SimpleResponse{
				Payload: payload,
			}, nil
		},
	}

	pool := &checkBufferPool{}

	if err := ss.Start(
		[]grpc.ServerOption{grpc.RecvBufferPool(pool)},
		grpc.WithRecvBufferPool(pool),
	); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const reqCount = 10
	for i := 0; i < reqCount; i++ {
		payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
		if err != nil {
			t.Fatal(err)
		}

		_, err = ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: payload})
		if err != nil {
			t.Fatalf("ss.Client.UnaryCall failed: %f", err)
		}
	}

	const bufferCount = reqCount * 2 // req + resp
	if len(pool.puts) != bufferCount {
		t.Fatalf("Expected 10 buffers to be returned to the pool, got %d", len(pool.puts))
	}

	for _, bs := range pool.puts {
		if len(bs) != bufSize {
			t.Fatalf("Expected buffer size %d, got %d", bufSize, len(bs))
		}
	}
}

type checkBufferPool struct {
	puts [][]byte
}

func (p *checkBufferPool) Get(size int) []byte {
	return make([]byte, size)
}

func (p *checkBufferPool) Put(bs *[]byte) {
	p.puts = append(p.puts, *bs)
}
