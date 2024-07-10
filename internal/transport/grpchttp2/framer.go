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

import "golang.org/x/net/http2/hpack"

// FrameType represents the type of an HTTP/2 Frame.
// See [Frame Type].
//
// [Frame Type]: https://httpwg.org/specs/rfc7540.html#FrameType
type FrameType uint8

const (
	FrameTypeData         FrameType = 0x0
	FrameTypeHeaders      FrameType = 0x1
	FrameTypePriority     FrameType = 0x2
	FrameTypeRSTStream    FrameType = 0x3
	FrameTypeSettings     FrameType = 0x4
	FrameTypePushPromise  FrameType = 0x5
	FrameTypePing         FrameType = 0x6
	FrameTypeGoAway       FrameType = 0x7
	FrameTypeWindowUpdate FrameType = 0x8
	FrameTypeContinuation FrameType = 0x9
)

// Flags represents one or more flags set on an HTTP/2 Frame.
type Flags uint8

const (
	FlagDataEndStream          Flags = 0x1
	FlagDataPadded             Flags = 0x8
	FlagHeadersEndStream       Flags = 0x1
	FlagHeadersEndHeaders      Flags = 0x4
	FlagHeadersPadded          Flags = 0x8
	FlagHeadersPriority        Flags = 0x20
	FlagSettingsAck            Flags = 0x1
	FlagPingAck                Flags = 0x1
	FlagContinuationEndHeaders Flags = 0x4
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
	Flags Flags
	// StreamID is the ID for the stream which this frame is for. If the
	// frame is connection specific instead of stream specific, the
	// streamID is 0.
	StreamID uint32
}

// Frame represents an HTTP/2 Frame.
type Frame interface {
	Header() FrameHeader
}

type DataFrame struct {
	hdr  FrameHeader
	free func([]byte)
	Data []byte
}

func (f *DataFrame) Header() FrameHeader {
	return f.hdr
}

func (f *DataFrame) Free() {
	if f.free != nil {
		f.free(f.Data)
	}
}

type HeadersFrame struct {
	hdr      FrameHeader
	free     func([]byte)
	HdrBlock []byte
}

func (f *HeadersFrame) Header() FrameHeader {
	return f.hdr
}

func (f *HeadersFrame) Free() {
	if f.free != nil {
		f.free(f.HdrBlock)
	}
}

type RSTStreamFrame struct {
	hdr  FrameHeader
	Code ErrCode
}

func (f *RSTStreamFrame) Header() FrameHeader {
	return f.hdr
}

type SettingsFrame struct {
	hdr      FrameHeader
	free     func([]byte)
	settings []byte
}

func (f *SettingsFrame) Header() FrameHeader {
	return f.hdr
}

func (f *SettingsFrame) Free() {
	if f.free != nil {
		f.free(f.settings)
	}
}

type PingFrame struct {
	hdr  FrameHeader
	free func([]byte)
	Data []byte
}

func (f *PingFrame) Header() FrameHeader {
	return f.hdr
}

func (f *PingFrame) Free() {
	if f.free != nil {
		f.free(f.Data)
	}
}

type GoAwayFrame struct {
	hdr          FrameHeader
	free         func([]byte)
	LastStreamID uint32
	Code         ErrCode
	DebugData    []byte
}

func (f *GoAwayFrame) Header() FrameHeader {
	return f.hdr
}

func (f *GoAwayFrame) Free() {
	if f.free != nil {
		f.free(f.DebugData)
	}
}

type WindowUpdateFrame struct {
	hdr FrameHeader
	Inc uint32
}

func (f *WindowUpdateFrame) Header() FrameHeader {
	return f.hdr
}

type ContinuationFrame struct {
	hdr      FrameHeader
	free     func([]byte)
	HdrBlock []byte
}

func (f *ContinuationFrame) Header() FrameHeader {
	return f.hdr
}

func (f *ContinuationFrame) Free() {
	if f.free != nil {
		f.free(f.HdrBlock)
	}
}

// Framer represents a Framer used in gRPC-Go.
type Framer interface {
	// SetMetaDecoder will set a decoder for the framer. When the decoder is
	// set, ReadFrame will parse the header values, merging all Headers and
	// Continuation frames.
	SetMetaDecoder(d *hpack.Decoder)
	// ReadFrame returns an HTTP/2 Frame. It is the caller's responsibility to
	// free the frame once it is done using it.
	ReadFrame() (Frame, error)
	WriteData(streamID uint32, endStream bool, data ...[]byte) error
	WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock ...[]byte) error
	WriteRSTStream(streamID uint32, code ErrCode) error
	WriteSettings(settings ...Setting) error
	WriteSettingsAck() error
	WritePing(ack bool, data [8]byte) error
	WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error
	WriteWindowUpdate(streamID, inc uint32) error
	WriteContinuation(streamID uint32, endHeaders bool, headerBlock ...[]byte) error
}
