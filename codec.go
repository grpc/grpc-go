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

// codecProvider is used to provide new streams with a Codec
type codecProvider interface {
	// Provides a new stream with a codec to be used for it's lifetime
	getCodec() Codec
}

// codecProviderCreator is used to provide transports with codecProviders,
// in order to set the scopes of their possible pools.
type codecProviderCreator interface {
	// Provides a codecProvider to be used by a connection/transport.
	// This can control the scope of codec pools, e.g. global, per-conn, none
	onNewTransport() func() interface{}
}

// protoCodec is a Codec implementation with protobuf. It is the default codec for gRPC.
type protoCodec struct {
	marshalPool   *marshalBufCache
	unmarshalPool *bufCache
}

func (p protoCodec) Marshal(v interface{}) ([]byte, error) {
	var protoMsg = v.(proto.Message)
	var sizeNeeded = proto.Size(protoMsg)
	var currentSlice []byte

	mb := p.marshalPool.marshalBufAlloc()
	buffer := mb.buffer

	if mb.lastSlice != nil && sizeNeeded <= len(mb.lastSlice) {
		currentSlice = mb.lastSlice
	} else {
		currentSlice = make([]byte, sizeNeeded)
	}
	buffer.SetBuf(currentSlice)
	buffer.Reset()
	err := buffer.Marshal(protoMsg)
	if err != nil {
		return nil, err
	}
	out := buffer.Bytes()
	buffer.SetBuf(nil)
	mb.lastSlice = currentSlice
	p.marshalPool.marshalBufFree(mb)
	return out, err
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	buffer := p.unmarshalPool.bufAlloc()
	buffer.SetBuf(data)
	err := buffer.Unmarshal(v.(proto.Message))
	buffer.SetBuf(nil)
	p.unmarshalPool.bufFree(buffer)
	return err
}

func (protoCodec) String() string {
	return "proto"
}

// The protoCodec per-stream creators, and per-transport creator "providers"
// are meant to handle pooling of proto.Buffer objects structs and byte slices.
// The current goal is to keep a pool of buffers per transport connection,
// to be shared by its streams.
type protoCodecProvider struct {
	marshalPool   *marshalBufCache
	unmarshalPool *bufCache
}

func (c protoCodecProvider) getCodec() Codec {
	return &protoCodec{
		marshalPool:   c.marshalPool,
		unmarshalPool: c.unmarshalPool,
	}
}

type protoCodecProviderCreator struct {
}

// Called when a new connection is made. Sets up the pool to be used by
// that connection, for its streams
func (c protoCodecProviderCreator) onNewTransport() func() interface{} {
	marshalPool := &marshalBufCache{
		cache: &ringCache{},
	}
	unmarshalPool := &bufCache{
		cache: &ringCache{},
	}

	provider := &protoCodecProvider{
		marshalPool:   marshalPool,
		unmarshalPool: unmarshalPool,
	}

	return func() interface{} { return provider.getCodec() }
}

func newProtoCodecProviderCreator() *protoCodecProviderCreator {
	return &protoCodecProviderCreator{}
}

// Keeps a buffer used for marshalling, and can also holds on to the last
// byte slice used for marshalling for reuse
type marshalBuffer struct {
	buffer    *proto.Buffer
	lastSlice []byte
}

func newMarshalBuffer() *marshalBuffer {
	return &marshalBuffer{
		buffer: &proto.Buffer{},
	}
}

// generic codec used with user-supplied codec.
// These are used in the same way as the default protoCodec providers,
// but result in the single user-supplied codec being used on every
// connection/stream.
type genericCodecProvider struct {
	codec Codec
}

func (c genericCodecProvider) getCodec() Codec {
	return c.codec
}

type genericCodecProviderCreator struct {
	codec Codec
}

func (c genericCodecProviderCreator) onNewTransport() func() interface{} {
	provider := &genericCodecProvider{
		codec: c.codec,
	}

	return func() interface{} { return provider.getCodec() }
}

func newGenericCodecProviderCreator(codec Codec) *genericCodecProviderCreator {
	return &genericCodecProviderCreator{
		codec: codec,
	}
}

type ringCache struct {
	// The ring holds entries in the indexes i%maxPerRing for i in [readIndex, writeIndex).
	// If readIndex == writeIndex, there is nothing in the ring.
	// If readIndex+maxPerRing == writeIndex, the ring is full.
	// The readIndex and writeIndex are atomic values used for synchronization:
	// the readIndex must be incremented only after removing ring[readIndex%maxPerRing],
	// because the increment makes that location available for writing,
	// and the writeIndex must be incremented only after adding ring[writeIndex%maxPerRing],
	// because the increment makes that location available for reading.
	// Although the reader and writer communicate via atomic operations,
	// it is only safe for one such reader and one such writer to be doing
	// these operations. Readers synchronize on readMu to ensure that
	// there is only one active reader at a time, and similarly writers synchronize
	// on writeMu to ensure that there is only one active writer at a time.
	// Using uint64 for index in order to assume there will never be any overflow.
	ring       [maxPerRing]interface{}
	readMu     sync.Mutex
	readIndex  uint64
	writeMu    sync.Mutex
	writeIndex uint64
}

// Note may want to change for more than 300 streams per channel.
const maxPerRing = 300

// push pushes the object into the buffer if possible.
// It reports whether the message was stored into the buffer.
// (If not, the buffer was full.)
func (s *ringCache) push(m interface{}) bool {
	i := atomic.LoadUint64(&s.writeIndex)
	if i-atomic.LoadUint64(&s.readIndex) >= maxPerRing {
		return false
	}
	s.writeMu.Lock()
	i = atomic.LoadUint64(&s.writeIndex)
	if i-atomic.LoadUint64(&s.readIndex) >= maxPerRing {
		s.writeMu.Unlock()
		return false
	}
	s.ring[i%maxPerRing] = m
	atomic.StoreUint64(&s.writeIndex, i+1)
	s.writeMu.Unlock()
	return true
}

// pop takes out and returns the object from the ring buffer.
// It returns nil if the buffer is empty.
func (s *ringCache) pop() interface{} {
	i := atomic.LoadUint64(&s.readIndex)
	if i == atomic.LoadUint64(&s.writeIndex) {
		return nil
	}
	s.readMu.Lock()
	i = atomic.LoadUint64(&s.readIndex)
	if i == atomic.LoadUint64(&s.writeIndex) {
		s.readMu.Unlock()
		return nil
	}
	m := s.ring[i%maxPerRing]
	s.ring[i%maxPerRing] = nil
	atomic.StoreUint64(&s.readIndex, i+1)
	s.readMu.Unlock()
	return m
}

type marshalBufCache struct {
	cache *ringCache
}

func (c *marshalBufCache) marshalBufAlloc() *marshalBuffer {
	mb := c.cache.pop()
	if mb == nil {
		mb = newMarshalBuffer()
	}
	return mb.(*marshalBuffer)
}

func (c *marshalBufCache) marshalBufFree(mb *marshalBuffer) {
	if mb == nil {
		panic("freeing a nil marshalBuffer")
	}
	c.cache.push(mb)
}

type bufCache struct {
	cache *ringCache
}

func (bc *bufCache) bufAlloc() *proto.Buffer {
	pb := bc.cache.pop()
	if pb == nil {
		pb = &proto.Buffer{}
	}
	return pb.(*proto.Buffer)
}

func (bc *bufCache) bufFree(pb *proto.Buffer) {
	if pb == nil {
		panic("freeing a nil proto.Buffer")
	}
	bc.cache.push(pb)
}
