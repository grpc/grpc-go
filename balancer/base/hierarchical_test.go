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

package base

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

func TestRetrieveHierarchicalPath(t *testing.T) {
	tests := []struct {
		name string
		addr resolver.Address
		want []string
	}{
		{
			name: "not set",
			addr: resolver.Address{},
			want: nil,
		},
		{
			name: "set",
			addr: resolver.Address{
				Attributes: attributes.New(hierarchicalPathKey, []string{"a", "b"}),
			},
			want: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RetrieveHierarchicalPath(tt.addr); !cmp.Equal(got, tt.want) {
				t.Errorf("RetrieveHierarchicalPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOverrideHierarchicalPath(t *testing.T) {
	tests := []struct {
		name string
		addr resolver.Address
		path []string
	}{
		{
			name: "before is not set",
			addr: resolver.Address{},
			path: []string{"a", "b"},
		},
		{
			name: "before is set",
			addr: resolver.Address{
				Attributes: attributes.New(hierarchicalPathKey, []string{"before", "a", "b"}),
			},
			path: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newAddr := OverrideHierarchicalPath(tt.addr, tt.path)
			newPath := RetrieveHierarchicalPath(newAddr)
			if !cmp.Equal(newPath, tt.path) {
				t.Errorf("path after OverrideHierarchicalPath() = %v, want %v", newPath, tt.path)
			}
		})
	}
}

func TestSplitHierarchicalAddresses(t *testing.T) {
	tests := []struct {
		name  string
		addrs []resolver.Address
		want  map[string][]resolver.Address
	}{
		{
			name: "all with hierarchy",
			addrs: []resolver.Address{
				{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "b0", Attributes: attributes.New(hierarchicalPathKey, []string{"b"})},
				{Addr: "b1", Attributes: attributes.New(hierarchicalPathKey, []string{"b"})},
			},
			want: map[string][]resolver.Address{
				"a": []resolver.Address{
					{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{})},
					{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{})},
				},
				"b": []resolver.Address{
					{Addr: "b0", Attributes: attributes.New(hierarchicalPathKey, []string{})},
					{Addr: "b1", Attributes: attributes.New(hierarchicalPathKey, []string{})},
				},
			},
		},
		{
			// Addresses without hierarchy are ignored.
			name: "without hierarchy",
			addrs: []resolver.Address{
				{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "b0", Attributes: nil},
				{Addr: "b1", Attributes: nil},
			},
			want: map[string][]resolver.Address{
				"a": []resolver.Address{
					{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{})},
					{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{})},
				},
			},
		},
		{
			// If hierarchy is set to a wrong type (which should never happen),
			// the address is ignored.
			name: "wrong type",
			addrs: []resolver.Address{
				{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{"a"})},
				{Addr: "b0", Attributes: attributes.New(hierarchicalPathKey, "b")},
				{Addr: "b1", Attributes: attributes.New(hierarchicalPathKey, 314)},
			},
			want: map[string][]resolver.Address{
				"a": []resolver.Address{
					{Addr: "a0", Attributes: attributes.New(hierarchicalPathKey, []string{})},
					{Addr: "a1", Attributes: attributes.New(hierarchicalPathKey, []string{})},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitHierarchicalAddresses(tt.addrs); !cmp.Equal(got, tt.want, cmp.AllowUnexported(attributes.Attributes{})) {
				t.Errorf("SplitHierarchicalAddresses() = %v, want %v", got, tt.want)
				t.Errorf("diff: %v", cmp.Diff(got, tt.want, cmp.AllowUnexported(attributes.Attributes{})))
			}
		})
	}
}

func TestSplitHierarchicalAddressesE2E(t *testing.T) {
	hierarchy := map[string]map[string][]string{
		"p0": map[string][]string{
			"wt0": []string{"addr0", "addr1"},
			"wt1": []string{"addr2", "addr3"},
		},
		"p1": map[string][]string{
			"wt10": []string{"addr10", "addr11"},
			"wt11": []string{"addr12", "addr13"},
		},
	}

	var addrsWithHierarchy []resolver.Address
	for p, wts := range hierarchy {
		path1 := []string{p}
		for wt, addrs := range wts {
			path2 := append([]string(nil), path1...)
			path2 = append(path2, wt)
			for _, addr := range addrs {
				a := resolver.Address{
					Addr:       addr,
					Attributes: attributes.New(hierarchicalPathKey, path2),
				}
				addrsWithHierarchy = append(addrsWithHierarchy, a)
			}
		}
	}

	gotHierarchy := make(map[string]map[string][]string)
	for p1, wts := range SplitHierarchicalAddresses(addrsWithHierarchy) {
		gotHierarchy[p1] = make(map[string][]string)
		for p2, addrs := range SplitHierarchicalAddresses(wts) {
			for _, addr := range addrs {
				gotHierarchy[p1][p2] = append(gotHierarchy[p1][p2], addr.Addr)
			}
		}
	}

	if !cmp.Equal(gotHierarchy, hierarchy) {
		t.Errorf("diff: %v", cmp.Diff(gotHierarchy, hierarchy))
	}
}
