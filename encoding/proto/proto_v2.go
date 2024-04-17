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

package proto

import (
	"fmt"

	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/proto"
)

func init() {
	encoding.RegisterCodecV2(&codecV2{})
}

// codec is a CodecV2 implementation with protobuf. It is the default codec for
// gRPC.
type codecV2 struct{}

var _ encoding.CodecV2 = (*codecV2)(nil)

func (c *codecV2) Marshal(v any) (mem.BufferSlice, error) {
	vv := messageV2Of(v)
	if vv == nil {
		return nil, fmt.Errorf("proto: failed to marshal, message is %T, want proto.Message", v)
	}

	buf := mem.DefaultBufferPool.Get(proto.Size(vv))
	_, err := proto.MarshalOptions{}.MarshalAppend(buf[:0], vv)
	if err != nil {
		mem.DefaultBufferPool.Put(buf)
		return nil, err
	} else {
		return mem.BufferSlice{mem.NewBuffer(buf, mem.DefaultBufferPool.Put)}, nil
	}
}

func (c *codecV2) Unmarshal(data mem.BufferSlice, v any) (err error) {
	vv := messageV2Of(v)
	if vv == nil {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}

	defer data.Free()

	buf := data.LazyMaterialize(mem.DefaultBufferPool)
	defer buf.Free()
	// TODO: Upgrade proto.Unmarshal to support encoding.BufferSlice
	return proto.Unmarshal(buf.ReadOnlyData(), vv)
}

func (c *codecV2) Name() string {
	return Name
}
