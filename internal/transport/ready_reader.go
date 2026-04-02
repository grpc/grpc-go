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
	"io"
	"net"
	"syscall"

	"google.golang.org/grpc/mem"
)

// ReadyReader is an optional interface that can be implemented by [net.Conn]
// implementations to enable gRPC to perform non-memory-pinning reads.
type ReadyReader interface {
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
	// The following fields are stored as field to avoid heap allocations.
	state  readState
	doRead func(fd uintptr) bool
}

type readState struct {
	// Request params.
	bufSize int
	pool    mem.BufferPool

	// Response params.
	readError error
	bytesRead int
	buf       *[]byte
}

// newNonBlockingReader returns a ReadyReader if the passed reader supports
// non-memory-pinning reads, else nil.
func newNonBlockingReader(r io.Reader) ReadyReader {
	if rr, ok := r.(ReadyReader); ok {
		return rr
	}
	if !isRawConnSupported() {
		return nil
	}
	// We restrict the types before asserting syscall.Conn. The credentials
	// package may return a wrapper that implements syscall.Conn by embedding
	// both the raw connection and the encrypted connection. If the code
	// attempts to read directly from the raw syscall.RawConn, it would read
	// encrypted data.
	switch r.(type) {
	case *net.TCPConn, *net.UDPConn, *net.UnixConn, *net.IPConn:
	default:
		return nil
	}
	sysConn, ok := r.(syscall.Conn)
	if !ok {
		return nil
	}
	if raw, err := sysConn.SyscallConn(); err == nil {
		r := &nonBlockingReader{raw: raw}
		r.doRead = func(fd uintptr) bool {
			s := &r.state

			s.buf = s.pool.Get(s.bufSize)
			s.bytesRead, s.readError = sysRead(fd, *s.buf)

			if s.readError != nil {
				s.pool.Put(s.buf)
				s.buf = nil
			}
			return !wouldBlock(s.readError)
		}
		return r
	}
	return nil
}

func (c *nonBlockingReader) ReadOnReady(bufSize int, pool mem.BufferPool) (*[]byte, int, error) {
	c.state = readState{
		pool:    pool,
		bufSize: bufSize,
	}
	err := c.raw.Read(c.doRead)

	buf := c.state.buf
	c.state.buf = nil
	n := c.state.bytesRead
	readErr := c.state.readError

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
	reader io.Reader
}

func (c *blockingReader) ReadOnReady(bufSize int, pool mem.BufferPool) (*[]byte, int, error) {
	buf := pool.Get(bufSize)
	n, err := c.reader.Read(*buf)
	if err != nil {
		pool.Put(buf)
		return nil, 0, err
	}
	return buf, n, nil
}

// NewReadyReader detects if [syscall.RawConn] is available for
// non-memory-pinning reads. If [syscall.RawConn] is unavailable, it falls back
// to using the simpler [net.Conn] interface for reads.
func NewReadyReader(r io.Reader) ReadyReader {
	if r := newNonBlockingReader(r); r != nil {
		return r
	}
	return &blockingReader{reader: r}
}
