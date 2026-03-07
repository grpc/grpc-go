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

package transport

import (
	"net"
	"syscall"

	"google.golang.org/grpc/mem"
)

// ReadyReader is an optional interface that can be implemented by net.Conn
// implementations to enable gRPC to perform non-memory-pinning reads.
type ReadyReader interface {
	// ReadOnReady waits for data to arrive, fetches a buffer, and performs a
	// read. It returns a pointer to the buffer so you can return it to the pool
	// later.
	ReadOnReady(bufSize int, pool mem.BufferPool) (*[]byte, int, error)
}

// nonBlockingReader is optimized for non-memory-pinning reads using the RawConn
// interface.
type nonBlockingReader struct {
	raw syscall.RawConn
}

func (c *nonBlockingReader) ReadOnReady(bufSize int, pool mem.BufferPool) (*[]byte, int, error) {
	var n int
	var readErr error
	var buf *[]byte

	err := c.raw.Read(func(fd uintptr) bool {
		buf = pool.Get(bufSize)
		n, readErr = sysRead(fd, *buf)
		if readErr != nil {
			pool.Put(buf)
			buf = nil
		}

		if wouldBlock(readErr) {
			return false // Wait for readiness
		}
		return true // Done
	})

	if err != nil {
		if buf != nil {
			pool.Put(buf)
		}
		return nil, 0, err
	}
	if readErr != nil {
		// buffer is already released in the callback.
		return nil, 0, readErr
	}
	return buf, n, nil
}

type blockingReader struct {
	conn net.Conn
}

func (c *blockingReader) ReadOnReady(bufSize int, pool mem.BufferPool) (*[]byte, int, error) {
	buf := pool.Get(bufSize)
	n, err := c.conn.Read(*buf)
	if err != nil {
		pool.Put(buf)
		return nil, 0, err
	}
	return buf, n, nil
}

// NewReadyReader detects if RawConn is available for non-memory-pinning reads.
// If RawConn is unavailable, it falls back to using the simpler net.Conn
// interface for reads.
func NewReadyReader(conn net.Conn) ReadyReader {
	sysConn, isSyscall := conn.(syscall.Conn)
	if !isSyscall || !isRawConnSupported() {
		return &blockingReader{
			conn: conn,
		}
	}

	if raw, err := sysConn.SyscallConn(); err == nil {
		return &nonBlockingReader{
			raw: raw,
		}
	}

	return &blockingReader{
		conn: conn,
	}
}
