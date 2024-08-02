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

package grpchttp2

import (
	"io"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
)

const (
	initHeaderTableSize = 4096
	maxFrameLen         = 16384
)

type HTTP2FramerBridge struct {
	framer *http2.Framer
	buf    [maxFrameLen]byte
	pool   grpc.SharedBufferPool
}

func NewHTTP2FramerBridge(w io.Writer, r io.Reader, maxHeaderListSize uint32) *HTTP2FramerBridge {
	fr := &HTTP2FramerBridge{
		framer: http2.NewFramer(w, r),
		pool:   grpc.NewSharedBufferPool(),
	}

	fr.framer.SetReuseFrames()
	fr.framer.MaxHeaderListSize = maxHeaderListSize
	fr.framer.ReadMetaHeaders = (hpack.NewDecoder(initHeaderTableSize, nil))

	return fr
}

func (fr *HTTP2FramerBridge) ReadFrame() (Frame, error) {
	f, err := fr.framer.ReadFrame()
	if err != nil {
		return nil, err
	}

	h := f.Header()
	hdr := &FrameHeader{
		Size:     h.Length,
		Type:     FrameType(h.Type),
		Flags:    Flag(h.Flags),
		StreamID: h.StreamID,
	}

	switch f := f.(type) {
	case *http2.DataFrame:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.Data())
		df := &DataFrame{
			hdr:  hdr,
			Data: buf,
		}
		df.free = func() {
			fr.pool.Put(&buf)
			df.Data = nil
		}
		return df, nil
	case *http2.HeadersFrame:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.HeaderBlockFragment())
		hf := &HeadersFrame{
			hdr:      hdr,
			HdrBlock: buf,
		}
		hf.free = func() {
			fr.pool.Put(&buf)
			hf.HdrBlock = nil
		}
		return hf, nil
	case *http2.RSTStreamFrame:
		return &RSTStreamFrame{
			hdr:  hdr,
			Code: ErrCode(f.ErrCode),
		}, nil
	case *http2.SettingsFrame:
		buf := make([]Setting, 0, f.NumSettings())
		f.ForeachSetting(func(s http2.Setting) error {
			buf = append(buf, Setting{
				ID:    SettingID(s.ID),
				Value: s.Val,
			})
			return nil
		})
		sf := &SettingsFrame{
			hdr:      hdr,
			Settings: buf,
		}
		return sf, nil
	case *http2.PingFrame:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.Data[:])
		pf := &PingFrame{
			hdr:  hdr,
			Data: buf,
		}
		pf.free = func() {
			fr.pool.Put(&buf)
			pf.Data = nil
		}
		return pf, nil
	case *http2.GoAwayFrame:
		buf := fr.pool.Get(int(hdr.Size - 8))
		copy(buf, f.DebugData())
		gf := &GoAwayFrame{
			hdr:          hdr,
			DebugData:    buf,
			Code:         ErrCode(f.ErrCode),
			LastStreamID: f.LastStreamID,
		}
		gf.free = func() {
			fr.pool.Put(&buf)
			gf.DebugData = nil
		}
		return gf, nil
	case *http2.WindowUpdateFrame:
		return &WindowUpdateFrame{
			hdr: hdr,
			Inc: f.Increment,
		}, nil
	case *http2.ContinuationFrame:
		buf := fr.pool.Get(int(hdr.Size))
		copy(buf, f.HeaderBlockFragment())
		return &ContinuationFrame{
			hdr:      hdr,
			HdrBlock: buf,
		}, nil
	case *http2.MetaHeadersFrame:
		return &MetaHeadersFrame{
			hdr:    hdr,
			Fields: f.Fields,
		}, nil
	}

	return nil, connError(ErrCodeProtocol)
}

func (fr *HTTP2FramerBridge) WriteData(streamID uint32, endStream bool, data ...[]byte) error {
	off := 0

	for _, s := range data {
		off += copy(fr.buf[off:], s)
	}

	return fr.framer.WriteData(streamID, endStream, fr.buf[:off])
}

func (fr *HTTP2FramerBridge) WriteHeaders(streamID uint32, endStream, endHeaders bool, headerBlock []byte) error {
	p := http2.HeadersFrameParam{
		StreamID:      streamID,
		EndStream:     endStream,
		EndHeaders:    endHeaders,
		BlockFragment: headerBlock,
	}

	return fr.framer.WriteHeaders(p)
}

func (fr *HTTP2FramerBridge) WriteRSTStream(streamID uint32, code ErrCode) error {
	return fr.framer.WriteRSTStream(streamID, http2.ErrCode(code))
}

func (fr *HTTP2FramerBridge) WriteSettings(settings ...Setting) error {
	ss := make([]http2.Setting, 0, len(settings))
	for _, s := range settings {
		ss = append(ss, http2.Setting{
			ID:  http2.SettingID(s.ID),
			Val: s.Value,
		})
	}

	return fr.framer.WriteSettings(ss...)
}

func (fr *HTTP2FramerBridge) WriteSettingsAck() error {
	return fr.framer.WriteSettingsAck()
}

func (fr *HTTP2FramerBridge) WritePing(ack bool, data [8]byte) error {
	return fr.framer.WritePing(ack, data)
}

func (fr *HTTP2FramerBridge) WriteGoAway(maxStreamID uint32, code ErrCode, debugData []byte) error {
	return fr.framer.WriteGoAway(maxStreamID, http2.ErrCode(code), debugData)
}

func (fr *HTTP2FramerBridge) WriteWindowUpdate(streamID, inc uint32) error {
	return fr.framer.WriteWindowUpdate(streamID, inc)
}

func (fr *HTTP2FramerBridge) WriteContinuation(streamID uint32, endHeaders bool, headerBlock []byte) error {
	return fr.framer.WriteContinuation(streamID, endHeaders, headerBlock)
}
