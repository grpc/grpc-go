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
	var protoMsg = v.(proto.Message)
	var sizeNeeded = proto.Size(protoMsg) + 4
	var token = atomic.AddUint32(&globalBufCacheToken, 1)

	buffer := globalBufAlloc(token)

	newSlice := make([]byte, sizeNeeded)

	buffer.SetBuf(newSlice)
	buffer.Reset()
	err := buffer.Marshal(protoMsg)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(buffer.Bytes()))
	copy(out, buffer.Bytes())
	buffer.SetBuf(nil)
	globalBufFree(buffer, token)
	return out, err
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	var token = atomic.AddUint32(&globalBufCacheToken, 1)
	buffer := globalBufAlloc(token)
	buffer.SetBuf(data)
	err := buffer.Unmarshal(v.(proto.Message))
	buffer.SetBuf(nil)
	globalBufFree(buffer, token)
	return err
}

func (protoCodec) String() string {
	return "proto"
}

var (
	poolSize            = uint32(64)
	globalBufCacheToken = uint32(0)
	bufCachePool        = newBufCacheArray(poolSize)
)

func newBufCacheArray(amount uint32) []*bufCache {
	out := make([]*bufCache, amount)

	for i := uint32(0); i < amount; i++ {
		out[i] = &bufCache{cache: &ringCache{}}
	}

	return out
}

func globalBufAlloc(token uint32) *proto.Buffer {
	index := token % poolSize
	return bufCachePool[index].bufAlloc()
}

func globalBufFree(buf *proto.Buffer, token uint32) {
	index := token % poolSize
	bufCachePool[index].bufFree(buf)
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

// Note may want to change for more than 600 streams per channel, depending on the common
// number of streams per channel.
const maxPerRing = 600

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
