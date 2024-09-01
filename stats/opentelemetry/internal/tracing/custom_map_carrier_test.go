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
	"errors"
	"testing"

	"google.golang.org/grpc/metadata"
)

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
		t.Run(tt.name, func(t *testing.T) {
			c := NewCustomCarrier(tt.md)
			got := c.Get(tt.key)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func (s) TestSet(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			c := NewCustomCarrier(tt.md)
			got := c.Get(tt.key)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func (s) TestGetBinary(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		key  string
		want []byte
		err  error
	}{
		{
			name: "get grpc-trace-bin key",
			md:   metadata.Pairs("grpc-trace-bin", "\x01\x02\x03"),
			key:  "grpc-trace-bin",
			want: []byte{0x01, 0x02, 0x03},
			err:  nil,
		},
		{
			name: "get non grpc-trace-bin key",
			md:   metadata.Pairs("non-trace-bin", "somevalue"),
			key:  "non-trace-bin",
			want: nil,
			err:  errors.New("only support 'grpc-trace-bin' binary header"),
		},
		{
			name: "key not found",
			md:   metadata.MD{},
			key:  "grpc-trace-bin",
			want: nil,
			err:  errors.New("key not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCustomCarrier(tt.md)
			got, err := c.GetBinary(tt.key)
			if (err != nil && tt.err == nil) || (err == nil && tt.err != nil) {
				t.Errorf("got %v, want error %v, ", tt.err, err)
			} else if err != nil && err.Error() != tt.err.Error() {
				t.Errorf("got %v, want error %v, ", tt.err, err)
			}

			if string(got) != string(tt.want) {
				t.Errorf("expected %s, got %s", got, tt.want)
			}
		})
	}
}

func (s) TestSetBinary(t *testing.T) {
	tests := []struct {
		name  string
		md    metadata.MD
		key   string
		value []byte
		want  metadata.MD
	}{
		{
			name:  "set grpc-trace-bin key",
			md:    metadata.MD{},
			key:   "grpc-trace-bin",
			value: []byte{0x01, 0x02, 0x03},
			want:  metadata.Pairs("grpc-trace-bin", "\x01\x02\x03"),
		},
		{
			name:  "ignore non grpc-trace-bin key",
			md:    metadata.MD{},
			key:   "non-trace-bin",
			value: []byte{0x01, 0x02, 0x03},
			want:  metadata.MD{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCustomCarrier(tt.md)
			c.SetBinary(tt.key, tt.value)
			if tt.key == "grpc-trace-bin" {
				if val := c.Md[tt.key][0]; val != string(tt.value) {
					t.Errorf("got %s, want %s", val, string(tt.value))
				}
			} else {
				if _, ok := c.Md[tt.key]; ok {
					t.Errorf("wanted key %q to be ignored", tt.key)
				}
			}
		})
	}
}
