/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"sync"

	"github.com/golang/protobuf/proto"
)

// Codec defines the interface gRPC uses to encode and decode messages.
type Codec interface {
	// Marshal returns the wire format of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the wire format into v.
	Unmarshal(data []byte, v interface{}) error
	// String returns the name of the Codec implementation. The returned
	// string will be used as part of content type in transmission.
	String() string
}

type sliceSuppliedCodec interface {
	ComputeSizeNeeded(v interface{}) int
	MarshalUseSlice(v interface{}, marshalTo []byte) ([]byte, error)
}

// protoCodec is a Codec implementation with protobuf. It is the default codec for gRPC.
type protoCodec struct {
}

func (p protoCodec) Marshal(v interface{}) ([]byte, error) {
	var sizeNeeded = p.ComputeSizeNeeded(v)
	newSlice := make([]byte, sizeNeeded)
	return p.MarshalUseSlice(v, newSlice)
}

func (p protoCodec) ComputeSizeNeeded(v interface{}) int {
	const (
		protoSizeFieldLength = 4
	)

	var protoMsg = v.(proto.Message)

	// Adding 4 to proto.Size avoids a realloc when appending the 4 byte
	// length field in 'proto.Buffer.enc_len_thing'
	var sizeNeeded = proto.Size(protoMsg) + protoSizeFieldLength
	return sizeNeeded
}

func (p protoCodec) MarshalUseSlice(v interface{}, marshalTo []byte) ([]byte, error) {
	var protoMsg = v.(proto.Message)
	buffer := globalBufAlloc()

	buffer.SetBuf(marshalTo)
	buffer.Reset()
	err := buffer.Marshal(protoMsg)
	if err != nil {
		return nil, err
	}
	out := buffer.Bytes()
	buffer.SetBuf(nil)
	globalBufFree(buffer)
	return out, nil
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	//var token = atomic.AddUint32(&globalBufCacheToken, 1)
	buffer := globalBufAlloc()
	buffer.SetBuf(data)
	err := buffer.Unmarshal(v.(proto.Message))
	buffer.SetBuf(nil)
	globalBufFree(buffer)
	return err
}

func (protoCodec) String() string {
	return "proto"
}

var (
	bufCachePool = &sync.Pool{New: func() interface{} { return &proto.Buffer{} }}
)

func globalBufAlloc() *proto.Buffer {
	return bufCachePool.Get().(*proto.Buffer)
}

func globalBufFree(buf *proto.Buffer) {
	bufCachePool.Put(buf)
}
