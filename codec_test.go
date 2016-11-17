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
	"testing"

	"google.golang.org/grpc/test/codec_perf"
)

func newProtoCodec() Codec {
	return &protoCodec{}
}

func marshalAndUnmarshal(protoCodec Codec, expectedBody []byte, t *testing.T) {
	original := &codec_perf.Buffer{}
	original.Body = expectedBody

	var marshalledBytes []byte
	deserialized := &codec_perf.Buffer{}
	var err error

	if marshalledBytes, err = protoCodec.Marshal(original); err != nil {
		t.Fatalf("protoCodec.Marshal(_) returned an error")
	}

	if err := protoCodec.Unmarshal(marshalledBytes, deserialized); err != nil {
		t.Fatalf("protoCodec.Unmarshal(_) returned an error")
	}

	result := deserialized.GetBody()

	for i, v := range result {
		if expectedBody[i] != v {
			t.Fatalf("expected slice differs from result")
		}
	}
}

func TestBasicProtoCodecMarshalAndUnmarshal(t *testing.T) {
	marshalAndUnmarshal(newProtoCodec(), []byte{1, 2, 3}, t)
}

// This tries to make sure that buffers weren't stomped on
// between marshals on codecs taking from the same pool.
func TestStaggeredMarshalAndUnmarshalUsingSamePool(t *testing.T) {
	codec1 := newProtoCodec().(Codec)
	codec2 := newProtoCodec().(Codec)

	expectedBody1 := []byte{1, 2, 3}
	expectedBody2 := []byte{4, 5, 6}

	proto1 := codec_perf.Buffer{Body: expectedBody1}
	proto2 := codec_perf.Buffer{Body: expectedBody2}

	var m1, m2 []byte
	var err error

	if m1, err = codec1.Marshal(&proto1); err != nil {
		t.Fatalf("protoCodec.Marshal(%v) failed", proto1)
	}

	if m2, err = codec2.Marshal(&proto2); err != nil {
		t.Fatalf("protoCodec.Marshal(%v) failed", proto2)
	}

	if err = codec1.Unmarshal(m1, &proto1); err != nil {
		t.Fatalf("protoCodec.Unmarshal(%v) failed", m1)
	}

	if err = codec2.Unmarshal(m2, &proto2); err != nil {
		t.Fatalf("protoCodec.Unmarshal(%v) failed", m2)
	}

	b1 := proto1.GetBody()
	b2 := proto2.GetBody()

	for i, v := range b1 {
		if expectedBody1[i] != v {
			t.Fatalf("expected %v at index %v but got %v", i, expectedBody1[i], v)
		}
	}

	for i, v := range b2 {
		if expectedBody2[i] != v {
			t.Fatalf("expected %v at index %v but got %v", i, expectedBody2[i], v)
		}
	}
}

func TestRingCacheBehaviorAndUseBeyondCapacity(t *testing.T) {
	cache := &ringCache{}
	objects := make([]interface{}, maxPerRing*2)

	// popping when empty should return nil
	for i := 0; i < 10; i++ {
		if res := cache.pop(); res != nil {
			t.Fatalf("cache.pop() expected to return nil")
		}
	}

	// pushing should return a value indicating whether
	// the item was pushed onto the stack of discarded
	for i := 0; i < cap(objects); i++ {
		objects[i] = &i
		expectedPushResult := true
		if i >= maxPerRing {
			expectedPushResult = false
		}
		if cache.push(&i) != expectedPushResult {
			t.Fatalf("unexpected result of pushing onto ring cache")
		}
	}

	// the first "maxPerRing" pushes should have been saved onto the stack
	for i := maxPerRing - 1; i >= 0; i-- {
		if objects[i] != cache.pop() {
			t.Fatalf("unexpected result of popping from ring cache")
		}
	}

	// after popping everything, the cache should be empty and further
	// pops should return nil
	for i := 0; i < 10; i++ {
		if res := cache.pop(); res != nil {
			t.Fatalf("cache.pop() expected to return nil")
		}
	}
}
