//go:build buffer_pooling

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

package mem

// Ref invokes Buffer.Ref on each Buffer in the slice.
func (s BufferSlice) Ref() BufferSlice {
	out := make(BufferSlice, len(s))
	for i, b := range s {
		out[i] = b.Ref()
	}
	return out
}

// Free invokes Buffer.Free() on each Buffer in the slice.
func (s BufferSlice) Free() {
	for _, b := range s {
		b.Free()
	}
}
