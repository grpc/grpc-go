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
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

type hierarchicalPathKeyType string

const hierarchicalPathKey = hierarchicalPathKeyType("grpc.internal.address.hierarchical_path")

// RetrieveHierarchicalPath returns the hierarchical path of addr.
func RetrieveHierarchicalPath(addr resolver.Address) []string {
	attrs := addr.Attributes
	if attrs == nil {
		return nil
	}
	path, ok := attrs.Value(hierarchicalPathKey).([]string)
	if !ok {
		return nil
	}
	return path
}

// OverrideHierarchicalPath overrides the hierarchical path in addr with path.
func OverrideHierarchicalPath(addr resolver.Address, path []string) resolver.Address {
	if addr.Attributes == nil {
		addr.Attributes = attributes.New(hierarchicalPathKey, path)
		return addr
	}
	addr.Attributes = addr.Attributes.WithValues(hierarchicalPathKey, path)
	return addr
}

// SplitHierarchicalAddresses splits a slice of addresses into groups based on
// the first hierarchy path. The first hierarchy path will be removed from the
// result.
//
// Input:
// [
//   {addr0, path: [p0, wt0]}
//   {addr1, path: [p0, wt1]}
//   {addr2, path: [p1, wt2]}
//   {addr3, path: [p1, wt3]}
// ]
//
// Addresses will be split into p0/p1, and the p0/p1 will be removed from the
// path.
//
// Output:
// {
//   p0: [
//     {addr0, path: [wt0]},
//     {addr1, path: [wt1]},
//   ],
//   p1: [
//     {addr2, path: [wt2]},
//     {addr3, path: [wt3]},
//   ],
// }
func SplitHierarchicalAddresses(addrs []resolver.Address) map[string][]resolver.Address {
	ret := make(map[string][]resolver.Address)
	for _, addr := range addrs {
		oldPath := RetrieveHierarchicalPath(addr)
		if len(oldPath) == 0 {
			// When hierarchical path is not set, or has no path in it, skip the
			// address. Another option is to return this address with path "",
			// this shouldn't conflict with anything because "" isn't a valid
			// path.
			continue
		}
		curPath := oldPath[0]
		newPath := oldPath[1:]
		newAddr := OverrideHierarchicalPath(addr, newPath)
		ret[curPath] = append(ret[curPath], newAddr)
	}
	return ret
}
