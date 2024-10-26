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
	"encoding/base64"
	"reflect"
	"strings"
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

func verifyOutgoingTraceGRPCTraceBinHeader(ctx context.Context, t *testing.T, wantB []byte) {
	gotB := stats.OutgoingTrace(ctx)
	if len(wantB) == 0 && gotB != nil {
		t.Fatalf("stats.OutgoingTrace() is non-nil, want nil")
	}
	if gotB != nil && !reflect.DeepEqual(gotB, wantB) {
		t.Fatalf("stats.OutgoingTrace() = %v, want %v", gotB, wantB)
	}
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
		{
			name: "grpc-trace-bin key",
			md:   metadata.MD{},
			key:  "grpc-trace-bin",
			want: base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}),
		},
		{
			name: "non-grpc-trace-bin key",
			md:   metadata.Pairs("non-trace-bin", base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03})),
			key:  "non-trace-bin",
			want: "", // -bin key that isn't grpc-trace-bin should always get empty string
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if strings.HasSuffix(test.key, "-bin") { // for binary headers set `grpc-trace-bin` using `stats.SetIncomingTrace()`
				b, _ := base64.StdEncoding.DecodeString(test.want)
				ctx = stats.SetIncomingTrace(ctx, b)
			}
			c := NewCustomCarrier(metadata.NewIncomingContext(ctx, test.md))
			got := c.Get(test.key)
			if got != test.want {
				t.Fatalf("Get() = %s, want %s", got, test.want)
			}
		})
	}
}

func (s) TestSet(t *testing.T) {
	tests := []struct {
		name      string
		initialMD metadata.MD
		setKey    string
		setValue  string
		wantValue string // expected value of the set key
		wantKeys  []string
	}{
		{
			name:      "set new key",
			initialMD: metadata.MD{},
			setKey:    "key1",
			setValue:  "value1",
			wantValue: "value1",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "override existing key",
			initialMD: metadata.MD{"key1": []string{"oldvalue"}},
			setKey:    "key1",
			setValue:  "newvalue",
			wantValue: "newvalue",
			wantKeys:  []string{"key1"},
		},
		{
			name:      "set new key with different existing key",
			initialMD: metadata.MD{"key2": []string{"value2"}},
			setKey:    "key1",
			setValue:  "value1",
			wantValue: "value1",
			wantKeys:  []string{"key2", "key1"},
		},
		{
			name:      "set grpc-trace-bin binary key",
			initialMD: metadata.MD{"key1": []string{"value1"}},
			setKey:    "grpc-trace-bin",
			setValue:  base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}),
			wantValue: base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}),
			wantKeys:  []string{"key1", "grpc-trace-bin"},
		},
		{
			name:      "set non-grpc-trace-bin binary key",
			initialMD: metadata.MD{"key1": []string{"value1"}},
			setKey:    "non-grpc-trace-bin",
			setValue:  base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03}),
			wantValue: "", // -bin key that isn't grpc-trace-bin should never be set
			wantKeys:  []string{"key1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := NewCustomCarrier(metadata.NewOutgoingContext(ctx, test.initialMD))
			c.Set(test.setKey, test.setValue)
			gotKeys := c.Keys()
			equalKeys := cmp.Equal(test.wantKeys, gotKeys, cmpopts.SortSlices(func(a, b string) bool { return a < b }))
			if !equalKeys {
				t.Fatalf("c.Keys() = keys %v, want %v", gotKeys, test.wantKeys)
			}
			// for binary headers verify `grpc-trace-bin` binary value for outgoing trace in carrier's context
			if strings.HasSuffix(test.setKey, "-bin") {
				wantB, _ := base64.StdEncoding.DecodeString(test.wantValue)
				verifyOutgoingTraceGRPCTraceBinHeader(c.ctx, t, wantB)
				return
			}
			// for non -bin headers verify string value in carrier's context metadata
			if gotMD, _ := metadata.FromOutgoingContext(c.Context()); test.wantValue != "" && gotMD.Get(test.setKey)[0] != test.setValue {
				t.Fatalf("got value %s, want %s, for key %s", gotMD.Get(test.setKey)[0], test.setValue, test.setKey)
			}
		})
	}
}

func (s) TestGetBinary(t *testing.T) {
	want := []byte{0x01, 0x02, 0x03}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewCustomCarrier(stats.SetIncomingTrace(ctx, want))
	got := c.GetBinary()
	if got == nil {
		t.Fatalf("c.GetBinary() = nil, want %v", got)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("c.GetBinary() = %s, want %s", got, want)
	}
}

func (s) TestSetBinary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := []byte{0x01, 0x02, 0x03}
	c := NewCustomCarrier(ctx)
	c.SetBinary(want)
	verifyOutgoingTraceGRPCTraceBinHeader(c.Context(), t, want)
}
