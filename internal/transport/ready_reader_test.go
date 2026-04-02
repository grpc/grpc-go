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
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/mem"
)

// TestReadyReader_NonRawConn verifies that ReadOnReady correctly reads data
// from a net.Conn that doesn't support non-memory-pinning reads.
func (s) TestReadyReader_NonRawConn(t *testing.T) {
	data := []byte("hello world")
	reader, writer := net.Pipe()
	go writer.Write(data)

	pool := mem.DefaultBufferPool()
	readyReader := NewReadyReader(reader)

	bufHandle, n, err := readyReader.ReadOnReady(1024, pool)
	if err != nil {
		t.Fatalf("ReadOnReady() failed: %v", err)
	}
	defer pool.Put(bufHandle)

	if n != len(data) {
		t.Errorf("n = %d; want %d", n, len(data))
	}
	if !bytes.Equal((*bufHandle)[:n], data) {
		t.Errorf("Read data = %s; want %s", string((*bufHandle)[:n]), string(data))
	}
}

func (s) TestReadyReader_EOF(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer ln.Close()

	data := []byte("hello")
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if _, err := conn.Write(data); err != nil {
			t.Errorf("Failed to write data: %v", err)
			return
		}
		conn.Close()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial failed: %v", err)
	}
	defer conn.Close()

	pool := mem.DefaultBufferPool()
	rr := NewReadyReader(conn)
	res, _, err := rr.ReadOnReady(len(data), pool)
	if err != nil {
		t.Errorf("Failed to read: %v", err)
		return
	}

	if !bytes.Equal(*res, data) {
		t.Errorf("Read data = %s; want %s", string(*res), string(data))
	}
	pool.Put(res)

	// Since the server closes the TCP connection, the next read should return
	// an io.EOF.
	res, _, err = rr.ReadOnReady(len(data), pool)
	if err != io.EOF {
		t.Errorf("Read after server connection close returned err = %v; want io.EOF", err)
	}
	if res != nil {
		t.Error("ReadOnReady() returned non-nil buffer.")
	}
}

func (s) TestReadyReader_TCP_Blocking(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), defaultTestTimeout)
	defer cancel()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer ln.Close()

	data := []byte("hello tcp delayed")
	connCh := make(chan net.Conn)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		connCh <- conn
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial failed: %v", err)
	}
	defer conn.Close()

	serverConn := <-connCh
	defer serverConn.Close()

	pool := newTrackingPool(mem.DefaultBufferPool())
	ac := NewReadyReader(conn)

	resCh := make(chan []byte)
	const readBufSize = 1024

	go func() {
		bufHandle, n, err := ac.ReadOnReady(readBufSize, pool)
		if err != nil {
			t.Errorf("Failed to read: %v", err)
			return
		}
		defer pool.Put(bufHandle)
		resCh <- (*bufHandle)[:n]
	}()

	// Verify that no read buffer is allocated for a short while. If it is
	// allocated (e.g. for a probe), it must be returned immediately.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if n, err := pool.requestChan.Receive(sCtx); err == nil {
		if n != readBufSize {
			t.Fatalf("Unexpected request buffer size: got %d, want %d", n, readBufSize)
		}
		if n, err := pool.putChan.Receive(ctx); err != nil {
			t.Fatal("Read buffer allocated and NOT returned while idle.")
		} else if n != readBufSize {
			t.Fatalf("Unexpected returned buffer size: got %d, want %d", n, readBufSize)
		}
	}

	// Write data to unblock.
	serverConn.Write(data)

	if n, err := pool.requestChan.Receive(ctx); err != nil {
		t.Fatal("Context timed out while waiting for a read buffer to be allocated.")
	} else if n != readBufSize {
		t.Fatalf("Unexpected request buffer size: got %d, want %d", n, readBufSize)
	}

	var res []byte
	select {
	case res = <-resCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for read to complete.")
	}
	if !bytes.Equal(res, data) {
		t.Errorf("Read data = %s; want %s", string(res), string(data))
	}
}

// trackingBufferPool wraps a mem.BufferPool and provides channels to track
// when buffers are requested and returned, useful for verifying allocation
// behavior in tests.
type trackingBufferPool struct {
	mem.BufferPool
	requestChan testutils.Channel
	putChan     testutils.Channel
}

func newTrackingPool(pool mem.BufferPool) *trackingBufferPool {
	return &trackingBufferPool{
		BufferPool:  pool,
		requestChan: *testutils.NewChannelWithSize(1),
		putChan:     *testutils.NewChannelWithSize(1),
	}
}

func (p *trackingBufferPool) Get(size int) *[]byte {
	p.requestChan.Replace(size)
	return p.BufferPool.Get(size)
}

func (p *trackingBufferPool) Put(b *[]byte) {
	p.putChan.Replace(len(*b))
	p.BufferPool.Put(b)
}
