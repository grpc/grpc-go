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

func TestFromEndpoint(t *testing.T) {
	tests := []struct {
		name string
		ep   resolver.Endpoint
		want []string
	}{
		{
			name: "not set",
			ep:   resolver.Endpoint{},
			want: nil,
		},
		{
			name: "set",
			ep: resolver.Endpoint{
				Attributes: attributes.New(pathKey, pathValue{"a", "b"}),
			},
			want: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromEndpoint(tt.ep); !cmp.Equal(got, tt.want) {
				t.Errorf("FromEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetInEndpoint(t *testing.T) {
	tests := []struct {
		name string
		ep   resolver.Endpoint
		path []string
	}{
		{
			name: "before is not set",
			ep:   resolver.Endpoint{},
			path: []string{"a", "b"},
		},
		{
			name: "before is set",
			ep: resolver.Endpoint{
				Attributes: attributes.New(pathKey, pathValue{"before", "a", "b"}),
			},
			path: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newEP := SetInEndpoint(tt.ep, tt.path)
			newPath := FromEndpoint(newEP)
			if !cmp.Equal(newPath, tt.path) {
				t.Errorf("path after SetInEndpoint() = %v, want %v", newPath, tt.path)
			}
		})
	}
}

func TestGroup(t *testing.T) {
	tests := []struct {
		name string
		eps  []resolver.Endpoint
		want map[string][]resolver.Endpoint
	}{
		{
			name: "all with hierarchy",
			eps: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "b0"}}, Attributes: attributes.New(pathKey, pathValue{"b"})},
				{Addresses: []resolver.Address{{Addr: "b1"}}, Attributes: attributes.New(pathKey, pathValue{"b"})},
			},
			want: map[string][]resolver.Endpoint{
				"a": {
					{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{})},
					{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{})},
				},
				"b": {
					{Addresses: []resolver.Address{{Addr: "b0"}}, Attributes: attributes.New(pathKey, pathValue{})},
					{Addresses: []resolver.Address{{Addr: "b1"}}, Attributes: attributes.New(pathKey, pathValue{})},
				},
			},
		},
		{
			// Endpoints without hierarchy are ignored.
			name: "without hierarchy",
			eps: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "b0"}}, Attributes: nil},
				{Addresses: []resolver.Address{{Addr: "b1"}}, Attributes: nil},
			},
			want: map[string][]resolver.Endpoint{
				"a": {
					{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{})},
					{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{})},
				},
			},
		},
		{
			// If hierarchy is set to a wrong type (which should never happen),
			// the endpoint is ignored.
			name: "wrong type",
			eps: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{"a"})},
				{Addresses: []resolver.Address{{Addr: "b0"}}, Attributes: attributes.New(pathKey, "b")},
				{Addresses: []resolver.Address{{Addr: "b1"}}, Attributes: attributes.New(pathKey, 314)},
			},
			want: map[string][]resolver.Endpoint{
				"a": {
					{Addresses: []resolver.Address{{Addr: "a0"}}, Attributes: attributes.New(pathKey, pathValue{})},
					{Addresses: []resolver.Address{{Addr: "a1"}}, Attributes: attributes.New(pathKey, pathValue{})},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Group(tt.eps); !cmp.Equal(got, tt.want, cmp.AllowUnexported(attributes.Attributes{})) {
				t.Errorf("Group() = %v, want %v", got, tt.want)
				t.Errorf("diff: %v", cmp.Diff(got, tt.want, cmp.AllowUnexported(attributes.Attributes{})))
			}
		})
	}
}

func TestGroupE2E(t *testing.T) {
	hierarchy := map[string]map[string][]string{
		"p0": {
			"wt0": {"addr0", "addr1"},
			"wt1": {"addr2", "addr3"},
		},
		"p1": {
			"wt10": {"addr10", "addr11"},
			"wt11": {"addr12", "addr13"},
		},
	}

	var epsWithHierarchy []resolver.Endpoint
	for p, wts := range hierarchy {
		path1 := pathValue{p}
		for wt, addrs := range wts {
			path2 := append(pathValue(nil), path1...)
			path2 = append(path2, wt)
			for _, addr := range addrs {
				a := resolver.Endpoint{
					Addresses:  []resolver.Address{{Addr: addr}},
					Attributes: attributes.New(pathKey, path2),
				}
				epsWithHierarchy = append(epsWithHierarchy, a)
			}
		}
	}

	gotHierarchy := make(map[string]map[string][]string)
	for p1, wts := range Group(epsWithHierarchy) {
		gotHierarchy[p1] = make(map[string][]string)
		for p2, eps := range Group(wts) {
			for _, ep := range eps {
				gotHierarchy[p1][p2] = append(gotHierarchy[p1][p2], ep.Addresses[0].Addr)
			}
		}
	}

	if !cmp.Equal(gotHierarchy, hierarchy) {
		t.Errorf("diff: %v", cmp.Diff(gotHierarchy, hierarchy))
	}
}
