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
	"google.golang.org/protobuf/proto"
)

func init() {
	encoding.RegisterCodecV2(&codecV2{SharedBufferPool: encoding.NewSharedBufferPool()})
}

// codec is a CodecV2 implementation with protobuf. It is the default codec for
// gRPC.
type codecV2 struct {
	encoding.SharedBufferPool
}

var _ encoding.CodecV2 = (*codecV2)(nil)

func (c *codecV2) Marshal(v any) (encoding.BufferSlice, error) {
	vv := messageV2Of(v)
	if vv == nil {
		return nil, fmt.Errorf("proto: failed to marshal, message is %T, want proto.Message", v)
	}

	buf := c.Get(proto.Size(vv))
	_, err := proto.MarshalOptions{}.MarshalAppend(buf[:0], vv)
	if err != nil {
		c.Put(buf)
		return nil, err
	} else {
		return encoding.BufferSlice{encoding.NewBuffer(buf, c.Put)}, nil
	}
}

func (c *codecV2) Unmarshal(data encoding.BufferSlice, v any) (err error) {
	vv := messageV2Of(v)
	if vv == nil {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}

	defer data.Free()

	buf := c.Get(data.Len())
	defer c.Put(buf)
	data.WriteTo(buf)
	// TODO: Upgrade proto.Unmarshal to support encoding.BufferSlice
	return proto.Unmarshal(buf, vv)
}

func (c *codecV2) Name() string {
	return Name
}
