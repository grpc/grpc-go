/*
 *
 * Copyright 2026 gRPC authors.
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

package optional_test

import (
	"slices"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/optional"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestOptionalInt tests the scenario of using integer optional values and
// verifies that zero value, constructors, and mutation methods return correct
// outputs.
func (s) TestOptionalInt(t *testing.T) {
	var opt optional.Optional[int]

	// Test unset value.
	if v, set := opt.Value(); set || v != 0 {
		t.Fatalf("Zero-value Option[int] = (%v, %v); want (0, false)", v, set)
	}

	opt = optional.New(42)
	if v, set := opt.Value(); !set || v != 42 {
		t.Fatalf("NewValue(42) = (%v, %v); want (42, true)", v, set)
	}

	opt.SetValue(100)
	if v, set := opt.Value(); !set || v != 100 {
		t.Fatalf("Set(100) = (%v, %v); want (100, true)", v, set)
	}
}

// TestOptionalString tests the scenario of using string optional values and
// verifies that zero value, constructors, and mutation methods return correct
// outputs.
func (s) TestOptionalString(t *testing.T) {
	var opt optional.Optional[string]

	// Test unset value.
	if v, set := opt.Value(); set || v != "" {
		t.Fatalf("Zero-value Option[string] = (%q, %v); want (%q, false)", v, set, "")
	}

	wantString := "test-string"
	opt = optional.New(wantString)
	if v, set := opt.Value(); !set || v != wantString {
		t.Fatalf("NewValue(%q) = (%q, %v); want (%q, true)", wantString, v, set, wantString)
	}

	wantStringNew := "world"
	opt.SetValue(wantStringNew)
	if v, set := opt.Value(); !set || v != wantStringNew {
		t.Fatalf("Set(%q) = (%q, %v); want (%q, true)", wantStringNew, v, set, wantStringNew)
	}
}

// TestOptionalStruct tests the scenario of using a custom struct optional value
// and verifies that custom struct field values are preserved, modified, and
// cleared correctly.
func (s) TestOptionalStruct(t *testing.T) {
	type testStruct struct {
		name string
		age  int
	}

	var opt optional.Optional[testStruct]
	if v, set := opt.Value(); set || v != (testStruct{}) {
		t.Fatalf("Zero-value Option[struct] = (%v, %v); want (empty, false)", v, set)
	}

	want := testStruct{name: "Alice", age: 30}
	opt = optional.New(want)
	if v, set := opt.Value(); !set || v != want {
		t.Fatalf("NewValue(%v) = (%v, %v); want (%v, true)", want, v, set, want)
	}

	want2 := testStruct{name: "Bob", age: 40}
	opt.SetValue(want2)
	if v, set := opt.Value(); !set || v != want2 {
		t.Fatalf("Set(%v) = (%v, %v); want (%v, true)", want2, v, set, want2)
	}
}

// TestOptionalSlice tests the scenario of using a slice optional value and
// verifies that zero value, constructors, and mutation methods return correct
// outputs.
func (s) TestOptionalSlice(t *testing.T) {
	var opt optional.Optional[[]int]
	if v, set := opt.Value(); set || v != nil {
		t.Fatalf("Zero-value Option[[]int] = (%v, %v); want (nil, false)", v, set)
	}

	want := []int{1, 2, 3}
	opt = optional.New(want)
	if v, set := opt.Value(); !set || !slices.Equal(v, want) {
		t.Fatalf("NewValue(%v) = (%v, %v); want (%v, true)", want, v, set, want)
	}

	want2 := []int{4, 5, 6}
	opt.SetValue(want2)
	if v, set := opt.Value(); !set || !slices.Equal(v, want2) {
		t.Fatalf("Set(%v) = (%v, %v); want (%v, true)", &want2, v, set, &want2)
	}
}
