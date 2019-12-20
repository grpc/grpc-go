/*
 *
 * Copyright 2018 gRPC authors.
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

// Package proto defines the protobuf codec. Importing this package will
// register the codec.
package proto

import (
	"sync"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/encoding"
)

// Name is the name registered for the proto compressor.
const Name = "proto"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with protobuf. It is the default codec for gRPC.
type codec struct{}

func marshal(v interface{}, pb *proto.Buffer) ([]byte, error) {
	protoMsg := v.(proto.Message)
	newSlice := returnBufferPool.Get().([]byte)

	pb.SetBuf(newSlice)
	pb.Reset()
	if err := pb.Marshal(protoMsg); err != nil {
		return nil, err
	}
	out := pb.Bytes()
	return out, nil
}

func (codec) Marshal(v interface{}) ([]byte, error) {
	if pm, ok := v.(proto.Marshaler); ok {
		// object can marshal itself, no need for buffer
		return pm.Marshal()
	}

	pb := protoBufferPool.Get().(*proto.Buffer)
	out, err := marshal(v, pb)

	// put back buffer and lose the ref to the slice
	pb.SetBuf(nil)
	protoBufferPool.Put(pb)
	return out, err
}

func (codec) Unmarshal(data []byte, v interface{}) error {
	protoMsg := v.(proto.Message)
	protoMsg.Reset()

	if pu, ok := protoMsg.(proto.Unmarshaler); ok {
		// object can unmarshal itself, no need for buffer
		return pu.Unmarshal(data)
	}

	pb := protoBufferPool.Get().(*proto.Buffer)
	pb.SetBuf(data)
	err := pb.Unmarshal(protoMsg)
	pb.SetBuf(nil)
	protoBufferPool.Put(pb)
	return err
}

func (codec) ReturnBuffer(data []byte) {
	// Make sure we set the length of the buffer to zero so that future appends
	// will start from the zeroeth byte, not append to the previous, stale data.
	//
	// Apparently, sync.Pool with non-pointer objects (slices, in this case)
	// causes small allocations because of how interface{} works under the hood.
	// This isn't a problem for us, however, because we're more concerned with
	// _how_ much that allocation is. Ideally, we'd be using bytes.Buffer as the
	// Marshal return value to remove even that allocation, but we can't change
	// the Marshal interface at this point.
	returnBufferPool.Put(data[:0])
}

func (codec) Name() string {
	return Name
}

var protoBufferPool = &sync.Pool{
	New: func() interface{} {
		return &proto.Buffer{}
	},
}

var returnBufferPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 16)
	},
}
