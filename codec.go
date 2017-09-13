/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc

import (
	"math"
	"strings"
	"sync"

	"github.com/golang/protobuf/proto"
)

var codecs = map[string]Codec{"proto": protoCodec{}}

// RegisterCodec registers the provided Codec for use with all gRPC clients
// and servers.
//
// The Codec will be stored and looked up by result of it's String() method,
// which should match the content-subtype of the encoding handled by the Codec.
// This is case-insensitive, and is stored and looked up as lowercase.
// If the result of calling String() is an empty string, RegisterCodec will
// panic. See Content-Type on https://grpc.io/docs/guides/wire.html#requests
// for more details.
//
// By default, a Codec for "proto" is registered.
//
// RegisterCodec should only be called from init() functions, and is not
// thread-safe. Codecs can be overwritten in the registry, including the
// default "proto" Codec.
//
func RegisterCodec(c Codec) {
	contentSubtype := strings.ToLower(c.String())
	if contentSubtype == "" {
		panic("cannot register Codec with empty string result for String()")
	}
	codecs[contentSubtype] = c
}

// Codec defines the interface gRPC uses to encode and decode messages.
// Note that implementations of this interface must be thread safe;
// a Codec's methods can be called from concurrent goroutines.
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

type cachedProtoBuffer struct {
	lastMarshaledSize uint32
	proto.Buffer
}

func capToMaxInt32(val int) uint32 {
	if val > math.MaxInt32 {
		return uint32(math.MaxInt32)
	}
	return uint32(val)
}

func (p protoCodec) marshal(v interface{}, cb *cachedProtoBuffer) ([]byte, error) {
	protoMsg := v.(proto.Message)
	newSlice := make([]byte, 0, cb.lastMarshaledSize)

	cb.SetBuf(newSlice)
	cb.Reset()
	if err := cb.Marshal(protoMsg); err != nil {
		return nil, err
	}
	out := cb.Bytes()
	cb.lastMarshaledSize = capToMaxInt32(len(out))
	return out, nil
}

func (p protoCodec) Marshal(v interface{}) ([]byte, error) {
	cb := protoBufferPool.Get().(*cachedProtoBuffer)
	out, err := p.marshal(v, cb)

	// put back buffer and lose the ref to the slice
	cb.SetBuf(nil)
	protoBufferPool.Put(cb)
	return out, err
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	cb := protoBufferPool.Get().(*cachedProtoBuffer)
	cb.SetBuf(data)
	v.(proto.Message).Reset()
	err := cb.Unmarshal(v.(proto.Message))
	cb.SetBuf(nil)
	protoBufferPool.Put(cb)
	return err
}

func (protoCodec) String() string {
	return "proto"
}

var (
	protoBufferPool = &sync.Pool{
		New: func() interface{} {
			return &cachedProtoBuffer{
				Buffer:            proto.Buffer{},
				lastMarshaledSize: 16,
			}
		},
	}
)
