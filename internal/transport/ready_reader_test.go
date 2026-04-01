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
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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

func (s) TestReadyReader_TCP_Blocking(t *testing.T) {
	tests := []struct {
		name string
		read func(conn net.Conn, pool *trackingBufferPool, readBufSize int) ([]byte, error)
	}{
		{
			name: "ReadyReader",
			read: func(conn net.Conn, pool *trackingBufferPool, readBufSize int) ([]byte, error) {
				rr := NewReadyReader(conn)
				bufHandle, n, err := rr.ReadOnReady(readBufSize, pool)
				if err != nil {
					return nil, err
				}
				defer pool.Put(bufHandle)
				res := make([]byte, n)
				copy(res, (*bufHandle)[:n])
				return res, nil
			},
		},
		{
			name: "BufReadyReader",
			read: func(conn net.Conn, pool *trackingBufferPool, readBufSize int) ([]byte, error) {
				rr := newNonBlockingReader(conn)
				bufRR := newBufReadyReader(rr, readBufSize, pool)
				buf := make([]byte, 100)
				n, err := bufRR.Read(buf)
				if err != nil {
					return nil, err
				}
				return buf[:n], nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
			const readBufSize = 1024

			resCh := make(chan []byte)

			go func() {
				res, err := tc.read(conn, pool, readBufSize)
				if err != nil {
					t.Errorf("Failed to read: %v", err)
					return
				}
				resCh <- res
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
		})
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

// Call Read to accumulate the text of a file
func reads(buf *bufReadyReader, m int) string {
	var b [1000]byte
	nb := 0
	for {
		n, err := buf.Read(b[nb : nb+m])
		nb += n
		if err == io.EOF {
			break
		}
	}
	return string(b[0:nb])
}

type bufReader struct {
	name string
	fn   func(*bufReadyReader) string
}

var bufreaders = []bufReader{
	{"1", func(b *bufReadyReader) string { return reads(b, 1) }},
	{"2", func(b *bufReadyReader) string { return reads(b, 2) }},
	{"3", func(b *bufReadyReader) string { return reads(b, 3) }},
	{"4", func(b *bufReadyReader) string { return reads(b, 4) }},
	{"5", func(b *bufReadyReader) string { return reads(b, 5) }},
	{"7", func(b *bufReadyReader) string { return reads(b, 7) }},
}

const minReadBufferSize = 16

var bufsizes = []int{
	0, minReadBufferSize, 23, 32, 46, 64, 93, 128, 1024, 4096,
}

func (s) TestBufReader(t *testing.T) {
	var texts [31]string
	str := ""
	all := ""
	for i := range len(texts) - 1 {
		texts[i] = str + "\n"
		all += texts[i]
		str += string(rune(i%26 + 'a'))
	}
	texts[len(texts)-1] = all

	for _, text := range texts {
		for _, bufreader := range bufreaders {
			for _, bufsize := range bufsizes {
				// We don't use t.Run() here to avoid excessive logging due to
				// the large number of subtests.
				read := NewReadyReader(strings.NewReader(text))
				buf := newBufReadyReader(read, bufsize, mem.DefaultBufferPool())
				s := bufreader.fn(buf)
				if s != text {
					t.Errorf("fn=%s bufsize=%d want=%q got=%q", bufreader.name, bufsize, text, s)
				}
			}
		}
	}
}

func (s) TestBufReader_ReadEmptyBuffer(t *testing.T) {
	rr := NewReadyReader(new(bytes.Buffer))
	l := newBufReadyReader(rr, minReadBufferSize, mem.DefaultBufferPool())
	b := make([]byte, 100)
	n, err := l.Read(b)
	if err != io.EOF {
		t.Errorf("expected EOF from Read, got %q %v", b[:n], err)
	}
}

type errorThenGoodReader struct {
	didErr bool
	nread  int
}

var errFake = errors.New("fake error")

func (r *errorThenGoodReader) Read(p []byte) (int, error) {
	r.nread++
	if !r.didErr {
		r.didErr = true
		return 0, errFake
	}
	return len(p), nil
}

func (s) TestBufReader_ClearError(t *testing.T) {
	r := &errorThenGoodReader{}
	b := newBufReadyReader(NewReadyReader(r), minReadBufferSize, mem.DefaultBufferPool())
	buf := make([]byte, 1)
	if _, err := b.Read(nil); err != nil {
		t.Fatalf("1st nil Read = %v; want nil", err)
	}
	if _, err := b.Read(buf); err != errFake {
		t.Fatalf("1st Read = %v; want errFake", err)
	}
	if _, err := b.Read(nil); err != nil {
		t.Fatalf("2nd nil Read = %v; want nil", err)
	}
	if _, err := b.Read(buf); err != nil {
		t.Fatalf("3rd Read with buffer = %v; want nil", err)
	}
	if r.nread != 2 {
		t.Errorf("num reads = %d; want 2", r.nread)
	}
}

type emptyThenNonEmptyReader struct {
	r io.Reader
	n int
}

func (r *emptyThenNonEmptyReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return r.r.Read(p)
	}
	r.n--
	return 0, nil
}

func (s) TestBufReader_ReadZero(t *testing.T) {
	for _, size := range []int{100, 2} {
		t.Run(fmt.Sprintf("bufsize=%d", size), func(t *testing.T) {
			r := io.MultiReader(strings.NewReader("abc"), &emptyThenNonEmptyReader{r: strings.NewReader("def"), n: 1})
			br := newBufReadyReader(NewReadyReader(r), size, mem.DefaultBufferPool())
			want := func(s string, wantErr error) {
				p := make([]byte, 50)
				n, err := br.Read(p)
				if err != wantErr || n != len(s) || string(p[:n]) != s {
					t.Fatalf("read(%d) = %q, %v, want %q, %v", len(p), string(p[:n]), err, s, wantErr)
				}
				t.Logf("read(%d) = %q, %v", len(p), string(p[:n]), err)
			}
			want("abc", nil)
			want("", nil)
			want("def", nil)
			want("", io.EOF)
		})
	}
}
