/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package transport

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bradfitz/http2"
	"github.com/bradfitz/http2/hpack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

const (
	// http2MaxFrameLen specifies the max length of a HTTP2 frame.
	http2MaxFrameLen = 16384 // 16KB frame
	// http://http2.github.io/http2-spec/#SettingValues
	http2InitHeaderTableSize = 4096
)

var (
	clientPreface = []byte(http2.ClientPreface)
)

var http2RSTErrConvTab = map[http2.ErrCode]codes.Code{
	http2.ErrCodeNo:                 codes.Internal,
	http2.ErrCodeProtocol:           codes.Internal,
	http2.ErrCodeInternal:           codes.Internal,
	http2.ErrCodeFlowControl:        codes.Internal,
	http2.ErrCodeSettingsTimeout:    codes.Internal,
	http2.ErrCodeFrameSize:          codes.Internal,
	http2.ErrCodeRefusedStream:      codes.Unavailable,
	http2.ErrCodeCancel:             codes.Canceled,
	http2.ErrCodeCompression:        codes.Internal,
	http2.ErrCodeConnect:            codes.Internal,
	http2.ErrCodeEnhanceYourCalm:    codes.ResourceExhausted,
	http2.ErrCodeInadequateSecurity: codes.PermissionDenied,
}

var statusCodeConvTab = map[codes.Code]http2.ErrCode{
	codes.Internal:          http2.ErrCodeInternal, // pick an arbitrary one which is matched.
	codes.Canceled:          http2.ErrCodeCancel,
	codes.Unavailable:       http2.ErrCodeRefusedStream,
	codes.ResourceExhausted: http2.ErrCodeEnhanceYourCalm,
	codes.PermissionDenied:  http2.ErrCodeInadequateSecurity,
}

// Records the states during HPACK decoding. Must be reset once the
// decoding of the entire headers are finished.
type decodeState struct {
	// statusCode caches the stream status received from the trailer
	// the server sent. Client side only.
	statusCode codes.Code
	statusDesc string
	// Server side only fields.
	timeoutSet bool
	timeout    time.Duration
	method     string
	// key-value metadata map from the peer.
	mdata map[string]string
}

// An hpackDecoder decodes HTTP2 headers which may span multiple frames.
type hpackDecoder struct {
	h     *hpack.Decoder
	state decodeState
	err   error // The err when decoding
}

// A headerFrame is either a http2.HeaderFrame or http2.ContinuationFrame.
type headerFrame interface {
	Header() http2.FrameHeader
	HeaderBlockFragment() []byte
	HeadersEnded() bool
}

// isReservedHeader checks whether hdr belongs to HTTP2 headers
// reserved by gRPC protocol. Any other headers are classified as the
// user-specified metadata.
func isReservedHeader(hdr string) bool {
	if hdr[0] == ':' {
		return true
	}
	switch hdr {
	case "content-type",
		"grpc-message-type",
		"grpc-encoding",
		"grpc-message",
		"grpc-status",
		"grpc-timeout",
		"te",
		"user-agent":
		return true
	default:
		return false
	}
}

func newHPACKDecoder() *hpackDecoder {
	d := &hpackDecoder{}
	d.h = hpack.NewDecoder(http2InitHeaderTableSize, func(f hpack.HeaderField) {
		switch f.Name {
		case "grpc-status":
			code, err := strconv.Atoi(f.Value)
			if err != nil {
				d.err = StreamErrorf(codes.Internal, "transport: malformed grpc-status: %v", err)
				return
			}
			d.state.statusCode = codes.Code(code)
		case "grpc-message":
			d.state.statusDesc = f.Value
		case "grpc-timeout":
			d.state.timeoutSet = true
			var err error
			d.state.timeout, err = timeoutDecode(f.Value)
			if err != nil {
				d.err = StreamErrorf(codes.Internal, "transport: malformed time-out: %v", err)
				return
			}
		case ":path":
			d.state.method = f.Value
		default:
			if !isReservedHeader(f.Name) {
				if d.state.mdata == nil {
					d.state.mdata = make(map[string]string)
				}
				k, v, err := metadata.DecodeKeyValue(f.Name, f.Value)
				if err != nil {
					log.Printf("Failed to decode (%q, %q): %v", f.Name, f.Value, err)
					return
				}
				d.state.mdata[k] = v
			}
		}
	})
	return d
}

func (d *hpackDecoder) decodeClientHTTP2Headers(s *Stream, frame headerFrame) (endHeaders bool, err error) {
	d.err = nil
	_, err = d.h.Write(frame.HeaderBlockFragment())
	if err != nil {
		err = StreamErrorf(codes.Internal, "transport: HPACK header decode error: %v", err)
	}

	if frame.HeadersEnded() {
		if closeErr := d.h.Close(); closeErr != nil && err == nil {
			err = StreamErrorf(codes.Internal, "transport: HPACK decoder close error: %v", closeErr)
		}
		endHeaders = true
	}

	if err == nil && d.err != nil {
		err = d.err
	}
	return
}

func (d *hpackDecoder) decodeServerHTTP2Headers(s *Stream, frame headerFrame) (endHeaders bool, err error) {
	d.err = nil
	_, err = d.h.Write(frame.HeaderBlockFragment())
	if err != nil {
		err = StreamErrorf(codes.Internal, "transport: HPACK header decode error: %v", err)
	}

	if frame.HeadersEnded() {
		if closeErr := d.h.Close(); closeErr != nil && err == nil {
			err = StreamErrorf(codes.Internal, "transport: HPACK decoder close error: %v", closeErr)
		}
		endHeaders = true
	}

	if err == nil && d.err != nil {
		err = d.err
	}
	return
}

type timeoutUnit uint8

const (
	hour        timeoutUnit = 'H'
	minute      timeoutUnit = 'M'
	second      timeoutUnit = 'S'
	millisecond timeoutUnit = 'm'
	microsecond timeoutUnit = 'u'
	nanosecond  timeoutUnit = 'n'
)

func timeoutUnitToDuration(u timeoutUnit) (d time.Duration, ok bool) {
	switch u {
	case hour:
		return time.Hour, true
	case minute:
		return time.Minute, true
	case second:
		return time.Second, true
	case millisecond:
		return time.Millisecond, true
	case microsecond:
		return time.Microsecond, true
	case nanosecond:
		return time.Nanosecond, true
	default:
	}
	return
}

const maxTimeoutValue int64 = 100000000 - 1

// div does integer division and round-up the result. Note that this is
// equivalent to (d+r-1)/r but has less chance to overflow.
func div(d, r time.Duration) int64 {
	if m := d % r; m > 0 {
		return int64(d/r + 1)
	}
	return int64(d / r)
}

// TODO(zhaoq): It is the simplistic and not bandwidth efficient. Improve it.
func timeoutEncode(t time.Duration) string {
	if d := div(t, time.Nanosecond); d <= maxTimeoutValue {
		return strconv.FormatInt(d, 10) + "n"
	}
	if d := div(t, time.Microsecond); d <= maxTimeoutValue {
		return strconv.FormatInt(d, 10) + "u"
	}
	if d := div(t, time.Millisecond); d <= maxTimeoutValue {
		return strconv.FormatInt(d, 10) + "m"
	}
	if d := div(t, time.Second); d <= maxTimeoutValue {
		return strconv.FormatInt(d, 10) + "S"
	}
	if d := div(t, time.Minute); d <= maxTimeoutValue {
		return strconv.FormatInt(d, 10) + "M"
	}
	// Note that maxTimeoutValue * time.Hour > MaxInt64.
	return strconv.FormatInt(div(t, time.Hour), 10) + "H"
}

func timeoutDecode(s string) (time.Duration, error) {
	size := len(s)
	if size < 2 {
		return 0, fmt.Errorf("transport: timeout string is too short: %q", s)
	}
	unit := timeoutUnit(s[size-1])
	d, ok := timeoutUnitToDuration(unit)
	if !ok {
		return 0, fmt.Errorf("transport: timeout unit is not recognized: %q", s)
	}
	t, err := strconv.ParseInt(s[:size-1], 10, 64)
	if err != nil {
		return 0, err
	}
	return d * time.Duration(t), nil
}
