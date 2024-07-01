// Copyright 2014 The Go Authors. All rights reserved.

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

// Package grpcframer defines and implements everything related to converting
// data into HTTP/2 Frames and viceversa. It is meant for grpc-internal usage
// and is not intended to be imported directly by users.
package grpcframer

import "fmt"

type FrameType uint8

const (
	DataFrame         FrameType = 0x0
	HeadersFrame      FrameType = 0x1
	PriorityFrame     FrameType = 0x2
	RSTStreamFrame    FrameType = 0x3
	SettingsFrame     FrameType = 0x4
	PushPromiseFrame  FrameType = 0x5
	PingFrame         FrameType = 0x6
	GoAwayFrame       FrameType = 0x7
	WindowUpdateFrame FrameType = 0x8
	ContinuationFrame FrameType = 0x9
)

var frameName = map[FrameType]string{
	DataFrame:         "DATA",
	HeadersFrame:      "HEADERS",
	PriorityFrame:     "PRIORITY",
	RSTStreamFrame:    "RST_STREAM",
	SettingsFrame:     "SETTINGS",
	PushPromiseFrame:  "PUSH_PROMISE",
	PingFrame:         "PING",
	GoAwayFrame:       "GOAWAY",
	WindowUpdateFrame: "WINDOW_UPDATE",
	ContinuationFrame: "CONTINUATION",
}

func (t FrameType) String() string {
	if s, ok := frameName[t]; ok {
		return s
	}
	return fmt.Sprintf("UNKNOWN_FRAME_TYPE_%d", uint8(t))
}

// Flags is a bitmask of HTTP/2 flags.
// The meaning of flags varies depending on the frame type.
type Flags uint8

// Has reports whether f contains all (0 or more) flags in v.
func (f Flags) Has(v Flags) bool {
	return (f & v) == v
}

// Frame-specific FrameHeader flag bits.
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
