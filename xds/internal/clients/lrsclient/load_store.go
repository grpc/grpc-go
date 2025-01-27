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

package lrsclient

// LoadStore keeps the loads for multiple clusters and services to be reported via
// LRS. It contains loads to report to one LRS server. Create multiple stores
// for multiple servers.
type LoadStore struct {
}

// NewLoadStore creates a new load store.
func NewLoadStore() *LoadStore {
	return &LoadStore{}
}
