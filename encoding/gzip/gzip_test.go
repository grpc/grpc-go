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

package gzip_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"math/rand/v2"
	"testing"

	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/mem"
)

const chunkSize = 16 * 1024

// makeCompressedSlice compresses a deterministic, realistically compressible
// payload of size n (so gzip uses Huffman/LZ77 blocks rather than stored
// blocks) and splits the compressed bytes into 16 KiB mem.Buffers.
func makeCompressedSlice(b *testing.B, n int) mem.BufferSlice {
	b.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	words := []string{
		"grpc", "compression", "message", "payload", "stream", "buffer",
		"header", "status", "metadata", "service", "method", "request",
		"response", "client", "server", "transport", "encoding", "gzip",
	}
	raw := make([]byte, 0, n)
	for len(raw) < n {
		w := words[rng.IntN(len(words))]
		raw = append(raw, w...)
		raw = append(raw, ' ')
	}
	raw = raw[:n]

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		b.Fatalf("gzip.Writer.Write failed: %v", err)
	}
	if err := zw.Close(); err != nil {
		b.Fatalf("gzip.Writer.Close failed: %v", err)
	}

	compressed := buf.Bytes()
	var slice mem.BufferSlice
	for len(compressed) > 0 {
		end := min(len(compressed), chunkSize)
		chunk := append([]byte(nil), compressed[:end]...)
		slice = append(slice, mem.NewBuffer(&chunk, mem.NopBufferPool{}))
		compressed = compressed[end:]
	}
	return slice
}

// BenchmarkDecompress measures end-to-end gzip decompression across payload
// sizes from 0 B to 4 MiB. Each compressed payload is split into 16 KiB
// mem.Buffers and read through mem.Reader into encoding/gzip's pooled
// decompressor and mem.ReadAll, exercising the same read path as gRPC message
// decompression.
func BenchmarkDecompress(b *testing.B) {
	comp := encoding.GetCompressor("gzip")
	pool := mem.DefaultBufferPool()

	sizes := []struct {
		name string
		size int
	}{
		{name: "0B", size: 0},
		{name: "128B", size: 128},
		{name: "1KiB", size: 1024},
		{name: "64KiB", size: 64 * 1024},
		{name: "4MiB", size: 4 * 1024 * 1024},
	}

	for _, tc := range sizes {
		b.Run(tc.name, func(b *testing.B) {
			compressed := makeCompressedSlice(b, tc.size)
			defer compressed.Free()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				r := compressed.Reader()
				dcReader, err := comp.Decompress(r)
				if err != nil {
					b.Fatalf("Decompress failed: %v", err)
				}
				out, err := mem.ReadAll(dcReader, pool)
				if err != nil {
					b.Fatalf("ReadAll failed: %v", err)
				}
				if out.Len() != tc.size {
					b.Fatalf("got %d bytes, want %d", out.Len(), tc.size)
				}
				if closer, ok := dcReader.(io.Closer); ok {
					if err := closer.Close(); err != nil {
						b.Fatalf("Close failed: %v", err)
					}
				}
				out.Free()
				r.Close()
			}
		})
	}
}
