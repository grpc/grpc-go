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

package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

type CountingBufferPool struct {
	alloc   int32
	dealloc int32
}

func (c *CountingBufferPool) Get(length int) []byte {
	atomic.AddInt32(&c.alloc, 1)
	return make([]byte, length)
}

// Put returns a buffer to the pool.
func (c *CountingBufferPool) Put(b *[]byte) {
	atomic.AddInt32(&c.dealloc, 1)
}

func (c *CountingBufferPool) ResetAllocs() int {
	return int(atomic.SwapInt32(&c.alloc, 0))
}

func (c *CountingBufferPool) ResetDeallocs() int {
	return int(atomic.SwapInt32(&c.dealloc, 0))
}

func (s) TestEncoderBufferPool(t *testing.T) {
	body := make([]byte, 1<<10)
	rand.Read(body)

	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testgrpc.SimpleRequest) (*testgrpc.SimpleResponse, error) {
			return &testgrpc.SimpleResponse{
				Payload: &testgrpc.Payload{
					Body: body,
				},
			}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for i := 0; i < 10; i++ {
				err := stream.SendMsg(&testgrpc.StreamingOutputCallResponse{
					Payload: &testgrpc.Payload{
						Body: body,
					},
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			return nil
		},
	}

	var (
		serverPool CountingBufferPool
		clientPool CountingBufferPool
	)

	if err := ss.Start(
		[]grpc.ServerOption{grpc.ServerEncoderBufferPool(&serverPool)},
		grpc.WithDefaultCallOptions(grpc.ClientEncoderBufferPool(&clientPool)),
	); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Assert that we can communicate without the buffer pool
	response, err := ss.Client.UnaryCall(ctx, &testgrpc.SimpleRequest{
		Payload: &testgrpc.Payload{
			Body: body,
		},
	}, grpc.ClientEncoderBufferPool(nil))
	if err != nil {
		t.Fatalf("ss.Client.UnaryCall failed: %s", err)
	}

	if !bytes.Equal(response.GetPayload().GetBody(), body) {
		t.Fatal("received mismatching buffer")
	}

	response, err = ss.Client.UnaryCall(ctx, &testgrpc.SimpleRequest{
		Payload: &testgrpc.Payload{
			Body: body,
		},
	})
	if err != nil {
		t.Fatalf("ss.Client.UnaryCall failed: %f", err)
	}

	if !bytes.Equal(response.GetPayload().GetBody(), body) {
		t.Fatal("received mismatching buffer")
	}

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	var ngot int
	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply.GetPayload().GetBody(), body) {
			t.Fatal("received mismatching buffer")
		}
		ngot++
	}
	if want := 10; ngot != want {
		t.Errorf("Got %d replies, want %d", ngot, want)
	}

	if got := clientPool.ResetAllocs(); got != 1 {
		t.Errorf("Got %d client allocation, want %d", got, 0)
	}
	if got := clientPool.ResetDeallocs(); got != 1 {
		t.Errorf("Got %d client deallocation, want %d", got, 0)
	}
	if got := serverPool.ResetAllocs(); got != 12 {
		t.Errorf("Got %d server allocation, want %d", got, 10)
	}
	if got := serverPool.ResetDeallocs(); got != 12 {
		t.Errorf("Got %d server deallocation, want %d", got, 10)
	}
}
