/*
 *
 * Copyright 2025 gRPC authors.
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

package conn

import (
	"errors"

	"golang.org/x/sys/unix"
)

// setRcvlowat updates SO_RCVLOWAT to reduce CPU usage.
func (p *conn) setRcvlowat(length int) error {
	if p.rawConn == nil {
		return nil
	}

	const (
		rcvlowatMax = 16 * 1024 * 1024
		rcvlowatMin = 32 * 1024
		rcvlowatGap = 16 * 1024
	)

	remaining := min(cap(p.protected), length, rcvlowatMax)

	// Small SO_RCVLOWAT values don't actually save CPU.
	if remaining < rcvlowatMin {
		remaining = 0
	}

	// Allow for a small gap, which can wake us up a tiny bit early. This
	// helps with latency, as bytes can arrive between wakeup and the
	// ensuing read.
	if remaining > 0 {
		remaining -= rcvlowatGap
	}

	// Don't hold up the socket once we've hit our threshold.
	if len(p.protected) > remaining {
		remaining = 0
	}

	// Don't enable SO_RCVLOWAT if it's not useful.
	if p.rcvlowat <= 1 && remaining <= 1 {
		return nil
	}

	// Don't make a syscall if nothing changed.
	if p.rcvlowat == remaining {
		return nil
	}

	// Make the actual setsockopt call.
	var sockoptErr error
	err := p.rawConn.Control(func(fd uintptr) {
		sockoptErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVLOWAT, p.rcvlowat)
	})
	if err != nil || sockoptErr != nil {
		return errors.Join(err, sockoptErr)
	}

	p.rcvlowat = remaining
	return nil
}
