/*
 *
 * Copyright 2019 gRPC authors.
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

package attributes_test

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/attributes"
)

type intVal int

func (i intVal) IsEqual(o attributes.Value) bool {
	oi, ok := o.(intVal)
	return ok && i == oi
}

type stringVal string

func (s stringVal) IsEqual(o attributes.Value) bool {
	os, ok := o.(stringVal)
	return ok && s == os
}

func ExampleAttributes() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, intVal(1)).WithValue(keyTwo{}, stringVal("two"))
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: two
}

func ExampleAttributes_WithValue() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, intVal(1))
	a = a.WithValue(keyTwo{}, stringVal("two"))
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: two
}

// Test that two attributes with the same content are Equal.
func TestIsEqual(t *testing.T) {
	type keyOne struct{}
	type keyTwo struct{}
	a1 := attributes.New(keyOne{}, intVal(1)).WithValue(keyTwo{}, stringVal("two"))
	a2 := attributes.New(keyOne{}, intVal(1)).WithValue(keyTwo{}, stringVal("two"))
	if !a1.IsEqual(a2) {
		t.Fatalf("%+v.Equals(%+v) = false; want true", a1, a2)
	}
	if !a2.IsEqual(a1) {
		t.Fatalf("%+v.Equals(%+v) = false; want true", a2, a1)
	}
}

// Test that two attributes with different content are not Equal.
func TestNotIsEqual(t *testing.T) {
	type keyOne struct{}
	type keyTwo struct{}
	a1 := attributes.New(keyOne{}, intVal(1)).WithValue(keyTwo{}, stringVal("two"))
	a2 := attributes.New(keyOne{}, intVal(2)).WithValue(keyTwo{}, stringVal("two"))
	if a1.IsEqual(a2) {
		t.Fatalf("%+v.Equals(%+v) = true; want false", a1, a2)
	}
	if a2.IsEqual(a1) {
		t.Fatalf("%+v.Equals(%+v) = true; want false", a2, a1)
	}
}
