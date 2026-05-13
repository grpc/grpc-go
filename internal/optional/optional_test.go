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

// TestOption_Int tests the scenario of using integer optional values and
// verifies that default value, constructors, and mutation methods work as
// expected for primitive integers.
func (s) TestOption_Int(t *testing.T) {
	var opt optional.Optional[int]
	// Test unset value.
	if v, set := opt.Get(); set || v != 0 {
		t.Fatalf("Zero-value Option[int] = (%v, %v); want (0, false)", v, set)
	}

	// Test that New() function also returns an unset optional value.
	optNew := optional.New[int]()
	if v, set := optNew.Get(); set || v != 0 {
		t.Fatalf("New[int]() = (%v, %v); want (0, false)", v, set)
	}

	optVal := optional.NewValue(42)
	if v, set := optVal.Get(); !set || v != 42 {
		t.Fatalf("NewValue(42) = (%v, %v); want (42, true)", v, set)
	}

	opt.Set(100)
	if v, set := opt.Get(); !set || v != 100 {
		t.Fatalf("Set(100) = (%v, %v); want (100, true)", v, set)
	}
}

// TestOption_String tests the scenario of using string optional values and
// verifies that default value, constructors, and mutation methods work as
// expected for text strings.
func (s) TestOption_String(t *testing.T) {
	var opt optional.Optional[string]
	// Test unset value.
	if v, set := opt.Get(); set || v != "" {
		t.Fatalf("Zero-value Option[string] = (%q, %v); want (%q, false)", v, set, "")
	}

	// Test that New() function also returns an unset optional value.
	optNew := optional.New[string]()
	if v, set := optNew.Get(); set || v != "" {
		t.Fatalf("New Option[string] = (%q, %v); want (%q, false)", v, set, "")
	}

	wantString := "test-string"
	optVal := optional.NewValue(wantString)
	if v, set := optVal.Get(); !set || v != wantString {
		t.Fatalf("NewValue(%q) = (%q, %v); want (%q, true)", wantString, v, set, wantString)
	}

	wantStringNew := "world"
	opt.Set(wantStringNew)
	if v, set := opt.Get(); !set || v != wantStringNew {
		t.Fatalf("Set(%q) = (%q, %v); want (%q, true)", wantStringNew, v, set, wantStringNew)
	}
}

// TestOption_Struct tests the scenario of using a custom struct type inside an
// option type and verifies that custom struct field values are preserved,
// modified, and cleared correctly.
func (s) TestOption_Struct(t *testing.T) {
	type testStruct struct {
		name string
		age  int
	}
	val1 := testStruct{name: "Alice", age: 30}
	val2 := testStruct{name: "Bob", age: 40}

	var opt optional.Optional[testStruct]
	if v, set := opt.Get(); set || v != (testStruct{}) {
		t.Fatalf("Zero-value Option[struct] = (%v, %v); want (empty, false)", v, set)
	}

	optVal := optional.NewValue(val1)
	if v, set := optVal.Get(); !set || v != val1 {
		t.Fatalf("NewValue(val1) = (%v, %v); want (%v, true)", v, set, val1)
	}

	opt.Set(val2)
	if v, set := opt.Get(); !set || v != val2 {
		t.Fatalf("Set(val2) = (%v, %v); want (%v, true)", v, set, val2)
	}
}

// TestOption_Slice tests the scenario of using a pointer type inside an
// option type and verifies that nil status, address preservation, and
// underlying value dereferencing work as expected.
func (s) TestOption_Slice(t *testing.T) {
	val1 := []int{1, 2, 3}
	val2 := []int{4, 5, 6}

	var opt optional.Optional[[]int]
	if v, set := opt.Get(); set || v != nil {
		t.Fatalf("Zero-value Option[[]int] = (%v, %v); want (nil, false)", v, set)
	}

	optVal := optional.NewValue(val1)
	if v, set := optVal.Get(); !set || !slices.Equal(v, val1) {
		t.Fatalf("NewValue(%v) = (%v, %v); want (%v, true)", val1, v, set, val1)
	}

	opt.Set(val2)
	if v, set := opt.Get(); !set || !slices.Equal(v, val2) {
		t.Fatalf("Set(%v) = (%v, %v); want (%v, true)", &val2, v, set, &val2)
	}
}
