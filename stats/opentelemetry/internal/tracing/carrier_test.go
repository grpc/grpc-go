/*
 *
 * Copyright 2024 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     htestp://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package tracing

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestIncomingCarrier verifies that `IncomingCarrier.Get()` returns correct
// value for the corresponding key in the carrier's context metadata, if key is
// present. If key is not present, it verifies that empty string is returned.
//
// If multiple values are present for a key, it verifies that last value is
// returned.
//
// If key ends with `-bin`, it verifies that a correct binary value is returned
// in the string format for the binary header.
func (s) TestIncomingCarrier(t *testing.T) {
	tests := []struct {
		name     string
		md       metadata.MD
		key      string
		want     string
		wantKeys []string
	}{
		{
			name:     "existing key",
			md:       metadata.Pairs("key1", "value1"),
			key:      "key1",
			want:     "value1",
			wantKeys: []string{"key1"},
		},
		{
			name:     "non-existing key",
			md:       metadata.Pairs("key1", "value1"),
			key:      "key2",
			want:     "",
			wantKeys: []string{"key1"},
		},
		{
			name:     "empty key",
			md:       metadata.MD{},
			key:      "key1",
			want:     "",
			wantKeys: []string{},
		},
		{
			name:     "more than one key/value pair",
			md:       metadata.MD{"key1": []string{"value1"}, "key2": []string{"value2"}},
			key:      "key2",
			want:     "value2",
			wantKeys: []string{"key1", "key2"},
		},
		{
			name:     "more than one value for a key",
			md:       metadata.MD{"key1": []string{"value1", "value2"}},
			key:      "key1",
			want:     "value2",
			wantKeys: []string{"key1"},
		},
		{
			name:     "grpc-trace-bin key",
			md:       metadata.Pairs("grpc-trace-bin", string([]byte{0x01, 0x02, 0x03})),
			key:      "grpc-trace-bin",
			want:     string([]byte{0x01, 0x02, 0x03}),
			wantKeys: []string{"grpc-trace-bin"},
		},
		{
			name:     "grpc-trace-bin key with another string key",
			md:       metadata.MD{"key1": []string{"value1"}, "grpc-trace-bin": []string{string([]byte{0x01, 0x02, 0x03})}},
			key:      "grpc-trace-bin",
			want:     string([]byte{0x01, 0x02, 0x03}),
			wantKeys: []string{"key1", "grpc-trace-bin"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := NewIncomingCarrier(metadata.NewIncomingContext(ctx, test.md))
			got := c.Get(test.key)
			if got != test.want {
				t.Fatalf("c.Get() = %s, want %s", got, test.want)
			}
			if gotKeys := c.Keys(); !cmp.Equal(test.wantKeys, gotKeys, cmpopts.SortSlices(func(a, b string) bool { return a < b })) {
				t.Fatalf("c.Keys() = keys %v, want %v", gotKeys, test.wantKeys)
			}
		})
	}
}

// TestOutgoingCarrier verifies that a key-value pair is set in carrier's
// context metadata using `OutgoingCarrier.Set()`. If key is not present, it
// verifies that key-value pair is insterted. If key is already present, it
// verifies that new value is appended at the end of list for the existing key.
//
// If key ends with `-bin`, it verifies that a binary value is set for
// `-bin` header in string format.
//
// It also verifies that both existing and newly inserted keys are present in
// the carrier's context using `Carrier.Keys()`.
func (s) TestOutgoingCarrier(t *testing.T) {
	tests := []struct {
		name      string
		initialMD metadata.MD
		setKey    string
		setValue  string
		wantValue string // expected value of the set key
		wantKeys  []string
	}{
		{
			name:      "new key",
			initialMD: metadata.MD{},
			setKey:    "key1",
			setValue:  "value1",
			wantValue: "value1",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "add to existing key",
			initialMD: metadata.MD{"key1": []string{"oldvalue"}},
			setKey:    "key1",
			setValue:  "newvalue",
			wantValue: "newvalue",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "new key with different existing key",
			initialMD: metadata.MD{"key2": []string{"value2"}},
			setKey:    "key1",
			setValue:  "value1",
			wantValue: "value1",
			wantKeys:  []string{"key2", "key1"},
		},
		{
			name:      "grpc-trace-bin binary key",
			initialMD: metadata.MD{"key1": []string{"value1"}},
			setKey:    "grpc-trace-bin",
			setValue:  string([]byte{0x01, 0x02, 0x03}),
			wantValue: string([]byte{0x01, 0x02, 0x03}),
			wantKeys:  []string{"key1", "grpc-trace-bin"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := NewOutgoingCarrier(metadata.NewOutgoingContext(ctx, test.initialMD))
			c.Set(test.setKey, test.setValue)
			if gotKeys := c.Keys(); !cmp.Equal(test.wantKeys, gotKeys, cmpopts.SortSlices(func(a, b string) bool { return a < b })) {
				t.Fatalf("c.Keys() = keys %v, want %v", gotKeys, test.wantKeys)
			}
			if md, ok := metadata.FromOutgoingContext(c.Context()); ok && md.Get(test.setKey)[len(md.Get(test.setKey))-1] != test.wantValue {
				t.Fatalf("got value %s, want %s, for key %s", md.Get(test.setKey)[len(md.Get(test.setKey))-1], test.wantValue, test.setKey)
			}
		})
	}
}
