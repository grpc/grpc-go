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
	"reflect"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/mem"
)

type serverHeaderTestConn struct {
	bytes.Reader
}

func (*serverHeaderTestConn) Write(p []byte) (int, error) {
	return len(p), nil
}

type serverHeaderTestBlock struct {
	fields       []hpack.HeaderField
	continuation bool
	endStream    bool
}

func encodeServerHeaderBlocks(
	t *testing.T,
	blocks ...serverHeaderTestBlock,
) []byte {
	t.Helper()

	var wire bytes.Buffer
	writer := http2.NewFramer(&wire, nil)

	var encoded bytes.Buffer
	encoder := hpack.NewEncoder(&encoded)

	for i, block := range blocks {
		encoded.Reset()

		for _, field := range block.fields {
			if err := encoder.WriteField(field); err != nil {
				t.Fatalf("failed to encode field: %v", err)
			}
		}

		fragment := append([]byte(nil), encoded.Bytes()...)
		streamID := uint32(1 + 2*i)

		if !block.continuation {
			if err := writer.WriteHeaders(http2.HeadersFrameParam{
				StreamID:      streamID,
				BlockFragment: fragment,
				EndStream:     block.endStream,
				EndHeaders:    true,
			}); err != nil {
				t.Fatalf("failed to write HEADERS: %v", err)
			}
			continue
		}

		split := len(fragment) / 2
		if split == 0 {
			t.Fatal("encoded block is too small to split")
		}

		if err := writer.WriteHeaders(http2.HeadersFrameParam{
			StreamID:      streamID,
			BlockFragment: fragment[:split],
			EndStream:     block.endStream,
			EndHeaders:    false,
		}); err != nil {
			t.Fatalf("failed to write HEADERS: %v", err)
		}

		if err := writer.WriteContinuation(
			streamID,
			true,
			fragment[split:],
		); err != nil {
			t.Fatalf("failed to write CONTINUATION: %v", err)
		}
	}

	return append([]byte(nil), wire.Bytes()...)
}

func encodeRawServerHeaders(
	t *testing.T,
	fragment []byte,
) []byte {
	t.Helper()

	var wire bytes.Buffer
	writer := http2.NewFramer(&wire, nil)

	if err := writer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: fragment,
		EndHeaders:    true,
	}); err != nil {
		t.Fatalf("failed to write raw HEADERS: %v", err)
	}

	return append([]byte(nil), wire.Bytes()...)
}

func encodeInterruptedServerHeaders(t *testing.T) []byte {
	t.Helper()

	fields := validServerHeaderFields()
	var encoded bytes.Buffer
	encoder := hpack.NewEncoder(&encoded)

	for _, field := range fields {
		if err := encoder.WriteField(field); err != nil {
			t.Fatalf("failed to encode field: %v", err)
		}
	}

	fragment := encoded.Bytes()
	split := len(fragment) / 2

	var wire bytes.Buffer
	writer := http2.NewFramer(&wire, nil)

	if err := writer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: fragment[:split],
		EndHeaders:    false,
	}); err != nil {
		t.Fatalf("failed to write HEADERS: %v", err)
	}

	if err := writer.WriteData(1, false, []byte("not continuation")); err != nil {
		t.Fatalf("failed to write DATA: %v", err)
	}

	return append([]byte(nil), wire.Bytes()...)
}

func validServerHeaderFields() []hpack.HeaderField {
	return []hpack.HeaderField{
		{Name: ":method", Value: "POST", Sensitive: true},
		{Name: ":scheme", Value: "https", Sensitive: true},
		{Name: ":path", Value: "/service/method", Sensitive: true},
		{Name: ":authority", Value: "localhost", Sensitive: true},
		{Name: "content-type", Value: "application/grpc", Sensitive: true},
		{Name: "te", Value: "trailers", Sensitive: true},
		{Name: "metadata-key", Value: "metadata-value", Sensitive: true},
	}
}

func newServerHeaderTestFramer(
	wire []byte,
	maxHeaderListSize uint32,
) *framer {
	conn := &serverHeaderTestConn{}
	conn.Reset(wire)

	return newFramer(
		conn,
		0,
		0,
		false,
		maxHeaderListSize,
		mem.DefaultBufferPool(),
	)
}

func describeServerHeaderError(err error) string {
	if err == nil {
		return ""
	}

	switch err := err.(type) {
	case http2.StreamError:
		return fmt.Sprintf(
			"stream:%d:%v:%v",
			err.StreamID,
			err.Code,
			err.Cause,
		)

	case http2.ConnectionError:
		return fmt.Sprintf("connection:%v", err)
	}

	return fmt.Sprintf("%T:%v", err, err)
}

func errorDetailText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func compareServerHeaderRead(
	t *testing.T,
	oldFrame any,
	oldErr error,
	oldDetail error,
	newFrame any,
	newErr error,
	newDetail error,
) {
	t.Helper()

	if got, want := describeServerHeaderError(newErr),
		describeServerHeaderError(oldErr); got != want {
		t.Fatalf("new error %q, want %q", got, want)
	}

	if got, want := errorDetailText(newDetail),
		errorDetailText(oldDetail); got != want {
		t.Fatalf("new error detail %q, want %q", got, want)
	}

	if (newFrame == nil) != (oldFrame == nil) {
		t.Fatalf(
			"new frame nil=%v, want %v",
			newFrame == nil,
			oldFrame == nil,
		)
	}

	if oldFrame == nil {
		return
	}

	oldHeaders, ok := oldFrame.(*http2.MetaHeadersFrame)
	if !ok {
		t.Fatalf(
			"old decoder returned %T, want *http2.MetaHeadersFrame",
			oldFrame,
		)
	}

	newHeaders, ok := newFrame.(*http2.MetaHeadersFrame)
	if !ok {
		t.Fatalf(
			"new decoder returned %T, want *http2.MetaHeadersFrame",
			newFrame,
		)
	}

	oldHeader := oldHeaders.Header()
	newHeader := newHeaders.Header()

	if newHeader.StreamID != oldHeader.StreamID ||
		newHeader.Length != oldHeader.Length ||
		newHeader.Type != oldHeader.Type ||
		newHeader.Flags != oldHeader.Flags {
		t.Fatalf(
			"new frame header %+v, want %+v",
			newHeader,
			oldHeader,
		)
	}

	if newHeaders.StreamEnded() != oldHeaders.StreamEnded() {
		t.Fatalf(
			"new StreamEnded=%v, want %v",
			newHeaders.StreamEnded(),
			oldHeaders.StreamEnded(),
		)
	}

	if newHeaders.Truncated != oldHeaders.Truncated {
		t.Fatalf(
			"new Truncated=%v, want %v",
			newHeaders.Truncated,
			oldHeaders.Truncated,
		)
	}

	if !reflect.DeepEqual(newHeaders.Fields, oldHeaders.Fields) {
		t.Fatalf(
			"new fields %#v, want %#v",
			newHeaders.Fields,
			oldHeaders.Fields,
		)
	}
}

func compareServerHeaderWire(
	t *testing.T,
	wire []byte,
	maxHeaderListSize uint32,
	reads int,
) {
	t.Helper()

	oldFramer := newServerHeaderTestFramer(wire, maxHeaderListSize)
	newFramer := newServerHeaderTestFramer(wire, maxHeaderListSize)

	for i := 0; i < reads; i++ {
		oldFrame, oldErr := oldFramer.readFrame()
		oldDetail := oldFramer.errorDetail()

		newFrame, newErr := newFramer.readServerFrame()
		newDetail := newFramer.errorDetail()

		compareServerHeaderRead(
			t,
			oldFrame,
			oldErr,
			oldDetail,
			newFrame,
			newErr,
			newDetail,
		)
	}
}

func TestServerHeaderDecoderMatchesHTTP2Framer(t *testing.T) {
	valid := validServerHeaderFields()

	tests := []struct {
		name              string
		fields            []hpack.HeaderField
		continuation      bool
		endStream         bool
		maxHeaderListSize uint32
		wantError         bool
		wantTruncated     bool
	}{
		{
			name:      "valid",
			fields:    valid,
			endStream: true,
		},
		{
			name:         "continuation",
			fields:       valid,
			continuation: true,
		},
		{
			name: "uppercase regular name",
			fields: append(
				append([]hpack.HeaderField(nil), valid[:4]...),
				hpack.HeaderField{
					Name:      "Uppercase",
					Value:     "value",
					Sensitive: true,
				},
			),
			wantError: true,
		},
		{
			name: "invalid regular value",
			fields: append(
				append([]hpack.HeaderField(nil), valid...),
				hpack.HeaderField{
					Name:      "invalid-value",
					Value:     "line\nbreak",
					Sensitive: true,
				},
			),
			wantError: true,
		},
		{
			name: "pseudo after regular",
			fields: []hpack.HeaderField{
				{Name: ":method", Value: "POST", Sensitive: true},
				{Name: "content-type", Value: "application/grpc", Sensitive: true},
				{Name: ":path", Value: "/service/method", Sensitive: true},
			},
			wantError: true,
		},
		{
			name: "duplicate pseudo",
			fields: []hpack.HeaderField{
				{Name: ":method", Value: "POST", Sensitive: true},
				{Name: ":method", Value: "POST", Sensitive: true},
			},
			wantError: true,
		},
		{
			name: "mixed pseudo types",
			fields: []hpack.HeaderField{
				{Name: ":method", Value: "POST", Sensitive: true},
				{Name: ":status", Value: "200", Sensitive: true},
			},
			wantError: true,
		},
		{
			name: "unknown pseudo",
			fields: []hpack.HeaderField{
				{Name: ":unknown", Value: "value", Sensitive: true},
			},
			wantError: true,
		},
		{
			name:              "truncated",
			fields:            valid,
			maxHeaderListSize: 220,
			wantTruncated:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := encodeServerHeaderBlocks(
				t,
				serverHeaderTestBlock{
					fields:       test.fields,
					continuation: test.continuation,
					endStream:    test.endStream,
				},
			)

			oldFramer := newServerHeaderTestFramer(
				wire,
				test.maxHeaderListSize,
			)
			newFramer := newServerHeaderTestFramer(
				wire,
				test.maxHeaderListSize,
			)

			oldFrame, oldErr := oldFramer.readFrame()
			oldDetail := oldFramer.errorDetail()

			newFrame, newErr := newFramer.readServerFrame()
			newDetail := newFramer.errorDetail()

			compareServerHeaderRead(
				t,
				oldFrame,
				oldErr,
				oldDetail,
				newFrame,
				newErr,
				newDetail,
			)

			if test.wantError != (oldErr != nil) {
				t.Fatalf(
					"old error=%v, want error=%v",
					oldErr,
					test.wantError,
				)
			}

			if test.wantTruncated {
				headers, ok := oldFrame.(*http2.MetaHeadersFrame)
				if !ok {
					t.Fatalf("old frame type %T", oldFrame)
				}
				if !headers.Truncated {
					t.Fatal("old decoder did not truncate")
				}
			}
		})
	}
}

func TestServerHeaderDecoderPreservesDynamicTable(t *testing.T) {
	fields := []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":path", Value: "/service/method"},
		{Name: "metadata-key", Value: "metadata-value"},
	}

	wire := encodeServerHeaderBlocks(
		t,
		serverHeaderTestBlock{fields: fields},
		serverHeaderTestBlock{fields: fields},
	)

	compareServerHeaderWire(t, wire, 0, 2)
}

func TestServerHeaderDecoderMatchesCompressionError(t *testing.T) {
	wire := encodeRawServerHeaders(t, []byte{0xff})
	compareServerHeaderWire(t, wire, 0, 1)

	framer := newServerHeaderTestFramer(wire, 0)
	_, err := framer.readFrame()

	connectionError, ok := err.(http2.ConnectionError)
	if !ok {
		t.Fatalf("error type %T, want http2.ConnectionError", err)
	}
	if connectionError != http2.ConnectionError(http2.ErrCodeCompression) {
		t.Fatalf(
			"connection error %v, want %v",
			connectionError,
			http2.ErrCodeCompression,
		)
	}
}

func TestServerHeaderDecoderMatchesContinuationOrderingError(t *testing.T) {
	wire := encodeInterruptedServerHeaders(t)
	compareServerHeaderWire(t, wire, 0, 1)
}
