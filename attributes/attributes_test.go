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

	"google.golang.org/grpc/attributes"
)
// New ...
func TestNew(t *testing.T) {
	k1 := 1
	v1 := "first"
	attr := New(k1, v1)

	ret1, ok1 := attr.Value(k1).(string)
	if !ok1 || v1 != ret1 {
		t.Fatalf("attributes.Value error: want:%v ret:%v", v1, ret1)
	}

	k2 := "2"
	v2 := 2
	attr = attr.WithValues(k2, v2)
	ret2, ok2 := attr.Value(k2).(int)
	if !ok2 || v2 != ret2 {
		t.Fatalf("attributes.WithValues error: want:%v ret:%v", v2, ret2)
	}
}
func ExampleAttributes() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, 1, keyTwo{}, "two")
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: two
}

func ExampleAttributes_WithValues() {
	type keyOne struct{}
	type keyTwo struct{}
	a := attributes.New(keyOne{}, 1)
	a = a.WithValues(keyTwo{}, "two")
	fmt.Println("Key one:", a.Value(keyOne{}))
	fmt.Println("Key two:", a.Value(keyTwo{}))
	// Output:
	// Key one: 1
	// Key two: two
}
