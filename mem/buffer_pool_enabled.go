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

func (p *tieredBufferPool) Get(size int) []byte {
	if size <= magic {
		return make([]byte, size)
	}
	return p.getPool(size).Get(size)
}

func (p *tieredBufferPool) Put(buf *[]byte) {
	c := cap(*buf)
	if c <= magic {
		return
	}
	p.getPool(cap(*buf)).Put(buf)
}
