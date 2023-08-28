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
	"io"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

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
	if err := ss.Start(
		[]grpc.ServerOption{grpc.RecvBufferPool(grpc.NewSharedBufferPool())},
		grpc.WithRecvBufferPool(grpc.NewSharedBufferPool()),
	); err != nil {
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
