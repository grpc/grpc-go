/*
 *
 * Copyright 2023 gRPC authors.
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

package grpc

import "testing"

func (s) TestSharedBufferPool(t *testing.T) {
	pools := []SharedBufferPool{
		nopBufferPool{},
		NewSharedBufferPool(),
	}

	lengths := []int{
		level4PoolMaxSize + 1,
		level4PoolMaxSize,
		level3PoolMaxSize,
		level2PoolMaxSize,
		level1PoolMaxSize,
		level0PoolMaxSize,
	}

	for _, p := range pools {
		for _, l := range lengths {
			bs := p.Get(l)
			if len(bs) != l {
				t.Fatalf("Expected buffer of length %d, got %d", l, len(bs))
			}

			p.Put(&bs)
		}
	}
}
