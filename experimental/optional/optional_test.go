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
	"testing"

	"google.golang.org/grpc/experimental/optional"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testStruct struct {
	Name string
	Age  int
}

// TestOption_Int tests the scenario of using integer optional values and
// verifies that default value, constructors, and mutation methods work as
// expected for primitive integers.
func (s) TestOption_Int(t *testing.T) {
	var opt optional.Option[int]
	// Test unset value.
	if v, set := opt.Value(); set || v != 0 {
		t.Fatalf("Zero-value Option[int] = (%v, %v); want (0, false)", v, set)
	}

	// Test that New() function also returns an unset optional value.
	optNew := optional.New[int]()
	if v, set := optNew.Value(); set || v != 0 {
		t.Fatalf("New[int]() = (%v, %v); want (0, false)", v, set)
	}

	optVal := optional.NewValue(42)
	if v, set := optVal.Value(); !set || v != 42 {
		t.Fatalf("NewValue(42) = (%v, %v); want (42, true)", v, set)
	}

	opt = opt.WithValue(100)
	if v, set := opt.Value(); !set || v != 100 {
		t.Fatalf("WithValue(100) = (%v, %v); want (100, true)", v, set)
	}

	opt = opt.Clear()
	if v, set := opt.Value(); set || v != 0 {
		t.Fatalf("Clear() = (%v, %v); want (0, false)", v, set)
	}
}

// TestOption_String tests the scenario of using string optional values and
// verifies that default value, constructors, and mutation methods work as
// expected for text strings.
func (s) TestOption_String(t *testing.T) {
	var opt optional.Option[string]
	// Test unset value.
	if v, set := opt.Value(); set || v != "" {
		t.Fatalf("Zero-value Option[string] = (%q, %v); want (%q, false)", v, set, "")
	}

	// Test that New() function also returns an unset optional value.
	optNew := optional.New[string]()
	if v, set := optNew.Value(); set || v != "" {
		t.Fatalf("New Option[string] = (%q, %v); want (%q, false)", v, set, "")
	}

	wantString := "test-string"
	optVal := optional.NewValue(wantString)
	if v, set := optVal.Value(); !set || v != wantString {
		t.Fatalf("NewValue(%q) = (%q, %v); want (%q, true)", wantString, v, set, wantString)
	}

	wantStringNew := "world"
	opt = opt.WithValue(wantStringNew)
	if v, set := opt.Value(); !set || v != wantStringNew {
		t.Fatalf("WithValue(%q) = (%q, %v); want (%q, true)", wantStringNew, v, set, wantStringNew)
	}

	opt = opt.Clear()
	if v, set := opt.Value(); set || v != "" {
		t.Fatalf("Clear() = (%q, %v); want (%q, false)", v, set, "")
	}
}

// TestOption_Struct tests the scenario of using a custom struct type inside an
// option type and verifies that custom struct field values are preserved,
// modified, and cleared correctly.
func (s) TestOption_Struct(t *testing.T) {
	val1 := testStruct{Name: "Alice", Age: 30}
	val2 := testStruct{Name: "Bob", Age: 40}

	var opt optional.Option[testStruct]
	if v, set := opt.Value(); set || v != (testStruct{}) {
		t.Fatalf("Zero-value Option[struct] = (%v, %v); want (empty, false)", v, set)
	}

	optVal := optional.NewValue(val1)
	if v, set := optVal.Value(); !set || v != val1 {
		t.Fatalf("NewValue(val1) = (%v, %v); want (%v, true)", v, set, val1)
	}

	opt = opt.WithValue(val2)
	if v, set := opt.Value(); !set || v != val2 {
		t.Fatalf("WithValue(val2) = (%v, %v); want (%v, true)", v, set, val2)
	}

	opt = opt.Clear()
	if v, set := opt.Value(); set || v != (testStruct{}) {
		t.Fatalf("Clear() = (%v, %v); want (empty, false)", v, set)
	}
}

// TestOption_Pointer tests the scenario of using a pointer type inside an
// option type and verifies that nil status, address preservation, and
// underlying value dereferencing work as expected.
func (s) TestOption_Pointer(t *testing.T) {
	val1 := 42
	val2 := 100

	var opt optional.Option[*int]
	if v, set := opt.Value(); set || v != nil {
		t.Fatalf("Zero-value Option[*int] = (%v, %v); want (nil, false)", v, set)
	}

	optVal := optional.NewValue(&val1)
	if v, set := optVal.Value(); !set || v != &val1 || *v != val1 {
		t.Fatalf("NewValue(%v) = (%v, %v); want (%v, true)", &val1, v, set, &val1)
	}

	opt = opt.WithValue(&val2)
	if v, set := opt.Value(); !set || v != &val2 || *v != val2 {
		t.Fatalf("WithValue(%v) = (%v, %v); want (%v, true)", &val2, v, set, &val2)
	}

	opt = opt.Clear()
	if v, set := opt.Value(); set || v != nil {
		t.Fatalf("Clear() = (%v, %v); want (nil, false)", v, set)
	}
}
