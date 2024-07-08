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

// Package grpcframer defines the interface for implementing an HTTP/2 framer,
// its required types and an implementation of said framer.
package grpcframer

import "sync"

// FrameType represents the type of a Frame and its value according to the
// HTTP/2 spec.
// See https://httpwg.org/specs/rfc7540.html#FrameTypes.
type FrameType uint8

const (
	DataFrameType         FrameType = 0x0
	HeadersFrameType      FrameType = 0x1
	PriorityFrameType     FrameType = 0x2
	RSTStreamFrameType    FrameType = 0x3
	SettingsFrameType     FrameType = 0x4
	PushPromiseFrameType  FrameType = 0x5
	PingFrameType         FrameType = 0x6
	GoAwayFrameType       FrameType = 0x7
	WindowUpdateFrameType FrameType = 0x8
	ContinuationFrameType FrameType = 0x9
)

// Flags represents the flags that can be set on every frame type.
type Flags uint8

const (
	// Data Frame
	FlagDataEndStream Flags = 0x1
	FlagDataPadded    Flags = 0x8

	// Headers Frame
	FlagHeadersEndStream  Flags = 0x1
	FlagHeadersEndHeaders Flags = 0x4
	FlagHeadersPadded     Flags = 0x8
	FlagHeadersPriority   Flags = 0x20

	// Settings Frame
	FlagSettingsAck Flags = 0x1

	// Ping Frame
	FlagPingAck Flags = 0x1

	// Continuation Frame
	FlagContinuationEndHeaders Flags = 0x4

	FlagPushPromiseEndHeaders Flags = 0x4
	FlagPushPromisePadded     Flags = 0x8
)

// Setting represents the id and value pair of a particular HTTP/2 setting.
// See https://httpwg.org/specs/rfc7540.html#SettingFormat.
type Setting struct {
	ID    SettingID
	Value uint32
}

// SettingID represents the id of a specific HTTP/2 setting.
// See https://httpwg.org/specs/rfc7540.html#SettingValues.
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
// See https://httpwg.org/specs/rfc7540.html#FrameHeader.
type FrameHeader struct {
	// Size is the size of the frame's payload without the 9 header bytes.
	// As per the HTTP/2 spec, size can be up to 3 bytes, but only frames
	// up to 16KB can be processed without agreement.
	Size uint32

	// Type is a byte that represents the Frame Type. The HTTP/2 spec
	// defines 10 standard types but extension frames may be written.
	Type FrameType

	// Flags is a byte representing the flags set for the specific frame
	// type.
	Flags Flags

	// StreamID is the ID for the stream which this frame is for. If the
	// frame is connection specific instead of stream specific, the
	// streamID is 0.
	StreamID uint32
}

// Frame represents any kind of HTTP/2 Frame, which can be casted into
// its corresponding type using the Header method provided.
type Frame interface {
	// Header returns this frame's Header.
	Header() FrameHeader
}

type DataFrame struct {
	hdr  FrameHeader
	pool *sync.Pool
	Data []byte
}

func (f *DataFrame) Header() FrameHeader {
	return f.hdr
}

func (f *DataFrame) Free() {
	if f.Data == nil {
		return
	}

	f.pool.Put(&f.Data)
	f.Data = nil
}

type HeadersFrame struct {
	hdr      FrameHeader
	pool     *sync.Pool
	HdrBlock []byte
}

func (f *HeadersFrame) Header() FrameHeader {
	return f.hdr
}

func (f *HeadersFrame) Free() {
	if f.HdrBlock == nil {
		return
	}

	f.pool.Put(&f.HdrBlock)
	f.HdrBlock = nil
}

type RSTStreamFrame struct {
	hdr  FrameHeader
	Code ErrorCode
}

func (f *RSTStreamFrame) Header() FrameHeader {
	return f.hdr
}

type SettingsFrame struct {
	hdr      FrameHeader
	pool     *sync.Pool
	settings []byte
}

func (f *SettingsFrame) Header() FrameHeader {
	return f.hdr
}

func (f *SettingsFrame) Free() {
	if f.settings == nil {
		return
	}

	f.pool.Put(&f.settings)
	f.settings = nil
}

type PingFrame struct {
	hdr  FrameHeader
	pool *sync.Pool
	Data []byte
}

func (f *PingFrame) Header() FrameHeader {
	return f.hdr
}

func (f *PingFrame) Free() {
	if f.Data == nil {
		return
	}

	f.pool.Put(&f.Data)
	f.Data = nil
}

type GoAwayFrame struct {
	hdr          FrameHeader
	pool         *sync.Pool
	LastStreamID uint32
	Code         ErrorCode
	DebugData    []byte
}

func (f *GoAwayFrame) Header() FrameHeader {
	return f.hdr
}

func (f *GoAwayFrame) Free() {
	if f.DebugData == nil {
		return
	}

	f.pool.Put(&f.DebugData)
	f.DebugData = nil
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
	pool     *sync.Pool
	HdrBlock []byte
}

func (f *ContinuationFrame) Header() FrameHeader {
	return f.hdr
}

func (f *ContinuationFrame) Free() {
	if f.HdrBlock == nil {
		return
	}

	f.pool.Put(&f.HdrBlock)
	f.HdrBlock = nil
}

// GRPCFramer represents the methods a framer must implement to be used in gRPC-Go.
type GRPCFramer interface {
	// ReadFrame returns an HTTP/2 Frame. It is the caller's responsibility to
	// free the frame once it is done using it.
	ReadFrame() (Frame, error)
	WriteData(streamID uint32, endStream bool, data ...[]byte) error
	WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock ...[]byte) error
	WriteRSTStream(streamID uint32, code ErrorCode) error
	WriteSettings(settings ...Setting) error
	WriteSettingsAck() error
	WritePing(ack bool, data [8]byte) error
	WriteGoAway(maxStreamID uint32, code ErrorCode, debugData []byte) error
	WriteWindowUpdate(streamID, inc uint32) error
	WriteContinuation(streamID uint32, endHeaders bool, headerBlock ...[]byte) error
}
