/*
 *
 * Copyright 2024 gRPC authors.
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
	"fmt"

	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/internal/envconfig"
	imem "google.golang.org/grpc/internal/mem"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"
)

// Name is the name registered for the proto compressor.
const Name = "proto"

func init() {
	encoding.RegisterCodecV2(&codecV2{})
}

// codec is a CodecV2 implementation with protobuf. It is the default codec for
// gRPC.
type codecV2 struct{}

// codecDirtyPool is a non-zeroing buffer pool used by the codec when dirty
// codec pooling is enabled. It is created in init() following the same pattern
// used by the ALTS conn package.
var codecDirtyPool *imem.BinaryTieredBufferPool

// codecBufferPool returns the pool used for the intermediate buffers this codec
// acquires while marshaling and unmarshaling. It is the default (zeroing) pool
// unless dirty codec pooling is enabled, in which case it is the non-zeroing
// pool.
//
// Dirty pooling is safe here because both call sites fully overwrite the buffer
// before any of its contents are read: Marshal writes exactly proto.Size(v)
// bytes over the buffer with MarshalAppend, and Unmarshal copies the whole
// BufferSlice into it with CopyTo before proto.Unmarshal sees it.
func codecBufferPool() mem.BufferPool {
	if envconfig.EnableCodecDirtyBufferPooling {
		return codecDirtyPool
	}
	return mem.DefaultBufferPool()
}

func init() {
	var err error
	codecDirtyPool, err = imem.NewDirtyBinaryTieredBufferPool(
		8,
		12, // Go page size, 4KB
		14, // 16KB (max HTTP/2 frame size used by gRPC)
		15, // 32KB (default buffer size for io.Copy)
		20, // 1MB
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to create codec dirty buffer pool: %v", err))
	}
}

func (c *codecV2) Marshal(v any) (data mem.BufferSlice, err error) {
	vv := messageV2Of(v)
	if vv == nil {
		return nil, fmt.Errorf("proto: failed to marshal, message is %T, want proto.Message", v)
	}

	// Important: if we remove this Size call then we cannot use
	// UseCachedSize in MarshalOptions below.
	size := proto.Size(vv)

	// MarshalOptions with UseCachedSize allows reusing the result from the
	// previous Size call. This is safe here because:
	//
	// 1. We just computed the size.
	// 2. We assume the message is not being mutated concurrently.
	//
	// Important: If the proto.Size call above is removed, using UseCachedSize
	// becomes unsafe and may lead to incorrect marshaling.
	//
	// For more details, see the doc of UseCachedSize:
	// https://pkg.go.dev/google.golang.org/protobuf/proto#MarshalOptions
	marshalOptions := proto.MarshalOptions{UseCachedSize: true}

	if mem.IsBelowBufferPoolingThreshold(size) {
		buf, err := marshalOptions.Marshal(vv)
		if err != nil {
			return nil, err
		}
		data = append(data, mem.SliceBuffer(buf))
	} else {
		pool := codecBufferPool()
		buf := pool.Get(size)
		if _, err := marshalOptions.MarshalAppend((*buf)[:0], vv); err != nil {
			pool.Put(buf)
			return nil, err
		}
		data = append(data, mem.NewBuffer(buf, pool))
	}

	return data, nil
}

func (c *codecV2) Unmarshal(data mem.BufferSlice, v any) (err error) {
	vv := messageV2Of(v)
	if vv == nil {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}

	buf := data.MaterializeToBuffer(codecBufferPool())
	defer buf.Free()
	// TODO: Upgrade proto.Unmarshal to support mem.BufferSlice. Right now, it's not
	//  really possible without a major overhaul of the proto package, but the
	//  vtprotobuf library may be able to support this.
	return proto.Unmarshal(buf.ReadOnlyData(), vv)
}

func messageV2Of(v any) proto.Message {
	switch v := v.(type) {
	case protoadapt.MessageV1:
		return protoadapt.MessageV2Of(v)
	case protoadapt.MessageV2:
		return v
	}

	return nil
}

func (c *codecV2) Name() string {
	return Name
}
