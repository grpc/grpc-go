/*
 *
 * Copyright 2024 gRPC authors.
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

// Package grpchttp2 defines HTTP/2 types and a framer API and implementation.
package grpchttp2

import (
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/mem"
)

// FrameType represents the type of an HTTP/2 Frame.
// See [Frame Type].
//
// [Frame Type]: https://httpwg.org/specs/rfc7540.html#FrameType
type FrameType uint8

// Frame types defined in the HTTP/2 Spec.
const (
	FrameTypeData         FrameType = 0x0
	FrameTypeHeaders      FrameType = 0x1
	FrameTypeRSTStream    FrameType = 0x3
	FrameTypeSettings     FrameType = 0x4
	FrameTypePing         FrameType = 0x6
	FrameTypeGoAway       FrameType = 0x7
	FrameTypeWindowUpdate FrameType = 0x8
	FrameTypeContinuation FrameType = 0x9
)

// Flag represents one or more flags set on an HTTP/2 Frame.
type Flag uint8

// Flags defined in the HTTP/2 Spec.
const (
	FlagDataEndStream          Flag = 0x1
	FlagDataPadded             Flag = 0x8
	FlagHeadersEndStream       Flag = 0x1
	FlagHeadersEndHeaders      Flag = 0x4
	FlagHeadersPadded          Flag = 0x8
	FlagHeadersPriority        Flag = 0x20
	FlagSettingsAck            Flag = 0x1
	FlagPingAck                Flag = 0x1
	FlagContinuationEndHeaders Flag = 0x4
)

// Setting represents the id and value pair of an HTTP/2 setting.
// See [Setting Format].
//
// [Setting Format]: https://httpwg.org/specs/rfc7540.html#SettingFormat
type Setting struct {
	ID    SettingID
	Value uint32
}

// SettingID represents the id of an HTTP/2 setting.
// See [Setting Values].
//
// [Setting Values]: https://httpwg.org/specs/rfc7540.html#SettingValues
type SettingID uint16

// Setting IDs defined in the HTTP/2 Spec.
const (
	SettingsHeaderTableSize      SettingID = 0x1
	SettingsEnablePush           SettingID = 0x2
	SettingsMaxConcurrentStreams SettingID = 0x3
	SettingsInitialWindowSize    SettingID = 0x4
	SettingsMaxFrameSize         SettingID = 0x5
	SettingsMaxHeaderListSize    SettingID = 0x6
)

// FrameHeader is the 9 byte header of any HTTP/2 Frame.
// See [Frame Header].
//
// [Frame Header]: https://httpwg.org/specs/rfc7540.html#FrameHeader
type FrameHeader struct {
	// Size is the size of the frame's payload without the 9 header bytes.
	// As per the HTTP/2 spec, size can be up to 3 bytes, but only frames
	// up to 16KB can be processed without agreement.
	Size uint32
	// Type is a byte that represents the Frame Type.
	Type FrameType
	// Flags is a byte representing the flags set on this Frame.
	Flags Flag
	// StreamID is the ID for the stream which this frame is for. If the
	// frame is connection specific instead of stream specific, the
	// streamID is 0.
	StreamID uint32
}

// Frame represents an HTTP/2 Frame. This interface struct is only to be used
// on the read path of the Framer. The writing path expects the data to be
// passed individually, not using this type.
//
// Each concrete Frame type defined below implements the Frame interface.
type Frame interface {
	Header() *FrameHeader
}

// DataFrame is the representation of a [DATA frame]. DATA frames convey
// arbitrary, variable-length sequences of octets associated with a stream. It
// is the user's responsibility to call Data.Free() when it is no longer
// needed.
//
// [DATA frame]: https://httpwg.org/specs/rfc7540.html#DATA
type DataFrame struct {
	hdr  *FrameHeader
	Data *mem.Buffer
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *DataFrame) Header() *FrameHeader {
	return f.hdr
}

// HeadersFrame is the representation of a [HEADERS Frame]. The HEADERS frame
// is used to open a stream, and additionally carries a header block fragment.
// It is the user's responsibility to call HdrBlock.Free() when it is no longer
// needed.
//
// [HEADERS Frame]: https://httpwg.org/specs/rfc7540.html#HEADERS
type HeadersFrame struct {
	hdr      *FrameHeader
	HdrBlock *mem.Buffer
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *HeadersFrame) Header() *FrameHeader {
	return f.hdr
}

// RSTStreamFrame is the representation of a [RST_STREAM Frame]. The RST_STREAM
// frame allows for immediate termination of a stream
//
// [RST_STREAM Frame]: https://httpwg.org/specs/rfc7540.html#RST_STREAM
type RSTStreamFrame struct {
	hdr  *FrameHeader
	Code ErrCode
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *RSTStreamFrame) Header() *FrameHeader {
	return f.hdr
}

// SettingsFrame is the representation of a [SETTINGS Frame]. The SETTINGS frame
// conveys configuration parameters that affect how endpoints communicate, such
// as preferences and constraints on peer behavior.
//
// [SETTINGS Frame]: https://httpwg.org/specs/rfc7540.html#SETTINGS
type SettingsFrame struct {
	hdr      *FrameHeader
	Settings []Setting
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *SettingsFrame) Header() *FrameHeader {
	return f.hdr
}

// PingFrame is the representation of a [PING Frame]. The PING frame is a
// mechanism for measuring a minimal round-trip time from the sender, as well
// as determining whether an idle connection is still functional.
//
// It is the user's responsibility to call Data.Free() when it is no longer
// needed.
//
// [PING Frame]: https://httpwg.org/specs/rfc7540.html#PING
type PingFrame struct {
	hdr  *FrameHeader
	Data *mem.Buffer
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *PingFrame) Header() *FrameHeader {
	return f.hdr
}

// GoAwayFrame is the representation of a [GOAWAY Frame]. The GOAWAY frame is
// used to initiate shutdown of a connection or to signal serious error
// conditions.
//
// It is the user's responsibility to call DebugData.Free() when it is no longer
// needed.
//
// [GOAWAY Frame]: https://httpwg.org/specs/rfc7540.html#GOAWAY
type GoAwayFrame struct {
	hdr          *FrameHeader
	LastStreamID uint32
	Code         ErrCode
	DebugData    *mem.Buffer
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *GoAwayFrame) Header() *FrameHeader {
	return f.hdr
}

// WindowUpdateFrame is the representation of a [WINDOW_UPDATE Frame]. The
// WINDOW_UPDATE frame is used to implement flow control.
//
// [WINDOW_UPDATE Frame]: https://httpwg.org/specs/rfc7540.html#WINDOW_UPDATE
type WindowUpdateFrame struct {
	hdr *FrameHeader
	Inc uint32
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *WindowUpdateFrame) Header() *FrameHeader {
	return f.hdr
}

// ContinuationFrame is the representation of a [CONTINUATION Frame]. The
// CONTINUATION frame is used to continue a sequence of header block fragments.
//
// It is the user's responsibility to call HdrBlock.Free() when it is no longer
// needed.
//
// [CONTINUATION Frame]: https://httpwg.org/specs/rfc7540.html#CONTINUATION
type ContinuationFrame struct {
	hdr      *FrameHeader
	HdrBlock *mem.Buffer
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *ContinuationFrame) Header() *FrameHeader {
	return f.hdr
}

// MetaHeadersFrame is the representation of one HEADERS frame and zero or more
// contiguous CONTINUATION frames and the decoding of their HPACK-encoded
// contents.  This frame type is not transmitted over the network and is only
// generated by the ReadFrame() function.
type MetaHeadersFrame struct {
	hdr    *FrameHeader
	Fields []hpack.HeaderField
	// Truncated indicates whether the MetaHeadersFrame has been truncated due
	// to being longer than the MaxHeaderListSize.
	Truncated bool
}

// Header returns the 9 byte HTTP/2 header for this frame.
func (f *MetaHeadersFrame) Header() *FrameHeader {
	return f.hdr
}

// Framer encapsulates the functionality to read and write HTTP/2 frames.
type Framer interface {
	// ReadFrame returns grpchttp2.Frame. It is the caller's responsibility to
	// free the underlying buffer when done using the Frame.
	ReadFrame() (Frame, error)
	// WriteData writes an HTTP/2 DATA frame to the stream. The data is expected
	// to be freed by the caller.
	WriteData(streamID uint32, endStream bool, data mem.BufferSlice) error
	// WriteHeaders writes an HTTP/2 HEADERS frame to the stream.
	WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock []byte) error
	// WriteRSTStream writes an HTTP/2 RST_STREAM frame to the stream.
	WriteRSTStream(streamID uint32, code ErrCode) error
	// WriteSettings writes an HTTP/2 SETTINGS frame to the connection.
	WriteSettings(settings ...Setting) error
	// WriteSettingsAck writes an HTTP/2 SETTINGS frame with the ACK flag set.
	WriteSettingsAck() error
	// WritePing writes an HTTP/2 PING frame to the connection.
	WritePing(ack bool, data [8]byte) error
	// WriteGoAway writes an HTTP/2 GOAWAY frame to the connection.
	WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error
	// WriteWindowUpdate writes an HTTP/2 WINDOW_UPDATE frame to the stream.
	WriteWindowUpdate(streamID, inc uint32) error
	// WriteContinuation writes an HTTP/2 CONTINUATION frame to the stream.
	WriteContinuation(streamID uint32, endHeaders bool, headerBlock []byte) error
}
