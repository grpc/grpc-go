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
	"errors"
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

// newNonBlockingReader returns a ReadyReader if the passed reader supports
// non-memory-pinning reads, else nil.
func newNonBlockingReader(r io.Reader) ReadyReader {
	if rr, ok := r.(ReadyReader); ok {
		return rr
	}
	sysConn, ok := r.(syscall.Conn)
	if !ok || !isRawConnSupported() {
		return nil
	}
	if raw, err := sysConn.SyscallConn(); err == nil {
		return &nonBlockingReader{raw: raw}
	}
	return nil
}

// NewReadyReader detects if [syscall.RawConn] is available for
// non-memory-pinning reads. If [syscall.RawConn] is unavailable, it falls back
// to using the simpler [net.Conn] interface for reads.
func NewReadyReader(conn net.Conn) ReadyReader {
	if r := newNonBlockingReader(conn); r != nil {
		return r
	}
	return &blockingReader{conn: conn}
}

// bufReadyReader implements buffering for an io.bufReadyReader object.
// A new bufReadyReader is created by calling [NewReader] or [newBufReadyReader];
// alternatively the zero value of a bufReadyReader may be used after calling [Reset]
// on it.
type bufReadyReader struct {
	buf       *[]byte
	pool      mem.BufferPool
	bufSize   int
	rd        ReadyReader // reader provided by the client
	r, w      int         // buf read and write positions
	err       error
	constPool constBufferPool // stored as a field to avoid heap allocations.
}

// newBufReadyReader returns a new [bufferedReadyReader] whose buffer has the
// specified size. If the argument.
func newBufReadyReader(rd ReadyReader, size int, pool mem.BufferPool) *bufReadyReader {
	r := &bufReadyReader{
		rd:      rd,
		pool:    pool,
		bufSize: size,
	}
	return r
}

var errNegativeRead = errors.New("bufio: reader returned negative count from Read")

func (b *bufReadyReader) readErr() error {
	err := b.err
	b.err = nil
	return err
}

func (b *bufReadyReader) buffered() int { return b.w - b.r }

// Read reads data into p.
// It returns the number of bytes read into p.
// The bytes are taken from at most one Read on the underlying [ReadyReader],
// hence n may be less than len(p).
// If the underlying [ReadyReader] can return a non-zero count with io.EOF,
// then this Read method can do so as well; see the [io.Reader] docs.
func (b *bufReadyReader) Read(p []byte) (n int, err error) {
	n = len(p)
	if n == 0 {
		if b.buffered() > 0 {
			return 0, nil
		}
		return 0, b.readErr()
	}
	if b.r == b.w {
		if b.err != nil {
			return 0, b.readErr()
		}
		if len(p) >= b.bufSize {
			// Large read, empty buffer.
			// Read directly into p to avoid copy.
			b.constPool.buffer = p
			_, n, err := b.rd.ReadOnReady(len(p), &b.constPool)
			b.err = err
			if n < 0 {
				panic(errNegativeRead)
			}
			return n, b.readErr()
		}
		// One read.
		b.r = 0
		b.w = 0
		b.buf, n, b.err = b.rd.ReadOnReady(b.bufSize, b.pool)
		if n < 0 {
			panic(errNegativeRead)
		}
		if n == 0 {
			return 0, b.readErr()
		}
		b.w += n
	}

	// copy as much as we can
	buf := *b.buf
	n = copy(p, buf[b.r:b.w])
	b.r += n
	if b.r == b.w {
		// Consumed entire buffer, release it.
		b.pool.Put(b.buf)
	}
	return n, nil
}

type constBufferPool struct {
	buffer []byte
}

func (p *constBufferPool) Get(int) *[]byte {
	return &p.buffer
}

func (p *constBufferPool) Put(*[]byte) {}
