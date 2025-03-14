/*
 *
 * Copyright 2017 gRPC authors.
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

// Package gzip implements and registers the gzip compressor
// during the initialization.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package gzip

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc/encoding"
)

// Name is the name registered for the gzip compressor.
const Name = "gzip"

func init() {
	c := &compressor{}
	c.poolCompressor.New = func() any {
		return gzip.NewWriter(io.Discard)
	}
	encoding.RegisterCompressor(c)
}

type writer struct {
	gzw  *gzip.Writer
	pool *sync.Pool
}

// SetLevel updates the registered gzip compressor to use the compression level specified (gzip.HuffmanOnly is not supported).
// NOTE: this function must only be called during initialization time (i.e. in an init() function),
// and is not thread-safe.
//
// The error returned will be nil if the specified level is valid.
func SetLevel(level int) error {
	if level < gzip.DefaultCompression || level > gzip.BestCompression {
		return fmt.Errorf("grpc: invalid gzip compression level: %d", level)
	}
	c := encoding.GetCompressor(Name).(*compressor)
	c.poolCompressor.New = func() any {
		w, err := gzip.NewWriterLevel(io.Discard, level)
		if err != nil {
			panic(err)
		}
		return w
	}
	return nil
}

func (c *compressor) Compress(w io.Writer) (io.WriteCloser, error) {
	z := c.poolCompressor.Get().(*gzip.Writer)
	z.Reset(w)
	return &writer{gzw: z, pool: &c.poolCompressor}, nil
}

func (z *writer) Write(p []byte) (int, error) {
	if z.gzw == nil {
		return 0, errors.New("writer is closed")
	}
	return z.gzw.Write(p)
}

func (z *writer) Close() error {
	if z.gzw == nil {
		return nil
	}
	err := z.gzw.Close()
	z.pool.Put(z.gzw)
	z.gzw = nil
	return err
}

type reader struct {
	gzr  *gzip.Reader
	pool *sync.Pool
}

func (c *compressor) Decompress(r io.Reader) (io.Reader, error) {
	z, inPool := c.poolDecompressor.Get().(*gzip.Reader)
	if !inPool {
		var err error
		z, err = gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
	} else if err := z.Reset(r); err != nil {
		c.poolDecompressor.Put(z)
		return nil, err
	}
	return &reader{gzr: z, pool: &c.poolDecompressor}, nil
}

func (z *reader) Read(p []byte) (n int, err error) {
	if z.gzr == nil {
		return 0, io.EOF
	}
	n, err = z.gzr.Read(p)
	if err == io.EOF {
		z.pool.Put(z.gzr)
		z.gzr = nil
	}
	return n, err
}

// RFC1952 specifies that the last four bytes "contains the size of
// the original (uncompressed) input data modulo 2^32."
// gRPC has a max message size of 2GB so we don't need to worry about wraparound.
func (c *compressor) DecompressedSize(buf []byte) int {
	last := len(buf)
	if last < 4 {
		return -1
	}
	return int(binary.LittleEndian.Uint32(buf[last-4 : last]))
}

func (c *compressor) Name() string {
	return Name
}

type compressor struct {
	poolCompressor   sync.Pool
	poolDecompressor sync.Pool
}
