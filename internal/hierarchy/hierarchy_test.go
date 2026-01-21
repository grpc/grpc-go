/*
 *
 * Copyright 2020 gRPC authors.
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

package hierarchy

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

// Tests that if hierarchy is set to a wrong type (which should never happen),
// the endpoint is ignored.
func TestGroup_WrongType(t *testing.T) {
	eps := []resolver.Endpoint{
		SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "a0"}}}, []string{"a"}),
		SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "a1"}}}, []string{"a"}),
		{Addresses: []resolver.Address{{Addr: "b0"}}, Attributes: attributes.New(pathKey, "b")},
		{Addresses: []resolver.Address{{Addr: "b1"}}, Attributes: attributes.New(pathKey, 314)},
	}
	want := map[string][]resolver.Endpoint{
		"a": {
			SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "a0"}}}, nil),
			SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: "a1"}}}, nil),
		},
	}
	if got := Group(eps); !cmp.Equal(got, want, cmp.AllowUnexported(attributes.Attributes{})) {
		t.Errorf("Group() = %v, want %v", got, want)
		t.Errorf("diff: %v", cmp.Diff(got, want, cmp.AllowUnexported(attributes.Attributes{})))
	}
}
