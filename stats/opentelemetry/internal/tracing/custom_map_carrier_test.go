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
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestGet(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		key  string
		want string
	}{
		{
			name: "existing key",
			md:   metadata.Pairs("key1", "value1"),
			key:  "key1",
			want: "value1",
		},
		{
			name: "non-existing key",
			md:   metadata.Pairs("key1", "value1"),
			key:  "key2",
			want: "",
		},
		{
			name: "empty key",
			md:   metadata.MD{},
			key:  "key1",
			want: "",
		},
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		t.Run(test.name, func(t *testing.T) {
			c := NewCustomCarrier(metadata.NewIncomingContext(ctx, test.md))
			got := c.Get(test.key)
			if got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
			cancel()
		})
	}
}

func (s) TestSet(t *testing.T) {
	tests := []struct {
		name      string
		initialMD metadata.MD // Metadata to initialize the context with
		setKey    string      // Key to set using c.Set()
		setValue  string      // Value to set using c.Set()
		wantKeys  []string    // Expected keys returned by c.Keys()
	}{
		{
			name:      "set new key",
			initialMD: metadata.MD{},
			setKey:    "key1",
			setValue:  "value1",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "override existing key",
			initialMD: metadata.MD{"key1": []string{"oldvalue"}},
			setKey:    "key1",
			setValue:  "newvalue",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "set key with existing unrelated key",
			initialMD: metadata.MD{"key2": []string{"value2"}},
			setKey:    "key1",
			setValue:  "value1",
			wantKeys:  []string{"key2", "key1"}, // Order matesters here!
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			c := NewCustomCarrier(metadata.NewOutgoingContext(ctx, test.initialMD))

			c.Set(test.setKey, test.setValue)

			gotKeys := c.Keys()
			equalKeys := cmp.Equal(test.wantKeys, gotKeys, cmpopts.SortSlices(func(a, b string) bool {
				return a < b
			}))
			if !equalKeys {
				t.Fatalf("got keys %v, want %v", gotKeys, test.wantKeys)
			}
			gotMD, _ := metadata.FromOutgoingContext(c.Context())
			if gotMD.Get(test.setKey)[0] != test.setValue {
				t.Fatalf("got value %s, want %s, for key %s", gotMD.Get(test.setKey)[0], test.setValue, test.setKey)
			}
			cancel()
		})
	}
}

func (s) TestGetBinary(t *testing.T) {
	t.Run("get grpc-trace-bin header", func(t *testing.T) {
		want := []byte{0x01, 0x02, 0x03}
		ctx, cancel := context.WithCancel(context.Background())
		c := NewCustomCarrier(stats.SetIncomingTrace(ctx, want))
		got := c.GetBinary()
		if got == nil {
			t.Fatalf("got nil, want %v", got)
		}
		if string(got) != string(want) {
			t.Fatalf("got %s, want %s", got, want)
		}
		cancel()
	})

	t.Run("get non grpc-trace-bin header", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := NewCustomCarrier(metadata.NewIncomingContext(ctx, metadata.Pairs("non-trace-bin", "\x01\x02\x03")))
		got := c.GetBinary()
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
		cancel()
	})
}

func (s) TestSetBinary(t *testing.T) {
	t.Run("set grpc-trace-bin header", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		want := []byte{0x01, 0x02, 0x03}
		c := NewCustomCarrier(stats.SetIncomingTrace(ctx, want))
		c.SetBinary(want)
		got := stats.OutgoingTrace(c.Context())
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		cancel()
	})

	t.Run("set non grpc-trace-bin header", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{"non-trace-bin": []string{"value"}}))
		got := stats.OutgoingTrace(c.Context())
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
		cancel()
	})
}
