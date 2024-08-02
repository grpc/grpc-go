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
	"google.golang.org/grpc/mem"
)

// HTTP2FramerBridge is a struct that works as an adapter for the net/x/http2
// Framer implementation to be able to work with the grpchttp2.Framer interface.
// This type exists to give an opt-out feature for the new framer and it is
// eventually going to be removed.
type HTTP2FramerBridge struct {
	framer *http2.Framer  // the underlying http2.Framer implementation to perform reads and writes.
	pool   mem.BufferPool // a pool to reutilize buffers when reading.
}

// NewHTTP2FramerBridge creates a new framer by taking a writer and a reader,
// alongside the maxHeaderListSize for the maximum size of the headers table.
// The underlying framer uses the SetReuseFrames feature to avoid extra
// allocations.
func NewHTTP2FramerBridge(w io.Writer, r io.Reader, maxHeaderListSize uint32) *HTTP2FramerBridge {
	fr := &HTTP2FramerBridge{
		framer: http2.NewFramer(w, r),
		pool:   mem.DefaultBufferPool(),
	}

	fr.framer.SetReuseFrames()
	fr.framer.MaxHeaderListSize = maxHeaderListSize
	fr.framer.ReadMetaHeaders = hpack.NewDecoder(initHeaderTableSize, nil)

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
		buf := mem.Copy(f.Data(), fr.pool)
		return &DataFrame{
			hdr:  hdr,
			Data: buf,
		}, nil
	case *http2.HeadersFrame:
		buf := mem.Copy(f.HeaderBlockFragment(), fr.pool)
		return &HeadersFrame{
			hdr:      hdr,
			HdrBlock: buf,
		}, nil
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
		return &SettingsFrame{
			hdr:      hdr,
			Settings: buf,
		}, nil
	case *http2.PingFrame:
		buf := mem.Copy(f.Data[:], fr.pool)
		return &PingFrame{
			hdr:  hdr,
			Data: buf,
		}, nil
	case *http2.GoAwayFrame:
		buf := mem.Copy(f.DebugData(), fr.pool)
		return &GoAwayFrame{
			hdr:          hdr,
			LastStreamID: f.LastStreamID,
			Code:         ErrCode(f.ErrCode),
			DebugData:    buf,
		}, nil
	case *http2.WindowUpdateFrame:
		return &WindowUpdateFrame{
			hdr: hdr,
			Inc: f.Increment,
		}, nil
	case *http2.ContinuationFrame:
		buf := mem.Copy(f.HeaderBlockFragment(), fr.pool)
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

func (fr *HTTP2FramerBridge) WriteData(streamID uint32, endStream bool, data mem.BufferSlice) error {
	buf := data.MaterializeToBuffer(fr.pool)
	err := fr.framer.WriteData(streamID, endStream, buf.ReadOnlyData())

	data.Free()
	buf.Free()
	return err
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
