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
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type errCodec struct {
	noError bool
}

func (c *errCodec) Marshal(v any) ([]byte, error) {
	if c.noError {
		return []byte{}, nil
	}
	return nil, fmt.Errorf("3987^12 + 4365^12 = 4472^12")
}

func (c *errCodec) Unmarshal(data []byte, v any) error {
	return nil
}

func (c *errCodec) Name() string {
	return "Fermat's near-miss."
}

func (s) TestEncodeDoesntPanic(t *testing.T) {
	for _, e := range listTestEnv() {
		testEncodeDoesntPanic(t, e)
	}
}

func testEncodeDoesntPanic(t *testing.T, e env) {
	te := newTest(t, e)
	erc := &errCodec{}
	te.customCodec = erc
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	te.customCodec = nil
	tc := testgrpc.NewTestServiceClient(te.clientConn())
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Failure case, should not panic.
	tc.EmptyCall(ctx, &testpb.Empty{})
	erc.noError = true
	// Passing case.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
}

type countingProtoCodec struct {
	marshalCount   int32
	unmarshalCount int32
}

func (p *countingProtoCodec) Marshal(v any) ([]byte, error) {
	atomic.AddInt32(&p.marshalCount, 1)
	vv, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	return proto.Marshal(vv)
}

func (p *countingProtoCodec) Unmarshal(data []byte, v any) error {
	atomic.AddInt32(&p.unmarshalCount, 1)
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return proto.Unmarshal(data, vv)
}

func (*countingProtoCodec) Name() string {
	return "proto"
}

func (s) TestForceServerCodec(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	codec := &countingProtoCodec{}
	if err := ss.Start([]grpc.ServerOption{grpc.ForceServerCodec(codec)}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	unmarshalCount := atomic.LoadInt32(&codec.unmarshalCount)
	const wantUnmarshalCount = 1
	if unmarshalCount != wantUnmarshalCount {
		t.Fatalf("protoCodec.unmarshalCount = %d; want %d", unmarshalCount, wantUnmarshalCount)
	}
	marshalCount := atomic.LoadInt32(&codec.marshalCount)
	const wantMarshalCount = 1
	if marshalCount != wantMarshalCount {
		t.Fatalf("protoCodec.marshalCount = %d; want %d", marshalCount, wantMarshalCount)
	}
}

// renameProtoCodec is an encoding.Codec wrapper that allows customizing the
// Name() of another codec.
type renameProtoCodec struct {
	encoding.Codec
	name string
}

func (r *renameProtoCodec) Name() string { return r.name }

// TestForceCodecName confirms that the ForceCodec call option sets the subtype
// in the content-type header according to the Name() of the codec provided.
func (s) TestForceCodecName(t *testing.T) {
	wantContentTypeCh := make(chan []string, 1)
	defer close(wantContentTypeCh)

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Errorf(codes.Internal, "no metadata in context")
			}
			if got, want := md["content-type"], <-wantContentTypeCh; !reflect.DeepEqual(got, want) {
				return nil, status.Errorf(codes.Internal, "got content-type=%q; want [%q]", got, want)
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{grpc.ForceServerCodec(encoding.GetCodec("proto"))}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	codec := &renameProtoCodec{Codec: encoding.GetCodec("proto"), name: "some-test-name"}
	wantContentTypeCh <- []string{"application/grpc+some-test-name"}
	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.ForceCodec(codec)); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	// Confirm the name is converted to lowercase before transmitting.
	codec.name = "aNoTHeRNaME"
	wantContentTypeCh <- []string{"application/grpc+anothername"}
	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.ForceCodec(codec)); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}
}
