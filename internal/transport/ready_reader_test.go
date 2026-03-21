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
	"net"
	"testing"

	"google.golang.org/grpc/mem"
)

func (s) TestReadyReader_Fallback(t *testing.T) {
	data := []byte("hello world")
	mc := &testConn{
		in: bytes.NewBuffer(data),
	}
	pool := mem.DefaultBufferPool()
	ac := NewReadyReader(mc)

	bufHandle, n, err := ac.ReadOnReady(1024, pool)
	if err != nil {
		t.Fatalf("readInitial() failed: %v", err)
	}
	defer pool.Put(bufHandle)

	if n != len(data) {
		t.Errorf("n = %d; want %d", n, len(data))
	}
	if !bytes.Equal((*bufHandle)[:n], data) {
		t.Errorf("read data = %s; want %s", string((*bufHandle)[:n]), string(data))
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

	go func() {
		bufHandle, n, err := ac.ReadOnReady(1024, pool)
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
	select {
	case <-pool.requestChan:
		select {
		case <-pool.putChan:
		case <-sCtx.Done():
			t.Fatal("Read buffer allocated and NOT returned while idle.")
		}
	case <-sCtx.Done():
	}

	// Write data to unblock.
	close(pool.doneCh)
	serverConn.Write(data)

	var res []byte
	select {
	case res = <-resCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for read to complete.")
	}
	if !bytes.Equal(res, data) {
		t.Errorf("read data = %s; want %s", string(res), string(data))
	}
}

type trackingBufferPool struct {
	mem.BufferPool
	doneCh      chan struct{}
	requestChan chan int
	putChan     chan int
}

func newTrackingPool(pool mem.BufferPool) *trackingBufferPool {
	return &trackingBufferPool{
		BufferPool:  pool,
		requestChan: make(chan int),
		putChan:     make(chan int),
		doneCh:      make(chan struct{}),
	}
}

func (p *trackingBufferPool) Get(size int) *[]byte {
	select {
	case p.requestChan <- size:
	case <-p.doneCh:
	}
	return p.BufferPool.Get(size)
}

func (p *trackingBufferPool) Put(b *[]byte) {
	select {
	case p.putChan <- len(*b):
	case <-p.doneCh:
	}
	p.BufferPool.Put(b)
}

// testConn mimics a net.Conn to the peer.
type testConn struct {
	net.Conn
	in  *bytes.Buffer
	out *bytes.Buffer
}

func (c *testConn) Read(b []byte) (n int, err error) {
	return c.in.Read(b)
}

func (c *testConn) Write(b []byte) (n int, err error) {
	return c.out.Write(b)
}

func (c *testConn) Close() error {
	return nil
}
