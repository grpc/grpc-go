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
	"testing"

	"golang.org/x/net/http2/hpack"
)

type fakeConn struct {
	WBuf []byte
	RBuf []byte
}

func (c *fakeConn) Write(p []byte) (int, error) {
	c.WBuf = append(c.WBuf, p...)
	return len(p), nil
}

func (c *fakeConn) Read(p []byte) (int, error) {
	n := copy(p, c.RBuf)
	c.RBuf = c.RBuf[n:]
	return n, nil
}

func appendUint32(b []byte, x uint32) []byte {
	return append(b, byte(x>>24), byte(x>>16), byte(x>>8), byte(x))
}

func readUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func readUint24(b []byte) int {
	return int(b[0])<<16 | int(b[1])<<8 | int(b[2])
}

func readUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

func checkWrittenHeader(t *testing.T, name string, hdr []byte, size int, ft FrameType, f Flag, streamID uint32) {
	if gotSize := readUint24(hdr[0:3]); gotSize != size {
		t.Errorf("%s(): Size: got %d, want %d", name, gotSize, size)
	}
	if gotType := FrameType(hdr[3]); gotType != ft {
		t.Errorf("%s(): Type: got %#x, want %#x", name, gotType, ft)
	}
	if gotFlag := Flag(hdr[4]); gotFlag != f {
		t.Errorf("%s(): Flags: got %#x, want %#x", name, gotFlag, f)
	}
	if gotSID := readUint32(hdr[5:9]); gotSID != streamID {
		t.Errorf("%s(): StreamID: got %d, want %d", name, gotSID, streamID)
	}
}

func checkReadHeader(t *testing.T, hdr *FrameHeader, size int, ft FrameType, f Flag, streamID uint32) {
	if hdr.Size != uint32(size) {
		t.Errorf("ReadFrame(): Size: got %d, want %d", hdr.Size, size)
	}
	if hdr.Type != ft {
		t.Errorf("ReadFrame(): Type: got %#x, want %#x", hdr.Type, ft)
	}
	if hdr.Flags != f {
		t.Errorf("ReadFrame(): Flags: got %#x, want %#x", hdr.Flags, f)
	}
	if hdr.StreamID != streamID {
		t.Errorf("ReadFrame(): StreamID: got %d, want %d", hdr.StreamID, streamID)
	}

}

func (s) TestHTTP2Bridge_ReadFrame_Data(t *testing.T) {
	c := &fakeConn{}
	recvData := "test data"
	c.RBuf = append(c.RBuf, 0, 0, byte(len(recvData)), byte(FrameTypeData), byte(FlagDataEndStream))
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = append(c.RBuf, []byte(recvData)...)

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, len(recvData), FrameTypeData, FlagDataEndStream, 1)
	df := fr.(*DataFrame)
	if string(df.Data) != recvData {
		t.Errorf("ReadFrame(): Data: got %q, want %q", string(df.Data), recvData)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_Headers(t *testing.T) {
	c := &fakeConn{}
	recvData := "test data"
	c.RBuf = append(c.RBuf, 0, 0, byte(len(recvData)), byte(FrameTypeHeaders), byte(FlagHeadersEndHeaders))
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = append(c.RBuf, []byte(recvData)...)

	f := NewHTTP2FramerBridge(c, c, 0)
	// Done to be able to test reading a single Headers Frame
	f.framer.ReadMetaHeaders = nil
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, len(recvData), FrameTypeHeaders, FlagHeadersEndHeaders, 1)
	hf := fr.(*HeadersFrame)
	if string(hf.HdrBlock) != recvData {
		t.Fatalf("ReadFrame(): HdrBlock: got %q, want %q", string(hf.HdrBlock), recvData)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_RSTStream(t *testing.T) {
	c := &fakeConn{}
	c.RBuf = append(c.RBuf, 0, 0, 4, byte(FrameTypeRSTStream), 0)
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = appendUint32(c.RBuf, uint32(ErrCodeProtocol))

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, 4, FrameTypeRSTStream, 0, 1)
	rf := fr.(*RSTStreamFrame)
	if rf.Code != ErrCodeProtocol {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", rf.Code, ErrCodeProtocol)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_Settings(t *testing.T) {
	c := &fakeConn{}
	s := Setting{ID: SettingsHeaderTableSize, Value: 200}
	c.RBuf = append(c.RBuf, 0, 0, 6, byte(FrameTypeSettings), 0)
	c.RBuf = appendUint32(c.RBuf, 0)
	c.RBuf = append(c.RBuf, byte(s.ID>>8), byte(s.ID))
	c.RBuf = appendUint32(c.RBuf, s.Value)

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, 6, FrameTypeSettings, 0, 0)

	sf := fr.(*SettingsFrame)
	if len(sf.Settings) != 1 {
		t.Fatalf("ReadFrame(): Settings: got %d, want %d", len(sf.Settings), 1)
	}
	if sf.Settings[0] != s {
		t.Errorf("ReadFrame(): Settings: got %v, want %v", sf.Settings[0], s)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_Ping(t *testing.T) {
	c := &fakeConn{}
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	c.RBuf = append(c.RBuf, 0, 0, 8, byte(FrameTypePing), 0)
	c.RBuf = appendUint32(c.RBuf, 0)
	c.RBuf = append(c.RBuf, d...)

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, 8, FrameTypePing, 0, 0)

	pf := fr.(*PingFrame)
	for i := range pf.Data {
		if pf.Data[i] != d[i] {
			t.Errorf("ReadFrame(): Data[%d]: got %d, want %d", i, pf.Data[i], d[i])
		}
	}
}

func (s) TestHTTP2Bridge_ReadFrame_GoAway(t *testing.T) {
	c := &fakeConn{}
	d := "debug_data"
	// The length of data + 4 byte code + 4 byte streamID
	ln := len(d) + 8
	c.RBuf = append(c.RBuf, 0, 0, byte(ln), byte(FrameTypeGoAway), 0)
	c.RBuf = appendUint32(c.RBuf, 0)
	c.RBuf = appendUint32(c.RBuf, 2)
	c.RBuf = appendUint32(c.RBuf, uint32(ErrCodeFlowControl))
	c.RBuf = append(c.RBuf, []byte(d)...)
	f := NewHTTP2FramerBridge(c, c, 0)

	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, ln, FrameTypeGoAway, 0, 0)

	gf := fr.(*GoAwayFrame)
	if gf.LastStreamID != 2 {
		t.Errorf("ReadFrame(): LastStreamID: got %d, want %d", gf.LastStreamID, 2)
	}
	if gf.Code != ErrCodeFlowControl {
		t.Errorf("ReadFrame(): Code: got %#x, want %#x", gf.Code, ErrCodeFlowControl)
	}
	if string(gf.DebugData) != d {
		t.Errorf("ReadFrame(): DebugData: got %q, want %q", string(gf.DebugData), d)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_WindowUpdate(t *testing.T) {
	c := &fakeConn{}
	c.RBuf = append(c.RBuf, 0, 0, 4, byte(FrameTypeWindowUpdate), 0)
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = appendUint32(c.RBuf, 100)

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, 4, FrameTypeWindowUpdate, 0, 1)

	wf := fr.(*WindowUpdateFrame)
	if wf.Inc != 100 {
		t.Errorf("ReadFrame(): Inc: got %d, want %d", wf.Inc, 1)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_Continuation(t *testing.T) {
	c := &fakeConn{}
	d := "hdr block"
	// Add a Headers frame to not get protocol error
	c.RBuf = append(c.RBuf, 0, 0, byte(len(d)), byte(FrameTypeHeaders), 0)
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = append(c.RBuf, []byte(d)...)

	c.RBuf = append(c.RBuf, 0, 0, byte(len(d)), byte(FrameTypeContinuation), byte(FlagContinuationEndHeaders))
	c.RBuf = appendUint32(c.RBuf, 1)
	c.RBuf = append(c.RBuf, []byte(d)...)

	f := NewHTTP2FramerBridge(c, c, 0)
	// Done to be able to test reading a single Headers Frame
	f.framer.ReadMetaHeaders = nil
	f.ReadFrame()
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	h := fr.Header()
	checkReadHeader(t, h, len(d), FrameTypeContinuation, FlagContinuationEndHeaders, 1)

	wf := fr.(*ContinuationFrame)
	if string(wf.HdrBlock) != d {
		t.Errorf("ReadFrame(): HdrBlock: got %q, want %q", string(wf.HdrBlock), d)
	}
}

func (s) TestHTTP2Bridge_ReadFrame_MetaHeaders(t *testing.T) {
	c := &fakeConn{}
	enc := hpack.NewEncoder(c)
	enc.WriteField(hpack.HeaderField{Name: "foo", Value: "bar"})
	c.RBuf = append(c.RBuf, 0, 0, byte(len(c.WBuf)), byte(FrameTypeHeaders), byte(FlagHeadersEndHeaders))
	c.RBuf = appendUint32(c.RBuf, 1)
	// Copy data written by the encoder into the reading buf
	c.RBuf = append(c.RBuf, c.WBuf...)

	f := NewHTTP2FramerBridge(c, c, 0)
	fr, err := f.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame(): %v", err)
	}

	mf, ok := fr.(*MetaHeadersFrame)
	if !ok {
		t.Errorf("ReadFrame(): Type: expected MetaHeadersFrame, got %T", fr)
	}
	if len(mf.Fields) != 1 {
		t.Errorf("ReadFrame(): Fields: got %d, want %d", len(mf.Fields), 1)
	}
	if mf.Fields[0].Name != "foo" {
		t.Errorf("ReadFrame(): Fields[0].Name: got %q, want %q", mf.Fields[0].Name, "foo")
	}
	if mf.Fields[0].Value != "bar" {
		t.Errorf("ReadFrame(): Fields[0].Value: got %q, want %q", mf.Fields[0].Value, "bar")
	}
}

func (s) TestHTTP2Bridge_WriteData(t *testing.T) {
	c := &fakeConn{}
	wantData := "test data"
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteData(1, false, []byte(wantData))

	checkWrittenHeader(t, "WriteData", c.WBuf[0:9], len(wantData), FrameTypeData, 0, 1)
	if string(c.WBuf[9:]) != wantData {
		t.Errorf("WriteData(): Data: got %q, want %q", string(c.WBuf[9:]), wantData)
	}
}

func (s) TestHttp2Bridge_WriteHeaders(t *testing.T) {
	tests := []struct {
		endStream  bool
		endHeaders bool
	}{
		{endStream: false, endHeaders: false},
		{endStream: true, endHeaders: false},
		{endStream: false, endHeaders: true},
		{endStream: true, endHeaders: true},
	}
	c := &fakeConn{}
	wantData := "test data"
	f := NewHTTP2FramerBridge(c, c, 0)

	for _, test := range tests {
		f.WriteHeaders(1, test.endStream, test.endHeaders, []byte(wantData))
		var flags Flag
		if test.endStream {
			flags |= FlagHeadersEndStream
		}
		if test.endHeaders {
			flags |= FlagHeadersEndHeaders
		}
		checkWrittenHeader(t, "WriteHeaders", c.WBuf[0:9], len(wantData), FrameTypeHeaders, flags, 1)
		if data := string(c.WBuf[9:]); data != wantData {
			t.Errorf("WriteHeaders(): Data: got %q, want %q", data, wantData)
		}
		c.WBuf = c.WBuf[:0]
	}
}

func (s) TestHTTP2Bridge_WriteRSTStream(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteRSTStream(1, ErrCodeProtocol)

	checkWrittenHeader(t, "WriteRSTStream", c.WBuf[0:9], 4, FrameTypeRSTStream, 0, 1)
	if errCode := readUint32(c.WBuf[9:13]); errCode != uint32(ErrCodeProtocol) {
		t.Errorf("WriteRSTStream(): SettingID: got %d, want %d", errCode, ErrCodeProtocol)
	}
}

func (s) TestHTTP2Bridge_WriteSettings(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteSettings(Setting{ID: SettingsHeaderTableSize, Value: 200})

	checkWrittenHeader(t, "WriteSettings", c.WBuf[0:9], 6, FrameTypeSettings, 0, 0)

	if settingID := readUint16(c.WBuf[9:11]); settingID != uint16(SettingsHeaderTableSize) {
		t.Errorf("WriteSettings(): SettingID: got %d, want %d", settingID, SettingsHeaderTableSize)
	}
	if settingVal := readUint32(c.WBuf[11:15]); settingVal != 200 {
		t.Errorf("WriteSettings(): SettingVal: got %d, want %d", settingVal, 200)
	}
}

func (s) TestHTTP2Bridge_WriteSettingsAck(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteSettingsAck()

	checkWrittenHeader(t, "WriteSettingsAck", c.WBuf[0:9], 0, FrameTypeSettings, FlagSettingsAck, 0)
}

func (s) TestHTTP2Bridge_WritePing(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	acks := []bool{true, false}
	wantData := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	for _, ack := range acks {
		f.WritePing(ack, wantData)
		var flag Flag
		if ack {
			flag |= FlagPingAck
		}
		checkWrittenHeader(t, "WritePing", c.WBuf[0:9], 8, FrameTypePing, flag, 0)
		data := c.WBuf[9:]
		for i := range data {
			if data[i] != wantData[i] {
				t.Errorf("WritePing(): Data[%d]: got %d, want %d", i, data[i], wantData[i])
			}
		}
		c.WBuf = c.WBuf[:0]
	}
}

func (s) TestHTTP2Bridge_WriteGoAway(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteGoAway(2, ErrCodeFlowControl, []byte("debug_data"))

	checkWrittenHeader(t, "WriteGoAway", c.WBuf[0:9], 18, FrameTypeGoAway, 0, 0)

	if lastStream := readUint32(c.WBuf[9:13]); lastStream != 2 {
		t.Errorf("WriteGoAway(): LastStreamID: got %d, want %d", lastStream, 2)
	}
	if code := ErrCode(readUint32(c.WBuf[13:17])); code != ErrCodeFlowControl {
		t.Errorf("WriteGoAway(): Code: got %d, want %d", code, ErrCodeFlowControl)
	}
	if data := string(c.WBuf[17:]); data != "debug_data" {
		t.Errorf("WriteGoAway(): Data: got %q, want %q", data, "debug_data")
	}
}

func (s) TestHTTP2Bridge_WriteWindowUpdate(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	f.WriteWindowUpdate(1, 2)

	checkWrittenHeader(t, "WriteWindowUpdate", c.WBuf[0:9], 4, FrameTypeWindowUpdate, 0, 1)
	if inc := readUint32(c.WBuf[9:13]); inc != 2 {
		t.Errorf("WriteWindowUpdate(): Inc: got %d, want %d", inc, 2)
	}
}

func (s) TestHTTP2Bridge_WriteContinuation(t *testing.T) {
	c := &fakeConn{}
	f := NewHTTP2FramerBridge(c, c, 0)
	wantData := "hdr block"
	endHeaders := []bool{true, false}

	for _, test := range endHeaders {
		f.WriteContinuation(1, test, []byte("hdr block"))
		var flags Flag
		if test {
			flags |= FlagContinuationEndHeaders
		}
		checkWrittenHeader(t, "WriteContinuation", c.WBuf[0:9], len(wantData), FrameTypeContinuation, flags, 1)
		if data := string(c.WBuf[9:]); data != wantData {
			t.Errorf("WriteContinuation(): Data: got %q, want %q", data, wantData)
		}
		c.WBuf = c.WBuf[:0]
	}
}
