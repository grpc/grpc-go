/*
 *
 * Copyright 2024 gRPC authors.
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

package bootstrap

// AppendIfNotPresent adds elems to a slice while ignoring duplicates
func AppendIfNotPresent[E comparable](slice []E, elems ...E) []E {
	presented := make(map[E]struct{})

	for _, s := range slice {
		presented[s] = struct{}{}
	}

	for _, e := range elems {
		if _, ok := presented[e]; !ok {
			slice = append(slice, e)
			presented[e] = struct{}{}
		}
	}
	return slice
}
