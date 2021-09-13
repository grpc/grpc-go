/*
 *
 * Copyright 2021 gRPC authors.
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

package grpcutil

import (
	"regexp"
	"testing"
	"time"
)

func TestEqualStringSlice(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "equal",
			a:    []string{"a", "b"},
			b:    []string{"a", "b"},
			want: true,
		},
		{
			name: "not equal",
			a:    []string{"a", "b"},
			b:    []string{"a", "b", "c"},
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    []string{"a", "b"},
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualStringSlice(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualStringSlice(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestEqualStringP(t *testing.T) {
	tests := []struct {
		name string
		a    *string
		b    *string
		want bool
	}{
		{
			name: "equal",
			a:    newStringP("a"),
			b:    newStringP("a"),
			want: true,
		},
		{
			name: "not equal",
			a:    newStringP("a"),
			b:    newStringP("b"),
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    newStringP("a"),
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualStringP(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualStringP(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestEqualUint32P(t *testing.T) {
	tests := []struct {
		name string
		a    *uint32
		b    *uint32
		want bool
	}{
		{
			name: "equal",
			a:    newUInt32P(1),
			b:    newUInt32P(1),
			want: true,
		},
		{
			name: "not equal",
			a:    newUInt32P(1),
			b:    newUInt32P(2),
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    newUInt32P(1),
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualUint32P(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualUint32P(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestEqualDurationP(t *testing.T) {
	tests := []struct {
		name string
		a    *time.Duration
		b    *time.Duration
		want bool
	}{
		{
			name: "equal",
			a:    newDurationP(time.Second),
			b:    newDurationP(time.Second),
			want: true,
		},
		{
			name: "not equal",
			a:    newDurationP(time.Second),
			b:    newDurationP(time.Minute),
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    newDurationP(time.Second),
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualDurationP(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualDurationP(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
func TestEqualBoolP(t *testing.T) {
	tests := []struct {
		name string
		a    *bool
		b    *bool
		want bool
	}{
		{
			name: "equal",
			a:    newBoolP(true),
			b:    newBoolP(true),
			want: true,
		},
		{
			name: "not equal",
			a:    newBoolP(true),
			b:    newBoolP(false),
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    newBoolP(true),
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualBoolP(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualBoolP(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestEqualAny(t *testing.T) {
	cmpFunc := func(x, y interface{}) bool {
		return x.(*regexp.Regexp).String() == y.(*regexp.Regexp).String()
	}

	tests := []struct {
		name string
		a    interface{}
		b    interface{}
		want bool
	}{
		{
			name: "equal",
			a:    regexp.MustCompile("foo"),
			b:    regexp.MustCompile("foo"),
			want: true,
		},
		{
			name: "not equal",
			a:    regexp.MustCompile("foo"),
			b:    regexp.MustCompile("bar"),
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one empty",
			a:    regexp.MustCompile("foo"),
			b:    nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualAny(tt.a, tt.b, cmpFunc); got != tt.want {
				t.Errorf("EqualAny(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func newStringP(s string) *string {
	return &s
}

func newUInt32P(i uint32) *uint32 {
	return &i
}

func newBoolP(b bool) *bool {
	return &b
}

func newDurationP(d time.Duration) *time.Duration {
	return &d
}
