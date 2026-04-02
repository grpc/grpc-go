/*
 *
 * Copyright 2026 gRPC authors.
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

package readyreader

import (
	"golang.org/x/sys/unix"
)

func isRawConnSupported() bool {
	return true
}

// sysRead uses the modern unix package for Unix-like systems.
func sysRead(fd uintptr, p []byte) (int, error) {
	return unix.Read(int(fd), p)
}

// wouldBlock checks standard Unix non-blocking errors.
func wouldBlock(err error) bool {
	return err == unix.EAGAIN || err == unix.EWOULDBLOCK
}
