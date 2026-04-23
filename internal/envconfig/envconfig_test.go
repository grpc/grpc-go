/*
 *
 * Copyright 2022 gRPC authors.
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

package envconfig

import (
	"os"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestUint64FromEnv(t *testing.T) {
	var testCases = []struct {
		name          string
		val           string
		def, min, max uint64
		want          uint64
	}{
		{
			name: "error parsing",
			val:  "asdf", def: 5, want: 5,
		}, {
			name: "unset",
			val:  "", def: 5, want: 5,
		}, {
			name: "too low",
			val:  "5", min: 10, want: 10,
		}, {
			name: "too high",
			val:  "5", max: 2, want: 2,
		}, {
			name: "in range",
			val:  "17391", def: 13000, min: 12000, max: 18000, want: 17391,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			const testVar = "testvar"
			if tc.val == "" {
				os.Unsetenv(testVar)
			} else {
				os.Setenv(testVar, tc.val)
			}
			if got := uint64FromEnv(testVar, tc.def, tc.min, tc.max); got != tc.want {
				t.Errorf("uint64FromEnv(%q(=%q), %v, %v, %v) = %v; want %v", testVar, tc.val, tc.def, tc.min, tc.max, got, tc.want)
			}
		})
	}
}

func (s) TestBoolFromEnv(t *testing.T) {
	var testCases = []struct {
		val  string
		def  bool
		want bool
	}{
		{val: "", def: true, want: true},
		{val: "", def: false, want: false},
		{val: "true", def: true, want: true},
		{val: "true", def: false, want: true},
		{val: "false", def: true, want: false},
		{val: "false", def: false, want: false},
		{val: "asdf", def: true, want: true},
		{val: "asdf", def: false, want: false},
	}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			const testVar = "testvar"
			if tc.val == "" {
				os.Unsetenv(testVar)
			} else {
				os.Setenv(testVar, tc.val)
			}
			if got := boolFromEnv(testVar, tc.def); got != tc.want {
				t.Errorf("boolFromEnv(%q(=%q), %v) = %v; want %v", testVar, tc.val, tc.def, got, tc.want)
			}
		})
	}
}

func (s) TestGoroutineLabelsFromEnv(t *testing.T) {
	var testCases = []struct {
		name string
		val  string
		def  GoroutineLabels
		want GoroutineLabels
	}{
		{
			name: "unset_env_non-zero_default",
			val:  "",
			def:  GoroutineLabelServerMethod,
			want: GoroutineLabelServerMethod,
		}, {
			name: "unset_env_zero_default",
			val:  "",
			def:  0,
			want: 0,
		}, {
			name: "force-enable_zero_default",
			val:  "grpc.method=true",
			def:  0,
			want: GoroutineLabelServerMethod,
		}, {
			name: "force-enable_zero_default_with_whitespace",
			val:  " grpc.method\t= true",
			def:  0,
			want: GoroutineLabelServerMethod,
		}, {
			name: "force-enable_zero_default_with_other_garbage",
			val:  "grpc.method=true,foobar",
			def:  0,
			want: GoroutineLabelServerMethod,
		}, {
			name: "force-enable_numeric_zero_default_with_other_garbage",
			val:  "grpc.method=1,foobar",
			def:  0,
			want: GoroutineLabelServerMethod,
		}, {
			name: "force-disable_zero_default",
			val:  "grpc.method=false",
			def:  0,
			want: 0,
		}, {
			name: "force-disable_non-zero_default",
			val:  "grpc.method=false",
			def:  GoroutineLabelServerMethod,
			want: 0,
		}, {
			name: "force-disable_non-zero_default_numeric",
			val:  "grpc.method=0",
			def:  GoroutineLabelServerMethod,
			want: 0,
		}, {
			name: "unknown_val_no_equal",
			val:  "grpc.unknown.garbage",
			def:  GoroutineLabelServerMethod,
			want: GoroutineLabelServerMethod,
		}, {
			name: "unknown_val",
			val:  "grpc.unknown.garbage=fooble",
			def:  GoroutineLabelServerMethod,
			want: GoroutineLabelServerMethod,
		}, {
			name: "unparseable_rhs",
			val:  "grpc.method=quux",
			def:  GoroutineLabelServerMethod,
			want: GoroutineLabelServerMethod,
		},
	}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			const testVar = "testvar"
			if tc.val == "" {
				os.Unsetenv(testVar)
			} else {
				os.Setenv(testVar, tc.val)
			}
			if got := goroutineLabelsFromEnv(testVar, tc.def); got != tc.want {
				t.Errorf("goroutineLabelsFromEnv(%q(=%q), %v) = %v; want %v", testVar, tc.val, tc.def, got, tc.want)
			}
		})
	}

}
