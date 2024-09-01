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

package tracing

import (
	"context"
	"reflect"
	"testing"

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

// sameElements checks if two string slices have the same elements,
// ignoring order.
func sameElements(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	countA := make(map[string]int)
	countB := make(map[string]int)
	for _, s := range a {
		countA[s]++
	}
	for _, s := range b {
		countB[s]++
	}

	for k, v := range countA {
		if countB[k] != v {
			return false
		}
	}
	return true
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

	for _, tt := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		t.Run(tt.name, func(t *testing.T) {
			c := NewCustomCarrier(metadata.NewIncomingContext(ctx, tt.md))
			got := c.Get(tt.key)
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
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
			wantKeys:  []string{"key2", "key1"}, // Order matters here!
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			c := NewCustomCarrier(metadata.NewOutgoingContext(ctx, tt.initialMD))

			c.Set(tt.setKey, tt.setValue)

			gotKeys := c.Keys()
			if !sameElements(gotKeys, tt.wantKeys) {
				t.Fatalf("got keys %v, want %v", gotKeys, tt.wantKeys)
			}
			gotMD, _ := metadata.FromOutgoingContext(*c.Ctx)
			if gotMD.Get(tt.setKey)[0] != tt.setValue {
				t.Fatalf("got value %s, want %s, for key %s", gotMD.Get(tt.setKey)[0], tt.setValue, tt.setKey)
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
		got, err := c.GetBinary()
		if err != nil {
			t.Fatalf("got error %v, want nil", err)
		}
		if string(got) != string(want) {
			t.Fatalf("got %s, want %s", got, want)
		}
		cancel()
	})

	t.Run("get non grpc-trace-bin header", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := NewCustomCarrier(metadata.NewIncomingContext(ctx, metadata.Pairs("non-trace-bin", "\x01\x02\x03")))
		_, err := c.GetBinary()
		if err == nil {
			t.Fatalf("got nil error, want error")
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
		got := stats.OutgoingTrace(*c.Ctx)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		cancel()
	})

	t.Run("set non grpc-trace-bin header", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := NewCustomCarrier(metadata.NewOutgoingContext(ctx, metadata.MD{"non-trace-bin": []string{"value"}}))
		got := stats.OutgoingTrace(*c.Ctx)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
		cancel()
	})
}
