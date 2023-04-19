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
 */

package testutils

import (
	"net"
	"strconv"
	"testing"
)

// ParsePort returns the port from the given address string, as a unit32.
func ParsePort(t *testing.T, addr string) uint32 {
	t.Helper()

	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Invalid serving address: %v", err)
	}
	port, err := strconv.ParseUint(p, 10, 32)
	if err != nil {
		t.Fatalf("Invalid serving port: %v", err)
	}
	return uint32(port)
}
