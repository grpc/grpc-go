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

package client

type stringSet struct {
	m map[string]struct{}
}

func newStringSet() *stringSet {
	return &stringSet{m: make(map[string]struct{})}
}

func (s *stringSet) add(i string) {
	s.m[i] = struct{}{}
}

func (s *stringSet) remove(i string) {
	delete(s.m, i)
}

func (s *stringSet) len() int {
	return len(s.m)
}

func (s *stringSet) toSlice() (ret []string) {
	for i := range s.m {
		ret = append(ret, i)
	}
	return
}
