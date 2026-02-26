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

type stringVal struct {
	s string
}

func (s stringVal) Equal(o any) bool {
	os, ok := o.(stringVal)
	return ok && s.s == os.s
}

type stringerVal struct {
	s string
}

func (s stringerVal) String() string {
	return s.s
}

func ExampleAttributes() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"})
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: {two}
}

func ExampleAttributes_WithValue() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, 1)
	a = a.WithValue(keyTwo{}, stringVal{s: "two"})
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: {two}
}

func ExampleAttributes_String() {
	type key struct{}
	var typedNil *stringerVal
	a1 := attributes.New(key{}, typedNil)            // typed nil implements [fmt.Stringer]
	a2 := attributes.New(key{}, (*stringerVal)(nil)) // typed nil implements [fmt.Stringer]
	a3 := attributes.New(key{}, (*stringVal)(nil))   // typed nil not implements [fmt.Stringer]
	a4 := attributes.New(key{}, nil)                 // untyped nil
	a5 := attributes.New(key{}, 1)
	a6 := attributes.New(key{}, stringerVal{s: "two"})
	a7 := attributes.New(key{}, stringVal{s: "two"})
	a8 := attributes.New(1, true)
	fmt.Println("a1:", a1.String())
	fmt.Println("a2:", a2.String())
	fmt.Println("a3:", a3.String())
	fmt.Println("a4:", a4.String())
	fmt.Println("a5:", a5.String())
	fmt.Println("a6:", a6.String())
	fmt.Println("a7:", a7.String())
	fmt.Println("a8:", a8.String())
	// Output:
	// a1: {"<%!p(attributes_test.key={})>": "<nil>" }
	// a2: {"<%!p(attributes_test.key={})>": "<nil>" }
	// a3: {"<%!p(attributes_test.key={})>": "<0x0>" }
	// a4: {"<%!p(attributes_test.key={})>": "<%!p(<nil>)>" }
	// a5: {"<%!p(attributes_test.key={})>": "<%!p(int=1)>" }
	// a6: {"<%!p(attributes_test.key={})>": "two" }
	// a7: {"<%!p(attributes_test.key={})>": "<%!p(attributes_test.stringVal={two})>" }
	// a8: {"<%!p(int=1)>": "<%!p(bool=true)>" }
}

// Test that two attributes with different content are not Equal.
func TestEqual(t *testing.T) {
	type keyOne struct{}
	type keyTwo struct{}
	tests := []struct {
		name string
		a    *attributes.Attributes
		b    *attributes.Attributes
		want bool
	}{
		{
			name: "different_first_value",
			a:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			b:    attributes.New(keyOne{}, 2).WithValue(keyTwo{}, stringVal{s: "two"}),
			want: false,
		},
		{
			name: "different_second_value",
			a:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "one"}),
			b:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			want: false,
		},
		{
			name: "same",
			a:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			b:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			want: true,
		},
		{
			name: "subset",
			a:    attributes.New(keyOne{}, 1),
			b:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			want: false,
		},
		{
			name: "superset",
			a:    attributes.New(keyOne{}, 1).WithValue(keyTwo{}, stringVal{s: "two"}),
			b:    attributes.New(keyTwo{}, stringVal{s: "two"}),
			want: false,
		},
		{
			name: "a_nil",
			a:    nil,
			b:    attributes.New(keyOne{}, 1),
			want: false,
		},
		{
			name: "b_nil",
			a:    attributes.New(keyOne{}, 1),
			b:    nil,
			want: false,
		},
		{
			name: "both_nil",
			a:    nil,
			b:    nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("%+v.Equal(%+v) = %v; want %v", tt.a, tt.b, got, tt.want)
			}
			// The Equal function should be symmetric, i.e. a.Equals(b) ==
			// b.Equals(a).
			if got := tt.b.Equal(tt.a); got != tt.want {
				t.Errorf("%+v.Equal(%+v) = %v; want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func BenchmarkWithValue(b *testing.B) {
	keys := make([]any, 10)
	for i := range 10 {
		keys[i] = i
	}
	b.ReportAllocs()

	for b.Loop() {
		// 50 endpoints
		for range 50 {
			a := attributes.New(keys[0], keys[0])
			// 10 attributes each.
			for j := 1; j < 10; j++ {
				a = a.WithValue(keys[j], keys[j])
			}
		}
	}
}
