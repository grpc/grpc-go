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
	"sync/atomic"

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

// protoCodec is a Codec implementation with protobuf. It is the default codec for gRPC.
type protoCodec struct {
}

func (p protoCodec) Marshal(v interface{}) ([]byte, error) {
	protoMsg := v.(proto.Message)
	buffer := protoBufferPool.Get().(*proto.Buffer)
	defer func() {
		buffer.SetBuf(nil)
		protoBufferPool.Put(buffer)
	}()

	newSlice := make([]byte, 0, atomic.LoadUint32(&lastMarshaledSize))

	buffer.SetBuf(newSlice)
	buffer.Reset()
	if err := buffer.Marshal(protoMsg); err != nil {
		return nil, err
	}
	out := buffer.Bytes()
	atomic.StoreUint32(&lastMarshaledSize, uint32(len(out)))
	return out, nil
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	buffer := protoBufferPool.Get().(*proto.Buffer)
	buffer.SetBuf(data)
	err := buffer.Unmarshal(v.(proto.Message))
	buffer.SetBuf(nil)
	protoBufferPool.Put(buffer)
	return err
}

func (protoCodec) String() string {
	return "proto"
}

var (
	protoBufferPool   = &sync.Pool{New: func() interface{} { return &proto.Buffer{} }}
	lastMarshaledSize = uint32(0)
)
