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

type watchInfoSet struct {
	m map[*watchInfo]struct{}
}

func newWatchInfoSet() *watchInfoSet {
	return &watchInfoSet{m: make(map[*watchInfo]struct{})}
}

func (s *watchInfoSet) add(i *watchInfo) {
	s.m[i] = struct{}{}
}

func (s *watchInfoSet) has(i *watchInfo) bool {
	_, ok := s.m[i]
	return ok
}

func (s *watchInfoSet) forEach(f func(*watchInfo)) {
	for v := range s.m {
		f(v)
	}
}

func (s *watchInfoSet) remove(i *watchInfo) {
	delete(s.m, i)
}

func (s *watchInfoSet) len() int {
	return len(s.m)
}
