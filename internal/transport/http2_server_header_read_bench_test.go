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
	"fmt"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/mem"
)

type headerBenchmarkConn struct {
	bytes.Reader
}

func (*headerBenchmarkConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func encodedRequestHeaders(b *testing.B, metadataFields int) []byte {
	b.Helper()

	var block bytes.Buffer
	encoder := hpack.NewEncoder(&block)

	fields := []hpack.HeaderField{
		{Name: ":method", Value: "POST", Sensitive: true},
		{Name: ":scheme", Value: "https", Sensitive: true},
		{Name: ":path", Value: "/service/method", Sensitive: true},
		{Name: ":authority", Value: "localhost", Sensitive: true},
		{Name: "content-type", Value: "application/grpc", Sensitive: true},
		{Name: "te", Value: "trailers", Sensitive: true},
	}

	for i := 0; i < metadataFields; i++ {
		fields = append(fields, hpack.HeaderField{
			Name:      fmt.Sprintf("metadata-%02d", i),
			Value:     "0123456789abcdef0123456789abcdef",
			Sensitive: true,
		})
	}

	for _, field := range fields {
		if err := encoder.WriteField(field); err != nil {
			b.Fatalf("failed to encode header field: %v", err)
		}
	}

	var wire bytes.Buffer
	writer := http2.NewFramer(&wire, nil)
	if err := writer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: block.Bytes(),
		EndStream:     true,
		EndHeaders:    true,
	}); err != nil {
		b.Fatalf("failed to encode HEADERS frame: %v", err)
	}

	return append([]byte(nil), wire.Bytes()...)
}

func BenchmarkReadServerHeaders(b *testing.B) {
	for _, metadataFields := range []int{0, 4, 12, 48} {
		b.Run(fmt.Sprintf("metadata_fields=%d", metadataFields), func(b *testing.B) {
			wire := encodedRequestHeaders(b, metadataFields)
			conn := &headerBenchmarkConn{}
			framer := newFramer(
				conn,
				0,
				0,
				false,
				defaultClientMaxHeaderListSize,
				mem.DefaultBufferPool(),
			)

			b.ReportAllocs()
			for b.Loop() {
				conn.Reset(wire)
				frame, err := framer.readServerFrame()
				if err != nil {
					b.Fatalf("readFrame() failed: %v", err)
				}
				headers, ok := frame.(*http2.MetaHeadersFrame)
				if !ok {
					b.Fatalf("readFrame() returned %T, want *http2.MetaHeadersFrame", frame)
				}
				if got, want := len(headers.Fields), metadataFields+6; got != want {
					b.Fatalf("decoded %d fields, want %d", got, want)
				}
			}
		})
	}
}
