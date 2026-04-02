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

// Package readyreader provides utilities to perform non-memory-pinning reads.
package readyreader

import (
	"io"
	"net"
	"syscall"

	"google.golang.org/grpc/mem"
)

// Reader is an optional interface that can be implemented by [net.Conn]
// implementations to enable gRPC to perform non-memory-pinning reads.
type Reader interface {
	// ReadOnReady waits for data to arrive, fetches a buffer, and performs a
	// read. When the underlying IO is readable, it allocates a buffer of size
	// bufSize from the pool and reads up to bufSize bytes into the buffer.
	//
	// It returns a pointer to the buffer so it can be returned to the pool
	// later, the number of bytes read, and an error.
	//
	// Callers should always process the n > 0 bytes returned before considering
	// the error. Doing so correctly handles I/O errors that happen after
	// reading some bytes, as well as both of the allowed EOF behaviors.
	ReadOnReady(bufSize int, pool mem.BufferPool) (b *[]byte, n int, err error)
}

// nonBlockingReader is optimized for non-memory-pinning reads using the RawConn
// interface.
type nonBlockingReader struct {
	raw syscall.RawConn
}

func (c *nonBlockingReader) ReadOnReady(bufSize int, pool mem.BufferPool) (buf *[]byte, n int, err error) {
	var readErr error
	err = c.raw.Read(func(fd uintptr) bool {
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
	if n == 0 {
		// syscall.Read doesn't consider a graceful socket closure to be an
		// error condition, but Go's io.Reader expects an EOF error.
		pool.Put(buf)
		return nil, 0, io.EOF
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

// New detects if [syscall.RawConn] is available for non-memory-pinning reads.
// If [syscall.RawConn] is unavailable, it falls back to using the simpler
// [net.Conn] interface for reads.
func New(conn net.Conn) Reader {
	sysConn, ok := conn.(syscall.Conn)
	if !ok || !isRawConnSupported() {
		return &blockingReader{conn: conn}
	}
	if raw, err := sysConn.SyscallConn(); err == nil {
		return &nonBlockingReader{raw: raw}
	}
	return &blockingReader{conn: conn}
}
