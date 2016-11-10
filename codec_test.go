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
	protoCodecProviderCreator := newProtoCodecProviderCreator()
	getCodec := protoCodecProviderCreator.onNewTransport()
	return getCodec().(Codec)
}

func marshalAndUnmarshal(protoCodec Codec, t *testing.T) {
	expectedBody := []byte{1, 2, 3}

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

// The genericCodecProvider needs to hand out the original codec passed in,
// testing here with pointer comparison
func TestGenericCodecProviderCreatorCreatesTheSameCodecProvided(t *testing.T) {
	origCodec := &testCodec{}
	codecProviderCreator := newGenericCodecProviderCreator(origCodec)
	for i := 0; i < 10; i++ {
		getCodec := codecProviderCreator.onNewTransport()
		for k := 0; k < 10; k++ {
			codec := getCodec()
			if codec != origCodec {
				t.Fatalf("generic codec should provide the codec on construction")
			}
		}
	}
}

func TestBasicProtoCodecMarshalAndUnmarshal(t *testing.T) {
	marshalAndUnmarshal(newProtoCodec(), t)
}

func TestConcurrentUsageOfProtoCodec(t *testing.T) {
	numProviderCreators := 5
	numCodecFuncsPerProvider := 5
	numCodecsPerGetCodecFunc := 100
	providerCreators := make([]*protoCodecProviderCreator, numProviderCreators)
	getCodecFuncs := make([]func() interface{}, numCodecFuncsPerProvider*numProviderCreators)
	var wg sync.WaitGroup

	for i := 0; i < numProviderCreators; i++ {
		p := newProtoCodecProviderCreator()
		providerCreators[i] = p

		for k := 0; k < numCodecFuncsPerProvider; k++ {
			getCodecFuncs[i*numCodecFuncsPerProvider+k] = p.onNewTransport()
		}
	}

	for getCodecIndex := 0; getCodecIndex < len(getCodecFuncs); getCodecIndex++ {
		// Create and use codecs from the getCodec func concurrently. Attempt
		// to use the shared pool beyond its capacity
		for i := 0; i < numCodecsPerGetCodecFunc; i++ {
			var codec Codec
			if codec = getCodecFuncs[getCodecIndex]().(Codec); codec == nil {
				t.Fatalf("nil Codec returned from getCodec func")
			}
			wg.Add(1)
			go func(codec Codec) {
				for k := 0; k < maxPerRing*2; k++ {
					marshalAndUnmarshal(codec, t)
				}
				wg.Add(-1)
			}(codec)
		}
	}

	wg.Wait()
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
